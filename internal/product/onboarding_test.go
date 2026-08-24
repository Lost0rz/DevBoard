package product

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/quota"
)

const onboardingToken = "abcdefghijklmnopqrstuvwxyz012345"

func TestNodeOnboardingDryRunIsMachineReadableAndDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: path, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test",
		NodeToken: onboardingToken, DryRun: true,
		Service:     func(string) operationResult { t.Fatal("dry-run called service"); return operationResult{} },
		Integration: func(string, string) operationResult { t.Fatal("dry-run called integration"); return operationResult{} },
	})
	if !result.OK || result.Status != "onboarding_dry_run" {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config: %v", err)
	}
	body := resultValue(result)
	if strings.Contains(string(mustJSON(body)), onboardingToken) {
		t.Fatal("onboarding result leaked Node token")
	}
}

func TestNodeOnboardingQuotaFilesAreValidatedAndPersistedWithoutCopyingSecrets(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "shared.identity.key")
	key := []byte("shared-test-identity-key-32-bytes-long")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	keyA := quota.AccountKey(key, "codex", "fixture-account-a")
	keyB := quota.AccountKey(key, "codex", "fixture-account-b")
	aliasPath := filepath.Join(dir, "account.aliases")
	aliasBody := keyB + "=Codex B\n" + keyA + "=Codex A\n"
	if err := os.WriteFile(aliasPath, []byte(aliasBody), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "node.yaml")
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: configPath, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test", NodeToken: onboardingToken,
		QuotaIdentityKeyFile: keyPath, QuotaAliasFile: aliasPath,
		Service:       func(string) operationResult { return okResult("ok", "", nil) },
		Integration:   func(string, string) operationResult { return okResult("ok", "", nil) },
		SocketCheck:   func() string { return "ready" },
		UplinkCheck:   func(context.Context, config.Config) string { return "complete" },
		HubCheck:      func(context.Context, config.Config, string) string { return "complete" },
		QuotaRunner:   stubRunner{fails: true},
		QuotaSnapshot: func(context.Context, config.Config) string { return "pending" },
		HubQuotaCheck: func(context.Context, config.Config) string { return "pending" },
	})
	if result.OK || result.Status != "onboarding_degraded" {
		t.Fatalf("result=%+v", result)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := quota.LoadAliasFile(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Quota.IdentityKeyFile != keyPath || cfg.Quota.AccountAliases != canonical {
		t.Fatalf("quota config=%+v", cfg.Quota)
	}
	body := string(mustJSON(resultValue(result)))
	if strings.Contains(body, string(key)) || strings.Contains(body, aliasBody) || strings.Contains(body, "fixture-account") {
		t.Fatalf("onboarding result leaked quota secret or raw identity: %s", body)
	}
	gotAliasBody, err := os.ReadFile(aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAliasBody) != aliasBody {
		t.Fatalf("onboarding modified alias source: %q", gotAliasBody)
	}
}

func TestNodeOnboardingQuotaDryRunOnlyReadsSecurityFiles(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "shared.identity.key")
	key := []byte("shared-test-identity-key-32-bytes-long")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(dir, "account.aliases")
	alias := quota.AccountKey(key, "codex", "fixture-account-a") + "=Codex A"
	if err := os.WriteFile(aliasPath, []byte(alias), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "node.yaml")
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: configPath, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test", NodeToken: onboardingToken,
		QuotaIdentityKeyFile: keyPath, QuotaAliasFile: aliasPath, DryRun: true,
		Service:     func(string) operationResult { t.Fatal("dry-run called service"); return operationResult{} },
		Integration: func(string, string) operationResult { t.Fatal("dry-run called integration"); return operationResult{} },
	})
	if !result.OK || result.Status != "onboarding_dry_run" {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config: %v", err)
	}
	if got, err := os.ReadFile(aliasPath); err != nil || string(got) != alias {
		t.Fatalf("dry-run changed alias source: %q err=%v", got, err)
	}
}

func TestNodeOnboardingQuotaInvalidFileFailsBeforeAnyWriteOrInstall(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.identity.key")
	if err := os.WriteFile(keyPath, []byte("too-short"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "node.yaml")
	serviceCalled := false
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: configPath, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test", NodeToken: onboardingToken,
		QuotaIdentityKeyFile: keyPath,
		Service:              func(string) operationResult { serviceCalled = true; return okResult("ok", "", nil) },
		Integration:          func(string, string) operationResult { serviceCalled = true; return okResult("ok", "", nil) },
	})
	if result.OK || result.Status != "quota_identity_key_invalid" || serviceCalled {
		t.Fatalf("invalid quota key result=%+v serviceCalled=%v", result, serviceCalled)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("invalid quota key wrote config: %v", err)
	}
}

func TestNodeOnboardingRegistersAndMergesBothHooksIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	serviceCalls := []string{}
	integrationCalls := []string{}
	registered := 0
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: path, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test", AdminToken: "admin-secret",
		Register: func(_ context.Context, endpoint, admin, nodeID, display string) (string, error) {
			registered++
			if endpoint != "https://hub.example.test" || admin != "admin-secret" || nodeID != "mac-b" || display != "Laptop" {
				t.Fatalf("registration args")
			}
			return onboardingToken, nil
		},
		Service: func(action string) operationResult {
			serviceCalls = append(serviceCalls, action)
			return okResult("ok", "", nil)
		},
		Integration: func(provider, action string) operationResult {
			integrationCalls = append(integrationCalls, provider+":"+action)
			return okResult("ok", "", nil)
		},
		SocketCheck: func() string { return "ready" },
		UplinkCheck: func(context.Context, config.Config) string { return "complete" },
		HubCheck:    func(context.Context, config.Config, string) string { return "complete" },
	})
	if result.OK || result.Status != "onboarding_degraded" || registered != 1 {
		t.Fatalf("result=%+v registered=%d", result, registered)
	}
	if data := result.Data; data["installationStatus"] != "complete" || data["closureStatus"] != "degraded" {
		t.Fatalf("installation/closure status=%v", data)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host.ID != "mac-b" || cfg.Uplink.Endpoint != "https://hub.example.test" || cfg.Uplink.Token != onboardingToken || !cfg.Uplink.Enabled {
		t.Fatalf("saved config=%+v", cfg)
	}
	if strings.Join(serviceCalls, ",") != "install" || strings.Join(integrationCalls, ",") != "codex:install,claude-code:install" {
		t.Fatalf("calls service=%v integrations=%v", serviceCalls, integrationCalls)
	}

	check := runNodeOnboarding(OnboardingOptions{
		ConfigPath: path, Check: true,
		Service: func(action string) operationResult {
			if action != "status" {
				t.Fatalf("check service=%s", action)
			}
			return okResult("ok", "", nil)
		},
		Integration: func(provider, action string) operationResult {
			if action != "status" {
				t.Fatalf("check integration=%s", action)
			}
			return okResult("ok", "", nil)
		},
		SocketCheck: func() string { return "ready" },
		UplinkCheck: func(context.Context, config.Config) string { return "complete" },
		HubCheck:    func(context.Context, config.Config, string) string { return "complete" },
	})
	if check.OK || check.Status != "onboarding_check_degraded" {
		t.Fatalf("check=%+v", check)
	}
}

func TestNodeOnboardingReportsConcreteRegistrationFailurePhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	result := runNodeOnboarding(OnboardingOptions{
		ConfigPath: path, NodeID: "mac-b", DisplayName: "Laptop", HubEndpoint: "https://hub.example.test", AdminToken: "admin-secret",
		Register: func(context.Context, string, string, string, string) (string, error) { return "", os.ErrPermission },
	})
	if result.OK || result.Status != "hub_registration_failed" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(string(mustJSON(resultValue(result))), "hub_registration") {
		t.Fatal("registration phase missing")
	}
}

func TestNodeOnboardingCheckFailureMatrixNeverClaimsComplete(t *testing.T) {
	testCases := []struct {
		name       string
		serviceOK  bool
		codexOK    bool
		claudeOK   bool
		socket     string
		uplink     string
		hub        string
		wantStatus string
	}{
		{name: "launch agent", serviceOK: false, codexOK: true, claudeOK: true, socket: "ready", uplink: "complete", hub: "complete", wantStatus: "onboarding_check_degraded"},
		{name: "codex hook", serviceOK: true, codexOK: false, claudeOK: true, socket: "ready", uplink: "complete", hub: "complete", wantStatus: "onboarding_check_degraded"},
		{name: "claude hook", serviceOK: true, codexOK: true, claudeOK: false, socket: "ready", uplink: "complete", hub: "complete", wantStatus: "onboarding_check_degraded"},
		{name: "unix socket", serviceOK: true, codexOK: true, claudeOK: true, socket: "unavailable", uplink: "complete", hub: "complete", wantStatus: "onboarding_check_degraded"},
		{name: "uplink first snapshot", serviceOK: true, codexOK: true, claudeOK: true, socket: "ready", uplink: "pending", hub: "complete", wantStatus: "onboarding_check_degraded"},
		{name: "hub registry", serviceOK: true, codexOK: true, claudeOK: true, socket: "ready", uplink: "complete", hub: "degraded", wantStatus: "onboarding_check_degraded"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "node.yaml")
			cfg := config.Defaults()
			cfg.Host.ID, cfg.Host.DisplayName = "mac-b", "Laptop"
			cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
			if err := config.SaveAtomic(path, cfg); err != nil {
				t.Fatal(err)
			}
			result := runNodeOnboarding(OnboardingOptions{
				ConfigPath: path, Check: true,
				Service: func(string) operationResult {
					if tc.serviceOK {
						return okResult("healthy", "", nil)
					}
					return errorResult("not_running", "", nil)
				},
				Integration: func(provider, _ string) operationResult {
					if (provider == integrationCodex && tc.codexOK) || (provider == integrationClaude && tc.claudeOK) {
						return okResult("configured", "", nil)
					}
					return errorResult("not_configured", "", nil)
				},
				SocketCheck: func() string { return tc.socket },
				UplinkCheck: func(context.Context, config.Config) string { return tc.uplink },
				HubCheck:    func(context.Context, config.Config, string) string { return tc.hub },
			})
			if result.OK || result.Status != tc.wantStatus {
				t.Fatalf("result=%+v want status=%q", result, tc.wantStatus)
			}
			if strings.Contains(string(mustJSON(resultValue(result))), "onboarding_check_complete") {
				t.Fatal("failed check claimed complete")
			}
		})
	}
}

func mustJSON(value any) []byte {
	// Tests only need a stable leak check; JSON marshaling cannot fail for the
	// bounded Result shape.
	body, _ := json.Marshal(value)
	return body
}
