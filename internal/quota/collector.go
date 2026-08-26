// Package quota contains the read-only CodexBar adapter. It is deliberately
// independent from the Hub transport: the Node writes sanitized observations
// into the existing state store and the normal projector/uplink carries them.
package quota

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	MinimumInterval   = 60 * time.Second
	CommandTimeout    = 15 * time.Second
	MaxOutputBytes    = 512 << 10
	MaxAliasFileBytes = 64 << 10
	// LastGoodTTL is deliberately longer than the collection interval. A
	// transient CodexBar failure therefore retains the last-good account data
	// as degraded/stale, but an old quota can never look connected forever.
	LastGoodTTL = 10 * time.Minute
)

const quotaSourcePrefix = "quota."

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == CodexBarName {
		// Resolve to an absolute path first: a LaunchAgent's minimal PATH
		// never contains Homebrew, so a bare name would only work in an
		// interactive terminal. No shell is involved at any point.
		resolved, err := ResolveCodexBarCLI()
		if err != nil {
			return nil, err
		}
		name = resolved
	}
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// DefaultRunner returns the bounded local exec runner production uses.
func DefaultRunner() Runner { return execRunner{} }

type Observation struct {
	Provider     string
	AccountKey   string
	DisplayLabel string
	Windows      []state.QuotaWindow
	SampledAt    time.Time
	ObservedBy   string
}

type Collector struct {
	store       *state.Store
	runner      Runner
	nodeID      string
	identityKey []byte
	aliases     map[string]string
	logger      *slog.Logger
	now         func() time.Time
	interval    time.Duration
	timeout     time.Duration
	mu          sync.Mutex
}

func NewCollector(store *state.Store, nodeID string, identityKey []byte, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Collector{
		store: store, runner: execRunner{}, nodeID: nodeID,
		identityKey: append([]byte(nil), identityKey...), aliases: map[string]string{}, logger: logger,
		now: time.Now, interval: MinimumInterval, timeout: CommandTimeout,
	}
}

// SetAliases installs the safe provider/account-key -> display-name mapping.
// Keys are already HMAC-derived account keys; raw emails, provider IDs and
// other CodexBar identity fields never enter this map or the public state.
func (c *Collector) SetAliases(aliases map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aliases = make(map[string]string, len(aliases))
	for key, label := range aliases {
		if strings.TrimSpace(key) != "" && ValidateAliasLabel(label) {
			c.aliases[key] = strings.TrimSpace(label)
		}
	}
}

// ValidateAliasLabel validates a user-managed display name. Labels are
// presentation-only; account identity and cross-Mac deduplication continue to
// use the HMAC-derived account key. Keep the grammar deliberately small so a
// label can safely travel through config, CLI arguments, JSON and HTML.
func ValidateAliasLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 48 || !utf8.ValidString(label) {
		return false
	}
	for _, r := range label {
		if unicode.IsControl(r) || r == ',' || r == '=' || r == '\n' || r == '\r' {
			return false
		}
	}
	return true
}

// ParseAliases parses the safe, shareable account-key alias configuration.
// Labels are editable display names, while keys remain HMAC-derived account
// identities. Duplicate keys and duplicate labels are rejected: an ambiguous
// alias map could attach a stable public label to the wrong account.
func ParseAliases(spec string) (map[string]string, error) {
	aliases := map[string]string{}
	seenLabels := map[string]bool{}
	for _, entry := range strings.Split(strings.TrimSpace(spec), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "acct_") || len(parts[0]) != len("acct_")+32 {
			return nil, fmt.Errorf("quota account alias key invalid")
		}
		for _, char := range parts[0][len("acct_"):] {
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
				return nil, fmt.Errorf("quota account alias key invalid")
			}
		}
		label := strings.TrimSpace(parts[1])
		if !ValidateAliasLabel(label) {
			return nil, fmt.Errorf("quota account alias label invalid")
		}
		if _, duplicateKey := aliases[parts[0]]; duplicateKey {
			return nil, fmt.Errorf("quota account alias key duplicated")
		}
		if seenLabels[label] {
			return nil, fmt.Errorf("quota account alias label duplicated")
		}
		aliases[parts[0]] = label
		seenLabels[label] = true
	}
	return aliases, nil
}

// SetRunner is intentionally test-only in spirit: production always uses the
// bounded local exec runner. It makes fake CodexBar fixtures possible without
// invoking a GUI or reading credentials.
func (c *Collector) SetRunner(r Runner) {
	if r != nil {
		c.runner = r
	}
}
func (c *Collector) SetClock(now func() time.Time) {
	if now != nil {
		c.now = now
	}
}
func (c *Collector) SetInterval(interval time.Duration) {
	if interval >= MinimumInterval {
		c.interval = interval
	}
}
func (c *Collector) SetTimeout(timeout time.Duration) {
	if timeout > 0 {
		c.timeout = timeout
	}
}

// Collect executes both provider commands once. Each provider owns a separate
// source-health record. A failed provider retains its last-good observations
// only inside LastGoodTTL; after that the provider becomes unavailable while
// other providers keep their independent health.
func (c *Collector) Collect(ctx context.Context) error {
	if c.store == nil || c.runner == nil {
		return fmt.Errorf("quota collector is not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.now().UTC()
	providers := []struct{ name, label string }{{"codex", "Codex"}, {"zai", "GLM"}}
	providerNames := make([]string, 0, len(providers))
	for _, provider := range providers {
		providerNames = append(providerNames, provider.name)
	}
	results := make(map[string]providerResult, len(providers))
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		body, err := c.runner.Run(callCtx, CodexBarName, "usage", "--provider", provider.name, "--all-accounts", "--format", "json")
		cancel()
		if err != nil {
			if errors.Is(err, ErrCodexBarCLINotFound) {
				c.logger.Warn("codexbar cli unavailable", "error", "cli_unavailable")
				results[provider.name] = providerResult{cliUnavailable: true}
			} else {
				c.logger.Warn("quota provider unavailable", "provider", provider.name, "error", "command_failed")
			}
			continue
		}
		parsed, err := parseProviderWithAliases(body, provider.name, provider.label, c.identityKey, c.nodeID, at, c.aliases)
		if err != nil {
			if errors.Is(err, ErrAliasCoverage) {
				c.logger.Warn("quota account alias coverage missing", "provider", provider.name, "error", "configuration_required")
				results[provider.name] = providerResult{configurationRequired: true}
			} else {
				c.logger.Warn("quota provider response rejected", "provider", provider.name, "error", "invalid_response")
			}
			continue
		}
		results[provider.name] = providerResult{observations: parsed, success: true}
	}

	return c.store.Update(func(root *state.InternalRootState) error {
		if root.Sources == nil {
			root.Sources = make(map[string]state.SourceHealth)
		}
		available, degraded, lastSuccess := 0, 0, (*time.Time)(nil)
		for _, provider := range providers {
			sourceID := quotaSourcePrefix + provider.name
			previous := root.Sources[sourceID]
			result := results[provider.name]
			health := state.SourceHealth{LastAttemptAt: &at, LastSuccessAt: cloneQuotaTime(previous.LastSuccessAt)}
			if result.success {
				health.Status = state.SourceAvailable
				health.LastSuccessAt = &at
				health.Message = "CodexBar provider is available."
				root.Quota = replaceProvider(root.Quota, provider.name, result.observations, sourceID)
				available++
				lastSuccess = &at
				root.Sources[sourceID] = health
				continue
			}
			if result.cliUnavailable {
				// The CLI itself is missing. Keep the last-good TTL semantics
				// but with the explicit reason so displays can say "CodexBar
				// CLI unavailable" instead of a generic quota failure.
				health.Reason = "cli_unavailable"
				if previous.LastSuccessAt != nil && at.Sub(*previous.LastSuccessAt) <= LastGoodTTL {
					health.Status = state.SourceDegraded
					health.Message = "CodexBar CLI is unavailable; last-good quota is retained as stale."
					degraded++
				} else {
					health.Status = state.SourceUnavailable
					health.Message = "CodexBar CLI is unavailable."
					root.Quota = replaceProvider(root.Quota, provider.name, nil, sourceID)
				}
				root.Sources[sourceID] = health
				continue
			}
			if result.configurationRequired {
				// Fail-closed alias state: retain last-good Codex data only for
				// the same bounded TTL as a transient provider failure. Once the
				// TTL expires, remove that provider's quota so an unresolved alias
				// map cannot keep stale data looking connected forever.
				health.Reason = "configuration_required"
				if previous.LastSuccessAt != nil && at.Sub(*previous.LastSuccessAt) <= LastGoodTTL {
					health.Status = state.SourceDegraded
					health.Message = "Quota account aliases are required for the current Codex accounts (configuration_required); last-good quota is retained as stale."
					degraded++
				} else {
					health.Status = state.SourceUnavailable
					health.Message = "Quota account aliases are required for the current Codex accounts (configuration_required)."
					root.Quota = replaceProvider(root.Quota, provider.name, nil, sourceID)
				}
				root.Sources[sourceID] = health
				continue
			}
			if previous.LastSuccessAt != nil && at.Sub(*previous.LastSuccessAt) <= LastGoodTTL {
				health.Status = state.SourceDegraded
				health.Reason = "command_failed"
				health.Message = "CodexBar provider failed; last-good quota is retained as stale."
				degraded++
			} else {
				health.Status = state.SourceUnavailable
				health.Reason = "command_failed"
				health.Message = "CodexBar provider is unavailable."
			}
			root.Sources[sourceID] = health
		}
		if available == len(providers) {
			root.Sources["quota"] = state.SourceHealth{Status: state.SourceAvailable, LastAttemptAt: &at, LastSuccessAt: &at, Message: "CodexBar quota collector is available."}
		} else if available > 0 || degraded > 0 {
			for _, provider := range providers {
				if candidate := root.Sources[quotaSourcePrefix+provider.name].LastSuccessAt; candidate != nil && (lastSuccess == nil || candidate.After(*lastSuccess)) {
					lastSuccess = candidate
				}
			}
			aggregate := state.SourceHealth{Status: state.SourceDegraded, LastAttemptAt: &at, LastSuccessAt: cloneQuotaTime(lastSuccess), Message: "CodexBar quota collector is partially available."}
			if reason, ok := aggregateCLIReason(results, providerNames); ok {
				aggregate.Reason = reason
			}
			root.Sources["quota"] = aggregate
		} else {
			aggregate := state.SourceHealth{Status: state.SourceUnavailable, LastAttemptAt: &at, Message: "CodexBar quota collector is unavailable.", Reason: "command_failed"}
			if reason, ok := aggregateCLIReason(results, providerNames); ok {
				aggregate.Message = "CodexBar CLI is unavailable."
				aggregate.Reason = reason
			}
			root.Sources["quota"] = aggregate
		}
		root.GeneratedAt = at
		return nil
	})
}

// providerResult is one provider's bounded outcome for a single collection.
type providerResult struct {
	observations []Observation
	success      bool
	// configurationRequired marks an alias-coverage failure: CodexBar
	// itself answered, but the accountKey -> label map does not cover the
	// current Codex accounts. No label may be invented in that state.
	configurationRequired bool
	// cliUnavailable marks a missing CodexBar CLI (ErrCodexBarCLINotFound)
	// so source health can carry the explicit cli_unavailable reason
	// instead of a generic provider failure.
	cliUnavailable bool
}

// aggregateCLIReason reports "cli_unavailable" when every failed provider in
// this round failed because the CodexBar CLI is missing. Any other failure
// class keeps the aggregate generic.
func aggregateCLIReason(results map[string]providerResult, names []string) (string, bool) {
	failed, cliMissing := 0, 0
	for _, name := range names {
		result := results[name]
		if result.success {
			continue
		}
		failed++
		if result.cliUnavailable {
			cliMissing++
		}
	}
	if failed > 0 && failed == cliMissing {
		return "cli_unavailable", true
	}
	return "", false
}

func replaceProvider(previous []state.QuotaState, provider string, observations []Observation, sourceID string) []state.QuotaState {
	out := make([]state.QuotaState, 0, len(previous)+len(observations))
	for _, item := range previous {
		if item.Provider != provider {
			out = append(out, item)
		}
	}
	for _, item := range observations {
		out = append(out, state.QuotaState{Provider: item.Provider, AccountKey: item.AccountKey, DisplayLabel: item.DisplayLabel, Windows: quotaWindows(item.Windows), SampledAt: timePtr(item.SampledAt), SourceID: sourceID, ObservedBy: item.ObservedBy})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].AccountKey < out[j].AccountKey
	})
	return out
}

func quotaWindows(in []state.QuotaWindow) *[]state.QuotaWindow {
	out := append([]state.QuotaWindow(nil), in...)
	return &out
}

func timePtr(v time.Time) *time.Time { return &v }

func cloneQuotaTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Start runs an immediate sample, then enforces the 60-second minimum between
// samples. The runtime is intentionally small so shutdown is deterministic.
type Runtime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func Start(ctx context.Context, collector *Collector) *Runtime {
	child, cancel := context.WithCancel(ctx)
	r := &Runtime{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		_ = collector.Collect(child)
		ticker := time.NewTicker(collector.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = collector.Collect(child)
			case <-child.Done():
				return
			}
		}
	}()
	return r
}
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// These types mirror CodexBar 0.54.0's ProviderPayload/UsageSnapshot JSON.
// In particular, the CLI emits a top-level array; there is no accounts
// wrapper. Keeping the decoder explicit prevents accepting a made-up or
// accidentally nested protocol shape.
type rawProviderPayload struct {
	Provider string          `json:"provider"`
	Account  json.RawMessage `json:"account"`
	Usage    rawUsage        `json:"usage"`
}

type rawUsage struct {
	Identity         rawIdentity          `json:"identity"`
	Primary          *rawRateWindow       `json:"primary"`
	Secondary        *rawRateWindow       `json:"secondary"`
	Tertiary         *rawRateWindow       `json:"tertiary"`
	ExtraRateWindows []rawNamedRateWindow `json:"extraRateWindows"`
}

type rawIdentity struct {
	ProviderID          string `json:"providerID"`
	AccountEmail        string `json:"accountEmail"`
	AccountOrganization string `json:"accountOrganization"`
	LoginMethod         string `json:"loginMethod"`
	AccountID           string `json:"accountID"`
}

type rawRateWindow struct {
	UsedPercent      *float64   `json:"usedPercent"`
	WindowMinutes    *int       `json:"windowMinutes"`
	RemainingPercent *float64   `json:"remainingPercent"`
	ResetsAt         *time.Time `json:"resetsAt"`
}

type rawNamedRateWindow struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Window     rawRateWindow `json:"window"`
	UsageKnown *bool         `json:"usageKnown"`
}

type parsedAccount struct {
	Identity string
	Windows  []state.QuotaWindow
}

type keyedAccount struct {
	parsedAccount
	key string
}

// ErrAliasCoverage marks a Codex response whose account keys are not fully
// covered by the configured alias map. It is the fail-closed boundary: no
// display label may be derived from array position, CodexBar-local account
// names, or the set of accounts a single Node happens to see right now.
var ErrAliasCoverage = errors.New("quota account alias coverage missing")

// ErrNoAccounts is a valid provider response with no usable account rows. It
// is deliberately not treated as SourceAvailable: an empty response cannot
// prove that a quota source is healthy or that the frozen account profile is
// covered.
var ErrNoAccounts = errors.New("quota provider returned no accounts")

func parseAccounts(body []byte, provider string) ([]parsedAccount, error) {
	if len(body) == 0 || len(body) > MaxOutputBytes {
		return nil, fmt.Errorf("quota response size invalid")
	}
	var payload []rawProviderPayload
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	if payload == nil {
		return nil, fmt.Errorf("top-level account array missing")
	}
	if len(payload) == 0 {
		return nil, ErrNoAccounts
	}
	parsed := make([]parsedAccount, 0, len(payload))
	for _, item := range payload {
		if item.Provider != "" && !strings.EqualFold(item.Provider, provider) {
			return nil, fmt.Errorf("provider mismatch")
		}
		identity := providerIdentity(item.Account, item.Usage.Identity)
		if identity == "" {
			return nil, fmt.Errorf("account identity missing")
		}
		windows, err := usageWindows(item.Usage)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, parsedAccount{Identity: identity, Windows: windows})
	}
	return parsed, nil
}

func parseProviderWithAliases(body []byte, provider, family string, salt []byte, nodeID string, sampledAt time.Time, aliases map[string]string) ([]Observation, error) {
	parsed, err := parseAccounts(body, provider)
	if err != nil {
		return nil, err
	}
	// Account aliases are resolved only from HMAC account keys. The single
	// provider family label stays reserved for providers without a
	// multi-account ambiguity.
	keyed := make([]keyedAccount, 0, len(parsed))
	for _, account := range parsed {
		keyed = append(keyed, keyedAccount{parsedAccount: account, key: AccountKey(salt, provider, account.Identity)})
	}
	sort.SliceStable(keyed, func(i, j int) bool { return keyed[i].key < keyed[j].key })
	out := make([]Observation, 0, len(keyed))
	for _, account := range keyed {
		// The secure alias map is the label authority whenever an alias is
		// configured. Codex requires explicit coverage because it may have
		// multiple accounts. Z.ai historically had one fixed GLM family label,
		// so an absent Z.ai alias safely falls back to that default for old
		// configurations while a configured alias remains editable.
		label := aliases[account.key]
		if label == "" {
			if provider == "codex" {
				return nil, ErrAliasCoverage
			}
			label = family
		}
		out = append(out, Observation{Provider: provider, AccountKey: account.key, DisplayLabel: label, Windows: account.Windows, SampledAt: sampledAt, ObservedBy: nodeID})
	}
	return out, nil
}

func providerIdentity(account json.RawMessage, identity rawIdentity) string {
	accountValue := accountString(account)
	return firstNonEmpty(
		identity.AccountID,
		identity.AccountEmail,
		accountValue,
		identity.AccountOrganization+"/"+identity.LoginMethod,
		identity.ProviderID,
	)
}

func accountString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, key := range []string{"accountID", "id", "email", "accountEmail", "label", "name"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func usageWindows(usage rawUsage) ([]state.QuotaWindow, error) {
	type namedWindow struct {
		name string
		body *rawRateWindow
	}
	items := []namedWindow{{"PRIMARY", usage.Primary}, {"SECONDARY", usage.Secondary}, {"TERTIARY", usage.Tertiary}}
	windows := make([]state.QuotaWindow, 0, len(items)+len(usage.ExtraRateWindows))
	for _, item := range items {
		if item.body == nil {
			continue
		}
		window, ok, err := convertRateWindow(item.name, *item.body)
		if err != nil {
			return nil, err
		}
		if ok {
			windows = append(windows, window)
		}
	}
	for _, item := range usage.ExtraRateWindows {
		if item.UsageKnown != nil && !*item.UsageKnown {
			continue
		}
		name := firstNonEmpty(item.Title, item.ID, "EXTRA")
		window, ok, err := convertRateWindow(name, item.Window)
		if err != nil {
			return nil, err
		}
		if ok {
			windows = append(windows, window)
		}
	}
	return windows, nil
}

func convertRateWindow(name string, raw rawRateWindow) (state.QuotaWindow, bool, error) {
	percent, ok := raw.UsedPercent, raw.UsedPercent != nil
	if !ok && raw.RemainingPercent != nil {
		value := 100 - *raw.RemainingPercent
		percent, ok = &value, true
	}
	if !ok {
		return state.QuotaWindow{}, false, nil
	}
	if math.IsNaN(*percent) || math.IsInf(*percent, 0) || *percent < 0 || *percent > 100 {
		return state.QuotaWindow{}, false, fmt.Errorf("invalid percent")
	}
	value := *percent
	return state.QuotaWindow{Name: firstNonEmpty(name, "WINDOW"), UsedPercent: &value, ResetsAt: raw.ResetsAt}, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// AccountKey is a stable irreversible cross-Mac identity. The key must be a
// shared secret provisioned through secure configuration; a per-machine
// random salt would make Hub deduplication mathematically impossible.
func AccountKey(identityKey []byte, provider, identity string) string {
	mac := hmac.New(sha256.New, identityKey)
	_, _ = mac.Write([]byte(provider + "\x00" + identity))
	sum := mac.Sum(nil)
	return "acct_" + hex.EncodeToString(sum[:16])
}

// LoadIdentityKey reads an existing shared HMAC key. It never creates one.
// The file is intentionally mode-0600 and must contain at least 32 bytes.
func LoadIdentityKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("quota identity key path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("quota identity key unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("quota identity key permissions invalid")
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) < 32 {
		return nil, fmt.Errorf("quota identity key invalid")
	}
	return body, nil
}

// LoadAliasFile validates and canonicalizes an existing, mode-0600 alias
// file. The returned value is safe config text only: it contains HMAC-derived
// account keys and safe user-managed display labels, never the source file
// path's contents beyond those safe mappings. The file is read for validation only;
// it is never copied or modified by onboarding.
func LoadAliasFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("quota alias file path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("quota alias file unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", fmt.Errorf("quota alias file permissions invalid")
	}
	if info.Size() > MaxAliasFileBytes {
		return "", fmt.Errorf("quota alias file too large")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("quota alias file unreadable")
	}
	parts := strings.FieldsFunc(string(body), func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	if len(parts) == 0 {
		return "", fmt.Errorf("quota alias file empty")
	}
	aliases, err := ParseAliases(strings.Join(parts, ","))
	if err != nil || len(aliases) == 0 {
		return "", fmt.Errorf("quota alias file invalid")
	}
	keys := make([]string, 0, len(aliases))
	for key := range aliases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, key+"="+aliases[key])
	}
	return strings.Join(entries, ","), nil
}

// AliasAuditEntry is one machine-readable row of the account-key safety
// check. It intentionally carries only the irreversible account key, the
// provider, and the alias status: emails, provider IDs, raw account names,
// and even the resolved public label never appear, so the output is safe to
// feed automated configuration tooling.
type AliasAuditEntry struct {
	AccountKey  string `json:"accountKey"`
	Provider    string `json:"provider"`
	AliasStatus string `json:"aliasStatus"`
}

// AliasAudit is the machine-readable result for future automated alias
// configuration. OK is true only when every visible Codex account key has a
// configured alias.
type AliasAudit struct {
	OK       bool              `json:"ok"`
	Accounts []AliasAuditEntry `json:"accounts"`
}

// AuditAliases probes CodexBar for the currently visible Codex accounts and
// reports alias coverage per account key. A probe failure is returned as an
// error so callers can distinguish "CodexBar unavailable" from "aliases
// incomplete"; the audit result itself never carries identity material.
func AuditAliases(ctx context.Context, runner Runner, identityKey []byte, aliases map[string]string) (AliasAudit, error) {
	if runner == nil {
		return AliasAudit{}, fmt.Errorf("quota alias audit requires a runner")
	}
	callCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	body, err := runner.Run(callCtx, "codexbar", "usage", "--provider", "codex", "--all-accounts", "--format", "json")
	if err != nil {
		return AliasAudit{}, err
	}
	accounts, err := parseAccounts(body, "codex")
	if err != nil {
		// Alias audit has a different purpose from product availability: an
		// empty local account set has no alias rows to cover. Product detect
		// still treats the same response as unavailable via DetectAccounts.
		if errors.Is(err, ErrNoAccounts) {
			return AliasAudit{OK: true, Accounts: []AliasAuditEntry{}}, nil
		}
		return AliasAudit{}, err
	}
	audit := AliasAudit{OK: true, Accounts: make([]AliasAuditEntry, 0, len(accounts))}
	for _, account := range accounts {
		key := AccountKey(identityKey, "codex", account.Identity)
		status := "missing"
		if aliases[key] != "" {
			status = "configured"
		} else {
			audit.OK = false
		}
		audit.Accounts = append(audit.Accounts, AliasAuditEntry{AccountKey: key, Provider: "codex", AliasStatus: status})
	}
	sort.Slice(audit.Accounts, func(i, j int) bool { return audit.Accounts[i].AccountKey < audit.Accounts[j].AccountKey })
	return audit, nil
}

// StartupCheck is the fail-closed collector startup decision. Reason is
// machine-readable and identity-free: "alias_parse_failed",
// "alias_coverage_missing", or "" when the collector may start.
type StartupCheck struct {
	Aliases        map[string]string
	Reason         string
	StartCollector bool
}

// CheckStartup validates the alias configuration before the collector is
// allowed to start. A parse failure or an alias map that does not cover the
// current Codex account keys is a hard fail-closed: the alias-less collector
// must not start. A CodexBar probe failure is deliberately not blocking,
// because the running collector already fails closed per collection and
// reports CodexBar unavailability honestly.
func CheckStartup(ctx context.Context, runner Runner, identityKey []byte, aliasSpec string) StartupCheck {
	aliases, err := ParseAliases(aliasSpec)
	if err != nil {
		return StartupCheck{Reason: "alias_parse_failed"}
	}
	if len(aliases) == 0 {
		// An explicitly configured quota collector must never start with an
		// empty alias authority. Otherwise a transient CodexBar outage could
		// make an alias-less collector appear healthy until the first later
		// response arrives.
		return StartupCheck{Reason: "alias_config_empty"}
	}
	audit, err := AuditAliases(ctx, runner, identityKey, aliases)
	if err != nil {
		return StartupCheck{Aliases: aliases, StartCollector: true}
	}
	if !audit.OK {
		return StartupCheck{Aliases: aliases, Reason: "alias_coverage_missing"}
	}
	return StartupCheck{Aliases: aliases, StartCollector: true}
}

// MarkConfigurationRequired records the fail-closed alias state when the
// collector is not allowed to start, so displays surface an explicit degraded
// quota instead of a silent "not connected". The message is static and never
// carries identity material.
func MarkConfigurationRequired(store *state.Store, at time.Time) error {
	if store == nil {
		return fmt.Errorf("quota configuration marker requires a store")
	}
	return store.Update(func(root *state.InternalRootState) error {
		if root.Sources == nil {
			root.Sources = make(map[string]state.SourceHealth)
		}
		message := "Quota account aliases are not configured for the current Codex accounts (configuration_required)."
		for _, sourceID := range []string{"quota", "quota.codex"} {
			previous := root.Sources[sourceID]
			root.Sources[sourceID] = state.SourceHealth{
				Status:        state.SourceDegraded,
				LastAttemptAt: cloneQuotaTime(&at),
				LastSuccessAt: previous.LastSuccessAt,
				Message:       message,
			}
		}
		root.GeneratedAt = at.UTC()
		return nil
	})
}
