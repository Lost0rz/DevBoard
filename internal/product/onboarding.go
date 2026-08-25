package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agent"
	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/quota"
)

// OnboardingOptions is the non-UI, repeatable Node installation workflow.
// Secrets are accepted from files or injected tests and never appear in the
// Result. The default command uses the existing service/integration managers.
type OnboardingOptions struct {
	ConfigPath  string
	NodeID      string
	DisplayName string
	HubEndpoint string
	NodeToken   string
	AdminToken  string
	// QuotaIdentityKeyFile and QuotaAliasFile are references to existing
	// security files. Onboarding validates them and persists only the key path
	// and sanitized account-key aliases; it never creates, copies, or prints
	// either file's contents.
	QuotaIdentityKeyFile string
	QuotaAliasFile       string
	DryRun               bool
	Check                bool
	Service              func(string) operationResult
	Integration          func(string, string) operationResult
	SaveConfig           func(string, config.Config) error
	Register             func(context.Context, string, string, string, string) (string, error)
	// The following probes are injectable so check/failure matrices can run
	// without touching a real LaunchAgent, Hook, socket, or Hub.
	SocketCheck func() string
	UplinkCheck func(context.Context, config.Config) string
	HubCheck    func(context.Context, config.Config, string) string
	// QuotaRunner overrides the CodexBar probe runner used by the quota check
	// phases; nil uses the bounded local exec runner. QuotaSnapshot overrides
	// the local first-quota-snapshot probe. HubQuotaCheck probes the Hub's
	// deduplicated global quota projection; it must not be replaced by the
	// local snapshot check because a Node is expected to observe only a subset
	// of the global accounts.
	QuotaRunner   quota.Runner
	QuotaSnapshot func(context.Context, config.Config) string
	HubQuotaCheck func(context.Context, config.Config) string
}

type onboardingPhase struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func RunNodeOnboarding(opts OnboardingOptions) Result {
	result := runNodeOnboarding(opts)
	return resultValue(result)
}

func runNodeOnboarding(opts OnboardingOptions) operationResult {
	if opts.Service == nil {
		opts.Service = runService
	}
	if opts.Integration == nil {
		opts.Integration = runIntegration
	}
	if opts.SaveConfig == nil {
		opts.SaveConfig = config.SaveAtomic
	}
	if opts.Register == nil {
		opts.Register = registerNode
	}
	if opts.SocketCheck == nil {
		opts.SocketCheck = localSocketStatus
	}
	if opts.UplinkCheck == nil {
		opts.UplinkCheck = checkLocalUplink
	}
	if opts.HubCheck == nil {
		opts.HubCheck = checkHubNodeOnline
	}
	if opts.HubQuotaCheck == nil {
		opts.HubQuotaCheck = checkHubQuotaCoverage
	}
	if opts.ConfigPath == "" {
		paths, err := ResolvePaths("")
		if err != nil {
			return errorResult("onboarding_paths_unavailable", "Node onboarding paths are unavailable", nil)
		}
		opts.ConfigPath = paths.Config
	}
	phases := []onboardingPhase{}
	add := func(name, status string) { phases = append(phases, onboardingPhase{Name: name, Status: status}) }

	cfg := config.Defaults()
	if existing, err := config.Load(opts.ConfigPath); err == nil {
		cfg = existing
		add("existing_config", "complete")
	} else if !errors.Is(err, os.ErrNotExist) {
		return onboardingFailure(phases, "config_read_failed", "existing Node config could not be read")
	} else {
		add("existing_config", "complete")
	}
	if err := applyQuotaOptions(&cfg, opts); err != nil {
		return onboardingFailure(phases, quotaOptionErrorCode(err), "quota security configuration is invalid")
	}
	if !opts.Check {
		if err := validateQuotaConfig(cfg); err != nil {
			return onboardingFailure(phases, quotaOptionErrorCode(err), "quota security configuration is invalid")
		}
	}

	nodeID := strings.TrimSpace(opts.NodeID)
	if nodeID == "" {
		nodeID = cfg.Host.ID
	}
	displayName := strings.TrimSpace(opts.DisplayName)
	if displayName == "" {
		displayName = cfg.Host.DisplayName
	}
	endpoint := strings.TrimSpace(opts.HubEndpoint)
	if endpoint == "" {
		endpoint = cfg.Uplink.Endpoint
	}
	if nodeID == "" || displayName == "" || endpoint == "" {
		return onboardingFailure(phases, "identity_or_endpoint_missing", "node id, display name, and Hub endpoint are required")
	}
	add("node_identity", "complete")
	add("hub_endpoint", "complete")

	token := strings.TrimSpace(opts.NodeToken)
	if token == "" {
		token = strings.TrimSpace(cfg.Uplink.Token)
	}
	if token == "" && opts.AdminToken != "" {
		if opts.DryRun || opts.Check {
			add("hub_registration", "pending")
		} else {
			registered, err := opts.Register(context.Background(), endpoint, opts.AdminToken, nodeID, displayName)
			if err != nil {
				return onboardingFailure(phases, "hub_registration_failed", "Hub registry registration failed")
			}
			token = registered
			add("hub_registration", "complete")
		}
	} else if token != "" {
		add("hub_registration", "complete")
	} else {
		return onboardingFailure(phases, "hub_registration_required", "provide a pre-provisioned Node token or an admin token file")
	}
	if token != "" {
		cfg.Runtime.Role = config.RuntimeRoleNode
		cfg.Host.ID, cfg.Host.DisplayName = nodeID, displayName
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: endpoint, NodeID: nodeID, Token: token}
	}
	add("node_token", "complete")

	if opts.Check {
		if err := config.Validate(cfg); err != nil {
			return onboardingFailure(phases, "config_invalid", "Node onboarding configuration is invalid")
		}
		add("config", "complete")
		service := opts.Service("status")
		if service.OK {
			add("launch_agent", "complete")
		} else {
			add("launch_agent", "degraded")
		}
		for _, provider := range []string{integrationCodex, integrationClaude} {
			status := opts.Integration(provider, "status")
			if status.OK {
				add(provider+"_hook", "complete")
			} else {
				add(provider+"_hook", "degraded")
			}
		}
		if opts.SocketCheck() == "ready" {
			add("local_socket", "complete")
		} else {
			add("local_socket", "degraded")
		}
		add("node_uplink", normalizeCheckStatus(opts.UplinkCheck(context.Background(), cfg)))
		add("hub_online", normalizeCheckStatus(opts.HubCheck(context.Background(), cfg, nodeID)))
		extra := map[string]any{}
		addClosureCheckPhases(add, extra, opts, cfg)
		return onboardingCheckResult(phases, extra)
	}
	if opts.DryRun {
		add("config", "would-write")
		if strings.TrimSpace(opts.QuotaIdentityKeyFile) != "" {
			add("quota_identity_key", "would-verify")
		}
		if strings.TrimSpace(opts.QuotaAliasFile) != "" {
			add("quota_alias_config", "would-verify")
		}
		add("launch_agent", "would-install")
		add("codex_hook", "would-merge")
		add("claude_code_hook", "would-merge")
		add("local_socket", "would-verify")
		add("node_uplink", "would-verify")
		add("hub_online", "would-verify")
		return okResult("onboarding_dry_run", "Node onboarding dry-run completed", map[string]any{"phases": phases})
	}
	if err := os.MkdirAll(filepath.Dir(opts.ConfigPath), 0o700); err != nil {
		return onboardingFailure(phases, "config_directory_failed", "Node configuration directory could not be prepared")
	}
	if err := opts.SaveConfig(opts.ConfigPath, cfg); err != nil {
		return onboardingFailure(phases, "config_write_failed", "Node onboarding config could not be saved")
	}
	add("config", "complete")
	service := opts.Service("install")
	if !service.OK {
		return onboardingFailure(phases, "launch_agent_failed", "LaunchAgent installation failed")
	}
	add("launch_agent", "complete")
	for _, item := range []struct{ name, provider string }{{"codex_hook", integrationCodex}, {"claude_code_hook", integrationClaude}} {
		status := opts.Integration(item.provider, "install")
		if !status.OK {
			return onboardingFailure(phases, item.name+"_failed", "provider Hook merge failed")
		}
		add(item.name, "complete")
	}
	if opts.SocketCheck() == "ready" {
		add("local_socket", "complete")
	} else {
		add("local_socket", "degraded")
	}
	add("node_uplink", normalizeCheckStatus(opts.UplinkCheck(context.Background(), cfg)))
	add("hub_online", normalizeCheckStatus(opts.HubCheck(context.Background(), cfg, nodeID)))
	extra := map[string]any{}
	addClosureCheckPhases(add, extra, opts, cfg)
	return onboardingInstallResult(phases, extra)
}

func applyQuotaOptions(cfg *config.Config, opts OnboardingOptions) error {
	if strings.TrimSpace(opts.QuotaIdentityKeyFile) != "" {
		if _, err := quota.LoadIdentityKey(opts.QuotaIdentityKeyFile); err != nil {
			return fmt.Errorf("quota_identity_key_invalid: %w", err)
		}
		cfg.Quota.IdentityKeyFile = filepath.Clean(opts.QuotaIdentityKeyFile)
	}
	if strings.TrimSpace(opts.QuotaAliasFile) != "" {
		canonical, err := quota.LoadAliasFile(opts.QuotaAliasFile)
		if err != nil {
			return fmt.Errorf("quota_alias_file_invalid: %w", err)
		}
		cfg.Quota.AccountAliases = canonical
	}
	return nil
}

func quotaOptionErrorCode(err error) string {
	if strings.Contains(err.Error(), "quota_alias_file_invalid") {
		return "quota_alias_file_invalid"
	}
	if strings.Contains(err.Error(), "quota_alias_config_invalid") {
		return "quota_alias_config_invalid"
	}
	return "quota_identity_key_invalid"
}

func validateQuotaConfig(cfg config.Config) error {
	if strings.TrimSpace(cfg.Quota.IdentityKeyFile) == "" {
		return nil
	}
	if _, err := quota.LoadIdentityKey(cfg.Quota.IdentityKeyFile); err != nil {
		return fmt.Errorf("quota_identity_key_invalid: %w", err)
	}
	aliases, err := quota.ParseAliases(cfg.Quota.AccountAliases)
	if err != nil || len(aliases) == 0 {
		return fmt.Errorf("quota_alias_config_invalid")
	}
	return nil
}

func normalizeCheckStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete":
		return "complete"
	case "pending":
		return "pending"
	default:
		return "degraded"
	}
}

// addClosureCheckPhases appends both independent quota acceptance stages:
// quota_snapshot is the local Node's partial source health, while
// quota_global_snapshot is the Hub's deduplicated all-host account coverage.
// A Node must not be forced to observe accounts that belong to another Mac.
func addClosureCheckPhases(add func(name, status string), extra map[string]any, opts OnboardingOptions, cfg config.Config) {
	addQuotaCheckPhases(add, extra, opts, cfg)
	addHubQuotaCheckPhase(add, extra, opts, cfg)
}

// addQuotaCheckPhases validates only the local Node's configured and observed
// quota sources. It deliberately accepts a partial account set: Codex A/B may
// be on one Mac while GLM is observed by another Mac.
func addQuotaCheckPhases(add func(name, status string), extra map[string]any, opts OnboardingOptions, cfg config.Config) {
	if strings.TrimSpace(cfg.Quota.IdentityKeyFile) == "" {
		add("quota_loop", "quota_not_configured")
		extra["quotaLocalStatus"] = "quota_not_configured"
		return
	}
	if identityKey, keyErr := quota.LoadIdentityKey(cfg.Quota.IdentityKeyFile); keyErr != nil {
		add("quota_identity_key", "degraded")
		add("quota_alias_config", "degraded")
		// Without the shared key no account keys can be derived, so CodexBar
		// availability and alias coverage cannot be verified either.
		add("codexbar", "degraded")
		add("quota_alias_coverage", "degraded")
		extra["quotaLocalStatus"] = "degraded"
	} else {
		add("quota_identity_key", "complete")
		localConfigurationHealthy := true
		runner := opts.QuotaRunner
		if runner == nil {
			runner = quota.DefaultRunner()
		}
		aliases, aliasErr := quota.ParseAliases(cfg.Quota.AccountAliases)
		aliasConfigOK := aliasErr == nil && len(aliases) > 0
		if aliasConfigOK {
			add("quota_alias_config", "complete")
		} else {
			add("quota_alias_config", "degraded")
			localConfigurationHealthy = false
		}
		// Probe CodexBar independently from alias validation. This preserves a
		// useful machine-readable availability result while keeping malformed or
		// incomplete alias configuration fail-closed.
		audit, err := quota.AuditAliases(context.Background(), runner, identityKey, aliases)
		if err != nil {
			add("codexbar", "degraded")
			add("quota_alias_coverage", "degraded")
			localConfigurationHealthy = false
		} else {
			add("codexbar", "complete")
			if aliasConfigOK && audit.OK {
				add("quota_alias_coverage", "complete")
			} else {
				add("quota_alias_coverage", "degraded")
				localConfigurationHealthy = false
			}
			extra["quotaAliasAudit"] = audit.Accounts
		}
		if !localConfigurationHealthy {
			extra["quotaLocalStatus"] = "degraded"
		}
	}
	snapshot := opts.QuotaSnapshot
	if snapshot == nil {
		snapshot = checkQuotaSnapshot
	}
	localStatus := normalizeQuotaStatus(snapshot(context.Background(), cfg))
	add("quota_snapshot", localStatus)
	if localStatus == "degraded" || localStatus == "pending" {
		extra["quotaLocalStatus"] = localStatus
	} else if _, exists := extra["quotaLocalStatus"]; !exists {
		extra["quotaLocalStatus"] = "complete"
	}
}

func addHubQuotaCheckPhase(add func(name, status string), extra map[string]any, opts OnboardingOptions, cfg config.Config) {
	if strings.TrimSpace(cfg.Quota.IdentityKeyFile) == "" {
		add("quota_global_snapshot", "quota_not_configured")
		extra["quotaGlobalStatus"] = "quota_not_configured"
		return
	}
	if _, err := quota.LoadIdentityKey(cfg.Quota.IdentityKeyFile); err != nil {
		add("quota_global_snapshot", "degraded")
		extra["quotaGlobalStatus"] = "degraded"
		return
	}
	status := normalizeQuotaStatus(opts.HubQuotaCheck(context.Background(), cfg))
	add("quota_global_snapshot", status)
	extra["quotaGlobalStatus"] = status
}

// normalizeLoopbackHost maps wildcard bind addresses to a dialable loopback
// host: a client cannot portably dial 0.0.0.0 or ::. IPv6 literals must be
// bracketed by net.JoinHostPort instead of string concatenation.
func normalizeLoopbackHost(host string) string {
	trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(host), "[]"))
	switch trimmed {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::", "0:0:0:0:0:0:0:0":
		return "::1"
	default:
		return trimmed
	}
}

// localStatusURL builds the Node's own local status URL from the configured
// bind address via net.JoinHostPort/url.URL so IPv6 binds stay well-formed.
func localStatusURL(host string, port int, path string) string {
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(normalizeLoopbackHost(host), strconv.Itoa(port)), Path: path}
	return u.String()
}

// quotaSnapshotEntry is the sanitized public shape shared by local and Hub
// checks. It intentionally contains no source credentials or raw command
// output.
type quotaSnapshotEntry struct {
	Provider     string            `json:"provider"`
	AccountKey   string            `json:"accountKey"`
	DisplayLabel string            `json:"displayLabel"`
	SourceStatus string            `json:"sourceStatus"`
	SampledAt    *time.Time        `json:"sampledAt"`
	Windows      []json.RawMessage `json:"windows"`
	ObservedBy   string            `json:"observedBy"`
}

// checkQuotaSnapshot probes the local public state. Unlike the Hub check it
// does not require all global accounts; every locally present observation must
// be structurally valid and at least one local observation must exist.
func checkQuotaSnapshot(ctx context.Context, cfg config.Config) string {
	return checkQuotaSnapshotAt(ctx, cfg, time.Now().UTC())
}

func checkQuotaSnapshotAt(ctx context.Context, cfg config.Config, now time.Time) string {
	endpoint := localStatusURL(cfg.Server.Host, cfg.Server.Port, "/api/state")
	entries, err := fetchQuotaEntries(ctx, endpoint)
	if err != nil {
		return "degraded"
	}
	return evaluateQuotaEntries(entries, false, now)
}

// checkHubQuotaCoverage verifies the Hub's global, deduplicated dashboard
// projection. Coverage is complete only when fresh observations for Codex A,
// Codex B, and GLM are all present and sourced from online/available hosts.
func checkHubQuotaCoverage(ctx context.Context, cfg config.Config) string {
	return checkHubQuotaCoverageAt(ctx, cfg, time.Now().UTC())
}

func checkHubQuotaCoverageAt(ctx context.Context, cfg config.Config, now time.Time) string {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Uplink.Endpoint), "/") + "/api/dashboard"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "degraded"
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "degraded"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "degraded"
	}
	var body struct {
		Hosts []struct {
			ConfiguredHostID string `json:"configuredHostId"`
			Source           struct {
				Status string `json:"status"`
			} `json:"source"`
		} `json:"hosts"`
		Quota []quotaSnapshotEntry `json:"quota"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&body); err != nil {
		return "degraded"
	}
	onlineHosts := make(map[string]bool, len(body.Hosts))
	for _, host := range body.Hosts {
		switch strings.ToLower(strings.TrimSpace(host.Source.Status)) {
		case "online", "available":
			onlineHosts[host.ConfiguredHostID] = true
		}
	}
	for _, item := range body.Quota {
		if strings.TrimSpace(item.ObservedBy) == "" || !onlineHosts[item.ObservedBy] {
			return "degraded"
		}
	}
	return evaluateQuotaEntries(body.Quota, true, now)
}

// quotaSourceHealthJSON is the sanitized machine-readable source-health slice
// of the public state: status plus the fixed-vocabulary reason slug only.
type quotaSourceHealthJSON struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// quotaPublicStateJSON is the bounded slice of /api/state the product reads:
// sanitized quota observations plus per-source status/reason. It never
// includes credentials or raw provider output.
type quotaPublicStateJSON struct {
	Quota   []quotaSnapshotEntry             `json:"quota"`
	Sources map[string]quotaSourceHealthJSON `json:"sources"`
}

func fetchQuotaPublicState(ctx context.Context, endpoint string) (quotaPublicStateJSON, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return quotaPublicStateJSON{}, err
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return quotaPublicStateJSON{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return quotaPublicStateJSON{}, fmt.Errorf("quota endpoint status %d", response.StatusCode)
	}
	var body quotaPublicStateJSON
	if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&body); err != nil {
		return quotaPublicStateJSON{}, err
	}
	return body, nil
}

func fetchQuotaEntries(ctx context.Context, endpoint string) ([]quotaSnapshotEntry, error) {
	state, err := fetchQuotaPublicState(ctx, endpoint)
	return state.Quota, err
}

func evaluateQuotaEntries(entries []quotaSnapshotEntry, requireGlobal bool, now time.Time) string {
	if len(entries) == 0 {
		return "pending"
	}
	seen := make(map[string]bool, len(entries))
	seenCoverage := make(map[string]bool, len(entries))
	wanted := map[string]bool{"codex\x00Codex A": false, "codex\x00Codex B": false, "zai\x00GLM": false}
	for _, item := range entries {
		provider := strings.ToLower(strings.TrimSpace(item.Provider))
		label := strings.TrimSpace(item.DisplayLabel)
		identity := provider + "\x00" + item.AccountKey
		if !validQuotaAccountKey(item.AccountKey) || (provider != "codex" && provider != "zai") || (provider == "codex" && label != "Codex A" && label != "Codex B") || (provider == "zai" && label != "GLM") || seen[identity] || strings.ToLower(strings.TrimSpace(item.SourceStatus)) != "available" || item.SampledAt == nil || len(item.Windows) == 0 {
			return "degraded"
		}
		age := now.Sub(item.SampledAt.UTC())
		if age < 0 || age > quota.LastGoodTTL {
			return "degraded"
		}
		seen[identity] = true
		coverage := provider + "\x00" + label
		if seenCoverage[coverage] {
			return "degraded"
		}
		seenCoverage[coverage] = true
		if _, required := wanted[coverage]; requireGlobal && (!required || wanted[coverage]) {
			return "degraded"
		} else if requireGlobal {
			wanted[coverage] = true
		}
	}
	if requireGlobal {
		for _, covered := range wanted {
			if !covered {
				return "degraded"
			}
		}
	}
	return "complete"
}

func normalizeQuotaStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete":
		return "complete"
	case "pending":
		return "pending"
	default:
		return "degraded"
	}
}

func validQuotaAccountKey(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != len("acct_")+32 || !strings.HasPrefix(value, "acct_") {
		return false
	}
	for _, char := range value[len("acct_"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func onboardingAggregateStatus(phases []onboardingPhase) string {
	status := "complete"
	for _, phase := range phases {
		switch phase.Status {
		case "degraded", "quota_not_configured":
			return "degraded"
		case "pending":
			status = "pending"
		}
	}
	return status
}

func onboardingCheckResult(phases []onboardingPhase, extra map[string]any) operationResult {
	status := onboardingAggregateStatus(phases)
	data := onboardingResultData(phases, extra, status)
	if status == "complete" {
		return okResult("onboarding_check_complete", "Node onboarding check completed", data)
	}
	return errorResult("onboarding_check_"+status, "Node onboarding check requires attention", data)
}

func onboardingInstallResult(phases []onboardingPhase, extra map[string]any) operationResult {
	status := onboardingAggregateStatus(phases)
	data := onboardingResultData(phases, extra, status)
	if status == "complete" {
		return okResult("onboarding_complete", "Node onboarding completed and Hub closed the loop", data)
	}
	return errorResult("onboarding_"+status, "Node installation/configuration completed; Hub data closure is "+status, data)
}

func onboardingResultData(phases []onboardingPhase, extra map[string]any, closureStatus string) map[string]any {
	data := map[string]any{
		"phases":             phases,
		"installationStatus": "complete",
		"closureStatus":      closureStatus,
	}
	for key, value := range extra {
		data[key] = value
	}
	return data
}

func localSocketStatus() string {
	paths, err := agent.ResolveRuntimePaths()
	if err != nil {
		return "unavailable"
	}
	info, err := os.Stat(paths.Socket)
	if err != nil {
		return "unavailable"
	}
	if info.Mode()&os.ModeSocket == 0 {
		return "invalid"
	}
	return "ready"
}

func checkLocalUplink(ctx context.Context, cfg config.Config) string {
	if !cfg.Uplink.Enabled || cfg.Uplink.Token == "" || cfg.Uplink.Endpoint == "" {
		return "degraded"
	}
	endpoint := localStatusURL(cfg.Server.Host, cfg.Server.Port, "/api/node/status")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "degraded"
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "degraded"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "degraded"
	}
	var body struct {
		NodeID          string     `json:"nodeId"`
		UplinkEnabled   bool       `json:"uplinkEnabled"`
		TokenConfigured bool       `json:"tokenConfigured"`
		UplinkRunning   bool       `json:"uplinkRunning"`
		Connected       bool       `json:"connected"`
		LastSuccessAt   *time.Time `json:"lastSuccessAt"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil {
		return "degraded"
	}
	if body.UplinkEnabled && body.TokenConfigured && body.UplinkRunning && body.Connected && body.LastSuccessAt != nil {
		return "complete"
	}
	if body.UplinkRunning && body.LastSuccessAt == nil {
		return "pending"
	}
	return "degraded"
}

func checkHubNodeOnline(ctx context.Context, cfg config.Config, nodeID string) string {
	endpoint := strings.TrimRight(cfg.Uplink.Endpoint, "/") + "/api/dashboard"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "degraded"
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "degraded"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "degraded"
	}
	var body struct {
		Hosts []struct {
			ConfiguredHostID string `json:"configuredHostId"`
			Source           struct {
				Status string `json:"status"`
			} `json:"source"`
		} `json:"hosts"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 256<<10))
	if err := decoder.Decode(&body); err != nil {
		return "degraded"
	}
	for _, host := range body.Hosts {
		if host.ConfiguredHostID != nodeID {
			continue
		}
		if host.Source.Status == "online" {
			return "complete"
		}
		if host.Source.Status == "stale" {
			return "degraded"
		}
		return "degraded"
	}
	return "pending"
}

func onboardingFailure(phases []onboardingPhase, status, message string) operationResult {
	return errorResult(status, message, map[string]any{"phases": phases})
}

func registerNode(ctx context.Context, endpoint, adminToken, nodeID, displayName string) (string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/admin/api/v1/nodes"
	body, err := json.Marshal(map[string]string{"nodeId": nodeID, "displayName": displayName})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 32<<10)
	var decoded struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(limited).Decode(&decoded); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || !decoded.OK || decoded.Token == "" {
		return "", fmt.Errorf("Hub registration rejected")
	}
	return decoded.Token, nil
}

func readSecretFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("secret file unreadable")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("secret file empty")
	}
	return value, nil
}

// ReadSecretFileForCLI keeps the command entry point from ever accepting a
// credential as a process-list argument. It returns only the in-memory value;
// callers must not include it in Result/Data or logs.
func ReadSecretFileForCLI(path string) (string, error) { return readSecretFile(path) }
