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

// QuotaAssignment is the sanitized account-key -> allow-listed label choice
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
		return quotaDetection(opts.Runner, key, cfg.Quota.AccountAliases)
	case "configure":
		key, cfg, err := ensureProductIdentity(paths, configPath, cfg, opts.SaveConfig)
		if err != nil {
			return errorResult("quota_identity_unavailable", "Quota identity could not be prepared", nil)
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
		return errorResult("quota_configuration_required", "Choose a unique label for each detected Codex account", map[string]any{
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
	snapshotStatus := productQuotaSnapshotStatus(ctx, cfg)
	data := map[string]any{
		"identityReady":      true,
		"configurationReady": true,
		"aliasCount":         len(aliases),
		"labels":             labels,
		"freshness":          snapshotStatus,
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
	endpoint := localStatusURL(cfg.Server.Host, cfg.Server.Port, "/api/state")
	entries, err := fetchQuotaEntries(ctx, endpoint)
	if err != nil {
		return "unavailable"
	}
	// The single-node Mac A product does not accept the partial-local
	// semantics used by historical multi-host onboarding: all three frozen
	// labels must be present in one current snapshot.
	switch evaluateQuotaEntries(entries, true, time.Now().UTC()) {
	case "complete":
		return "complete"
	case "pending":
		return "unavailable"
	default:
		return "degraded"
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
		return errorResult("quota_configuration_required", "Choose a unique label for each detected Codex account", data)
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
	for _, account := range detection.Accounts {
		if account.Provider == "codex" {
			current[account.AccountKey] = struct{}{}
		}
	}
	if err := validateAssignments(opts.Assignments, current); err != nil {
		return errorResult("quota_configuration_required", err.Error(), map[string]any{"accountCount": len(current)})
	}
	entries := make([]string, 0, len(opts.Assignments))
	for _, assignment := range opts.Assignments {
		entries = append(entries, strings.TrimSpace(assignment.AccountKey)+"="+strings.TrimSpace(assignment.Label))
	}
	sort.Strings(entries)
	spec := strings.Join(entries, ",")
	parsed, err := quota.ParseAliases(spec)
	if err != nil || len(parsed) != len(current) {
		return errorResult("quota_configuration_required", "Quota labels must be unique and cover every detected Codex account", nil)
	}
	cfg.Quota.AccountAliases = spec
	save := opts.SaveConfig
	if save == nil {
		save = config.SaveAtomic
	}
	if err := save(configPath, cfg); err != nil {
		return errorResult("quota_config_write_failed", "Quota configuration could not be saved", nil)
	}
	return okResult("quota_configured", "Quota account labels saved", map[string]any{
		"accountCount": len(parsed),
		"labels":       aliasLabels(parsed),
	})
}

func validateAssignments(assignments []QuotaAssignment, current map[string]struct{}) error {
	if len(current) == 0 || len(assignments) == 0 {
		return fmt.Errorf("Quota labels require at least one detected Codex account")
	}
	if len(assignments) != len(current) {
		return fmt.Errorf("Quota labels must cover every detected Codex account")
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
		if label != "Codex A" && label != "Codex B" {
			return fmt.Errorf("Quota labels must use Codex A or Codex B for Codex accounts")
		}
		if seenLabels[label] {
			return fmt.Errorf("Quota labels must be unique")
		}
		seenKeys[key] = true
		seenLabels[label] = true
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
	result := macAProfileResult{message: "Mac A quota setup requires Codex A, Codex B, and GLM."}
	labels := map[string]bool{}
	for _, account := range detection.Accounts {
		switch account.Provider {
		case "codex":
			result.codexCount++
			labels[account.DisplayLabel] = true
		case "zai":
			result.glmCount++
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
	if !labels["Codex A"] || !labels["Codex B"] {
		result.message = "Choose unique Codex A and Codex B labels for both detected Codex accounts."
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
