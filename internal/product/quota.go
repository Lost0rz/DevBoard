package product

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/quota"
)

// QuotaAssignment is the sanitized account-key -> editable display-name choice
// made by the macOS UI. It never contains source account identity material.
type QuotaAssignment struct {
	AccountKey string
	Label      string
}

// QuotaCommandOptions keeps the product command testable without touching a
// real home directory or invoking CodexBar. Production uses the current user
// home, the local atomic config writer, and the bounded runner.
type QuotaCommandOptions struct {
	Home        string
	ConfigPath  string
	Assignments []QuotaAssignment
	Runner      quota.Runner
	SaveConfig  func(string, config.Config) error
	// CLIResolve overrides the absolute CodexBar CLI resolution for tests.
	// Production resolves the same well-known absolute paths the LaunchAgent
	// collector uses, so a missing CLI is reported as its own state instead
	// of a generic quota failure.
	CLIResolve func() (string, error)
}

func quotaCLIResolve(opt func() (string, error)) func() (string, error) {
	if opt != nil {
		return opt
	}
	return quota.ResolveCodexBarCLI
}

// gateCodexBarCLI returns the distinct "CodexBar CLI unavailable" result when
// the read-only CLI cannot be resolved. The message never carries an install
// path or provider output.
func gateCodexBarCLI(resolve func() (string, error)) *operationResult {
	if _, err := resolve(); err == nil {
		return nil
	}
	result := errorResult("quota_cli_unavailable", "CodexBar CLI is unavailable on this Mac", map[string]any{"cliAvailable": false})
	return &result
}

// RunQuotaCommand implements the product-facing status/detect/configure
// workflow. Each result is schema-v1 and contains only bounded sanitized data.
func RunQuotaCommand(action string, opts QuotaCommandOptions) Result {
	return resultValue(runQuotaCommand(action, opts))
}

func runQuotaCommand(action string, opts QuotaCommandOptions) operationResult {
	paths, err := ResolvePaths(opts.Home)
	if err != nil {
		return errorResult("quota_unavailable", "Quota product paths are unavailable", nil)
	}
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = paths.Config
	}
	cfg, err := loadProductConfig(configPath)
	if err != nil {
		return errorResult("quota_config_invalid", "Local DevBoard configuration could not be read", nil)
	}

	switch action {
	case "status":
		statusCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return quotaStatus(statusCtx, cfg)
	case "detect":
		key, cfg, err := ensureProductIdentity(paths, configPath, cfg, opts.SaveConfig)
		if err != nil {
			return errorResult("quota_identity_unavailable", "Quota identity could not be prepared", nil)
		}
		if gated := gateCodexBarCLI(quotaCLIResolve(opts.CLIResolve)); gated != nil {
			return *gated
		}
		return quotaDetection(opts.Runner, key, cfg.Quota.AccountAliases)
	case "configure":
		key, cfg, err := ensureProductIdentity(paths, configPath, cfg, opts.SaveConfig)
		if err != nil {
			return errorResult("quota_identity_unavailable", "Quota identity could not be prepared", nil)
		}
		if gated := gateCodexBarCLI(quotaCLIResolve(opts.CLIResolve)); gated != nil {
			return *gated
		}
		return configureQuota(opts, configPath, cfg, key)
	default:
		return errorResult("invalid_command", "quota action must be status, detect, or configure", nil)
	}
}

func loadProductConfig(path string) (config.Config, error) {
	cfg := config.Defaults()
	existing, err := config.Load(path)
	if err == nil {
		return existing, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	return config.Config{}, err
}

func ensureProductIdentity(paths Paths, configPath string, cfg config.Config, save func(string, config.Config) error) ([]byte, config.Config, error) {
	if configured := strings.TrimSpace(cfg.Quota.IdentityKeyFile); configured != "" {
		key, err := quota.LoadIdentityKey(configured)
		return key, cfg, err
	}
	key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
	if err != nil {
		return nil, cfg, err
	}
	cfg.Quota.IdentityKeyFile = paths.QuotaIdentityKey
	if save == nil {
		save = config.SaveAtomic
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return nil, cfg, err
	}
	if err := save(configPath, cfg); err != nil {
		return nil, cfg, err
	}
	return key, cfg, nil
}

func quotaStatus(ctx context.Context, cfg config.Config) operationResult {
	path := strings.TrimSpace(cfg.Quota.IdentityKeyFile)
	if path == "" {
		return errorResult("quota_not_configured", "Quota setup has not generated a local identity yet", map[string]any{
			"identityReady":      false,
			"configurationReady": false,
			"aliasCount":         0,
		})
	}
	if _, err := quota.LoadIdentityKey(path); err != nil {
		return errorResult("quota_unavailable", "Quota identity requires product repair", map[string]any{"identityReady": false, "configurationReady": false})
	}
	aliases, err := quota.ParseAliases(cfg.Quota.AccountAliases)
	if err != nil || len(aliases) == 0 {
		return errorResult("quota_configuration_required", "Choose a unique display name for each detected account", map[string]any{
			"identityReady":      true,
			"configurationReady": false,
			"aliasCount":         0,
		})
	}
	labels := make([]string, 0, len(aliases))
	for _, label := range aliases {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	// Configuration readiness is not source availability. Read the existing
	// local sanitized snapshot only; this path never invokes CodexBar.
	snapshotStatus, cliUnavailable := productQuotaSnapshotHealth(ctx, cfg)
	data := map[string]any{
		"identityReady":      true,
		"configurationReady": true,
		"aliasCount":         len(aliases),
		"labels":             labels,
		"freshness":          snapshotStatus,
		"cliAvailable":       !cliUnavailable,
	}
	if cliUnavailable {
		// The Node's own source health reports the CLI as missing; surfacing
		// a generic degraded/unavailable here would hide the actionable fix.
		return errorResult("quota_cli_unavailable", "CodexBar CLI is unavailable on this Mac", data)
	}
	switch snapshotStatus {
	case "complete":
		return okResult("quota_available", "Quota snapshot is available", data)
	case "unavailable":
		return errorResult("quota_unavailable", "Quota has no local snapshot", data)
	default:
		return errorResult("quota_degraded", "Quota snapshot is stale or degraded", data)
	}
}

// productQuotaSnapshotStatus reads only the bounded, redacted local public
// state endpoint. It keeps unavailable (no endpoint/no snapshot) distinct
// from degraded (a retained or stale source) without invoking CodexBar.
func productQuotaSnapshotStatus(ctx context.Context, cfg config.Config) string {
	status, _ := productQuotaSnapshotHealth(ctx, cfg)
	return status
}

// productQuotaSnapshotHealth extends productQuotaSnapshotStatus with the
// Node's machine-readable quota source reason. cliUnavailable is true only
// when the Node itself reported the CodexBar CLI as missing.
func productQuotaSnapshotHealth(ctx context.Context, cfg config.Config) (string, bool) {
	endpoint := localStatusURL(cfg.Server.Host, cfg.Server.Port, "/api/state")
	state, err := fetchQuotaPublicState(ctx, endpoint)
	if err != nil {
		return "unavailable", false
	}
	cliUnavailable := state.Sources["quota"].Reason == "cli_unavailable"
	// The single-node Mac A product does not accept the partial-local
	// semantics used by historical multi-host onboarding: all three frozen
	// labels must be present in one current snapshot.
	switch evaluateQuotaEntries(state.Quota, true, time.Now().UTC()) {
	case "complete":
		return "complete", cliUnavailable
	case "pending":
		return "unavailable", cliUnavailable
	default:
		return "degraded", cliUnavailable
	}
}

func quotaDetection(runner quota.Runner, key []byte, aliasSpec string) operationResult {
	if runner == nil {
		runner = quota.DefaultRunner()
	}
	aliases, err := quota.ParseAliases(aliasSpec)
	if err != nil {
		aliases = map[string]string{}
	}
	detection, err := quota.DetectAccounts(context.Background(), runner, key, aliases)
	if err != nil {
		return errorResult("quota_unavailable", "Quota detection could not start", nil)
	}
	accounts := make([]map[string]any, 0, len(detection.Accounts))
	codexConfigRequired := false
	for _, account := range detection.Accounts {
		if account.Provider == "codex" && account.DisplayLabel == "" {
			codexConfigRequired = true
		}
		accounts = append(accounts, map[string]any{
			"provider":     account.Provider,
			"accountKey":   account.AccountKey,
			"displayLabel": account.DisplayLabel,
			"sourceHealth": account.SourceHealth,
		})
	}
	data := map[string]any{"accounts": accounts, "sources": detection.Sources, "identityReady": true}
	profile := macAProfile(detection)
	codexHealth, codexOK := detection.Sources["codex"]
	zaiHealth, zaiOK := detection.Sources["zai"]
	if codexConfigRequired || codexHealth == "configuration_required" {
		return errorResult("quota_configuration_required", "Choose a unique display name for each detected account", data)
	}
	if zaiHealth == "configuration_required" {
		return errorResult("quota_configuration_required", "Quota labels cannot uniquely cover the detected GLM accounts", data)
	}
	if codexHealth == "unavailable" && zaiHealth == "unavailable" {
		return errorResult("quota_unavailable", "CodexBar quota sources are unavailable", data)
	}
	if codexOK && zaiOK && codexHealth == "available" && zaiHealth == "available" && profile.complete {
		return okResult("quota_detected", "Quota sources detected", data)
	}
	if (codexOK && codexHealth != "unavailable") || (zaiOK && zaiHealth != "unavailable") {
		return errorResult("quota_degraded", "One or more quota sources are unavailable", data)
	}
	if profile.configurationRequired {
		return errorResult("quota_configuration_required", profile.message, data)
	}
	return errorResult("quota_unavailable", "CodexBar quota sources are unavailable", data)
}

func configureQuota(opts QuotaCommandOptions, configPath string, cfg config.Config, key []byte) operationResult {
	if opts.Runner == nil {
		opts.Runner = quota.DefaultRunner()
	}
	aliases, err := quota.ParseAliases(cfg.Quota.AccountAliases)
	if err != nil {
		aliases = map[string]string{}
	}
	detection, err := quota.DetectAccounts(context.Background(), opts.Runner, key, aliases)
	if err != nil {
		return errorResult("quota_unavailable", "Quota detection could not start", nil)
	}
	profile := macAProfile(detection)
	if detection.Sources["codex"] != "available" {
		return errorResult("quota_unavailable", "Required quota providers are unavailable", map[string]any{"sources": detection.Sources})
	}
	if detection.Sources["zai"] == "configuration_required" {
		return errorResult("quota_configuration_required", "Quota labels cannot uniquely cover the detected GLM accounts", map[string]any{"sources": detection.Sources})
	}
	if detection.Sources["zai"] != "available" {
		return errorResult("quota_unavailable", "Required quota providers are unavailable", map[string]any{"sources": detection.Sources})
	}
	if !profile.shapeComplete {
		return errorResult("quota_configuration_required", profile.message, map[string]any{"sources": detection.Sources, "codexCount": profile.codexCount, "glmCount": profile.glmCount})
	}
	current := map[string]struct{}{}
	required := map[string]struct{}{}
	for _, account := range detection.Accounts {
		current[account.AccountKey] = struct{}{}
		if account.Provider == "codex" {
			required[account.AccountKey] = struct{}{}
		}
	}
	if err := validateAssignmentsForAccounts(opts.Assignments, current, required); err != nil {
		return errorResult("quota_configuration_required", err.Error(), map[string]any{"accountCount": len(current)})
	}
	entries := make([]string, 0, len(opts.Assignments))
	for _, assignment := range opts.Assignments {
		entries = append(entries, strings.TrimSpace(assignment.AccountKey)+"="+strings.TrimSpace(assignment.Label))
	}
	sort.Strings(entries)
	// Keep the historical CLI contract compatible: omitting the sole Z.ai
	// assignment retains the default GLM presentation label. The native UI
	// sends all detected accounts, so a custom Z.ai name is persisted too.
	provided := make(map[string]bool, len(opts.Assignments))
	for _, assignment := range opts.Assignments {
		provided[strings.TrimSpace(assignment.AccountKey)] = true
	}
	for _, account := range detection.Accounts {
		if account.Provider != "zai" || provided[account.AccountKey] {
			continue
		}
		for _, assignment := range opts.Assignments {
			if strings.EqualFold(strings.TrimSpace(assignment.Label), "GLM") {
				return errorResult("quota_configuration_required", "Display names must be unique across all detected accounts; rename the Codex account or the GLM account", nil)
			}
		}
		if label := aliases[account.AccountKey]; label != "" {
			entries = append(entries, account.AccountKey+"="+label)
		}
	}
	sort.Strings(entries)
	spec := strings.Join(entries, ",")
	parsed, err := quota.ParseAliases(spec)
	if err != nil || len(parsed) < len(required) {
		return errorResult("quota_configuration_required", "Display names must be unique and cover every detected Codex account", nil)
	}
	cfg.Quota.AccountAliases = spec
	save := opts.SaveConfig
	if save == nil {
		save = config.SaveAtomic
	}
	if err := save(configPath, cfg); err != nil {
		return errorResult("quota_config_write_failed", "Quota configuration could not be saved", nil)
	}
	// The running Node loads the quota collector at startup; after the first
	// Quota Setup the background Node must be restarted (More → Restart) to
	// begin collecting.
	return okResult("quota_configured", "Quota account labels saved. Restart the background Node to start quota collection.", map[string]any{
		"accountCount":    len(parsed),
		"labels":          aliasLabels(parsed),
		"restartRequired": true,
	})
}

func validateAssignments(assignments []QuotaAssignment, current map[string]struct{}) error {
	if len(current) == 0 || len(assignments) == 0 {
		return fmt.Errorf("Display names require at least one detected account")
	}
	return validateAssignmentsForAccounts(assignments, current, current)
}

func validateAssignmentsForAccounts(assignments []QuotaAssignment, current, required map[string]struct{}) error {
	if len(current) == 0 || len(assignments) < len(required) || len(assignments) > len(current) {
		return fmt.Errorf("Display names must cover every detected Codex account")
	}
	seenKeys := map[string]bool{}
	seenLabels := map[string]bool{}
	for _, assignment := range assignments {
		key := strings.TrimSpace(assignment.AccountKey)
		label := strings.TrimSpace(assignment.Label)
		if key == "" || label == "" {
			return fmt.Errorf("Quota account labels are incomplete")
		}
		if _, ok := current[key]; !ok {
			return fmt.Errorf("Quota account selection is no longer current")
		}
		if seenKeys[key] {
			return fmt.Errorf("Quota account selections must be unique")
		}
		if !quota.ValidateAliasLabel(label) {
			return fmt.Errorf("Display name is invalid or too long")
		}
		if seenLabels[label] {
			return fmt.Errorf("Quota labels must be unique")
		}
		seenKeys[key] = true
		seenLabels[label] = true
	}
	for key := range required {
		if !seenKeys[key] {
			return fmt.Errorf("Display names must cover every detected Codex account")
		}
	}
	return nil
}

type macAProfileResult struct {
	complete              bool
	shapeComplete         bool
	configurationRequired bool
	codexCount            int
	glmCount              int
	message               string
}

// macAProfile is the product-level acceptance profile. The lower quota
// collector remains provider-isolated, while the Mac A product surface is
// available only when both Codex accounts and the single GLM account are
// simultaneously detected and covered.
func macAProfile(detection quota.Detection) macAProfileResult {
	result := macAProfileResult{message: "Mac quota setup requires two Codex accounts and one GLM account."}
	labels := map[string]bool{}
	for _, account := range detection.Accounts {
		switch account.Provider {
		case "codex":
			result.codexCount++
		case "zai":
			result.glmCount++
		}
		if account.DisplayLabel != "" {
			labels[account.DisplayLabel] = true
		}
	}
	result.configurationRequired = true
	if result.codexCount == 0 || result.glmCount == 0 {
		result.message = "Mac A quota setup requires two Codex accounts and one GLM account."
		return result
	}
	if result.codexCount != 2 || result.glmCount != 1 {
		result.message = "Mac A quota setup requires exactly two Codex accounts and one GLM account."
		return result
	}
	result.shapeComplete = true
	if len(labels) != len(detection.Accounts) {
		result.message = "Choose a unique display name for each detected account."
		return result
	}
	result.complete = true
	result.configurationRequired = false
	result.message = "Mac A quota sources are configured and covered."
	return result
}

func aliasLabels(aliases map[string]string) []string {
	labels := make([]string, 0, len(aliases))
	for _, label := range aliases {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}
