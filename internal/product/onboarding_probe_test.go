package product

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/quota"
)

func TestLocalStatusURLBuildsJoinHostPortAndNormalizesWildcards(t *testing.T) {
	cases := map[string]struct {
		host string
		port int
		want string
	}{
		"ipv4":               {host: "127.0.0.1", port: 8787, want: "http://127.0.0.1:8787/api/node/status"},
		"wildcard ipv4":      {host: "0.0.0.0", port: 9000, want: "http://127.0.0.1:9000/api/node/status"},
		"ipv6 loopback":      {host: "::1", port: 8787, want: "http://[::1]:8787/api/node/status"},
		"bracketed ipv6":     {host: "[::1]", port: 8787, want: "http://[::1]:8787/api/node/status"},
		"wildcard ipv6":      {host: "::", port: 8787, want: "http://[::1]:8787/api/node/status"},
		"bracketed wildcard": {host: "[::]", port: 80, want: "http://[::1]:80/api/node/status"},
	}
	for name, tc := range cases {
		if got := localStatusURL(tc.host, tc.port, "/api/node/status"); got != tc.want {
			t.Fatalf("%s: localStatusURL=%q want %q", name, got, tc.want)
		}
	}
}

func nodeStatusHandler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/node/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"nodeId":"mac-b","uplinkEnabled":true,"tokenConfigured":true,"uplinkRunning":true,"connected":true,"lastSuccessAt":%q}`, time.Now().UTC().Format(time.RFC3339))
	})
	return mux
}

func TestCheckLocalUplinkDialsLoopbackForWildcardBinds(t *testing.T) {
	server := httptest.NewServer(nodeStatusHandler(t))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		t.Fatal(err)
	}
	for _, bind := range []string{"127.0.0.1", "0.0.0.0"} {
		cfg := config.Defaults()
		cfg.Server.Host, cfg.Server.Port = bind, portNum
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
		if got := checkLocalUplink(context.Background(), cfg); got != "complete" {
			t.Fatalf("bind %s probed as %q, want complete", bind, got)
		}
	}
}

func TestCheckLocalUplinkDialsBracketedIPv6Loopback(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(nodeStatusHandler(t))
	server.Listener = listener
	server.Start()
	defer server.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		t.Fatal(err)
	}
	for _, bind := range []string{"::1", "::"} {
		cfg := config.Defaults()
		cfg.Server.Host, cfg.Server.Port = bind, portNum
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
		if got := checkLocalUplink(context.Background(), cfg); got != "complete" {
			t.Fatalf("bind %s probed as %q, want complete", bind, got)
		}
	}
}

func TestCheckLocalUplinkKeepsPendingAndDegradedSemantics(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/node/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"nodeId":"mac-b","uplinkEnabled":true,"tokenConfigured":true,"uplinkRunning":true,"connected":false,"lastSuccessAt":null}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Host, cfg.Server.Port = "127.0.0.1", portNum
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
	if got := checkLocalUplink(context.Background(), cfg); got != "pending" {
		t.Fatalf("first snapshot pending semantic=%q", got)
	}
	cfg.Server.Port = portNum + 1
	if got := checkLocalUplink(context.Background(), cfg); got != "degraded" {
		t.Fatalf("unreachable server degraded semantic=%q", got)
	}
}

// onboardingQuotaFixtures returns a shared identity key file, an alias spec
// covering the synthetic CodexBar accounts, and fake CodexBar runner output.
func onboardingQuotaFixtures(t *testing.T, coverAll bool) (keyPath, aliasSpec string, runner quota.Runner) {
	t.Helper()
	dir := t.TempDir()
	keyPath = filepath.Join(dir, "identity.key")
	if err := os.WriteFile(keyPath, []byte("shared-test-identity-key-32-bytes-long"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := []byte("shared-test-identity-key-32-bytes-long")
	keyA := quota.AccountKey(key, "codex", "synthetic-product-account-a")
	keyB := quota.AccountKey(key, "codex", "synthetic-product-account-b")
	aliasSpec = keyA + "=Codex A"
	if coverAll {
		aliasSpec += "," + keyB + "=Codex B"
	}
	body, err := json.Marshal([]any{
		map[string]any{"provider": "codex", "account": "Local Name A", "usage": map[string]any{
			"identity": map[string]any{"accountID": "synthetic-product-account-a"},
			"primary":  map[string]any{"usedPercent": 10.0, "windowMinutes": 300},
		}},
		map[string]any{"provider": "codex", "account": "Local Name B", "usage": map[string]any{
			"identity": map[string]any{"accountID": "synthetic-product-account-b"},
			"primary":  map[string]any{"usedPercent": 20.0, "windowMinutes": 300},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return keyPath, aliasSpec, stubRunner{responses: map[string][]byte{"codex": body}}
}

type stubRunner struct {
	responses map[string][]byte
	fails     bool
}

func (s stubRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if s.fails {
		return nil, os.ErrNotExist
	}
	return s.responses[args[2]], nil
}

func onboardingCheckWithQuota(t *testing.T, cfg config.Config, runner quota.Runner, snapshot func(context.Context, config.Config) string) operationResult {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.yaml")
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	return runNodeOnboarding(OnboardingOptions{
		ConfigPath: path, Check: true,
		Service:       func(string) operationResult { return okResult("healthy", "", nil) },
		Integration:   func(string, string) operationResult { return okResult("configured", "", nil) },
		SocketCheck:   func() string { return "ready" },
		UplinkCheck:   func(context.Context, config.Config) string { return "complete" },
		HubCheck:      func(context.Context, config.Config, string) string { return "complete" },
		HubQuotaCheck: func(context.Context, config.Config) string { return "complete" },
		QuotaRunner:   runner,
		QuotaSnapshot: func(context.Context, config.Config) string {
			if snapshot == nil {
				return "complete"
			}
			return snapshot(nil, cfg)
		},
	})
}

func onboardingQuotaConfig(keyPath, aliasSpec string) config.Config {
	cfg := config.Defaults()
	cfg.Host.ID, cfg.Host.DisplayName = "mac-b", "Laptop"
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
	cfg.Quota = config.QuotaConfig{IdentityKeyFile: keyPath, AccountAliases: aliasSpec}
	return cfg
}

func phaseMap(t *testing.T, result operationResult) map[string]string {
	t.Helper()
	body := mustJSON(resultValue(result))
	var decoded struct {
		Data struct {
			Phases []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"phases"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	phases := map[string]string{}
	for _, phase := range decoded.Data.Phases {
		phases[phase.Name] = phase.Status
	}
	return phases
}

func TestNodeOnboardingCheckReportsQuotaNotConfiguredExplicitly(t *testing.T) {
	cfg := config.Defaults()
	cfg.Host.ID, cfg.Host.DisplayName = "mac-b", "Laptop"
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-b", Token: onboardingToken}
	result := onboardingCheckWithQuota(t, cfg, stubRunner{fails: true}, nil)
	if result.OK || result.Status != "onboarding_check_degraded" {
		t.Fatalf("result=%+v", result)
	}
	phases := phaseMap(t, result)
	if phases["quota_loop"] != "quota_not_configured" {
		t.Fatalf("quota_loop phase=%v", phases)
	}
	for name, status := range phases {
		if strings.HasPrefix(name, "quota") && status == "complete" {
			t.Fatalf("unconfigured quota claimed complete: %s=%s", name, status)
		}
	}
	if !strings.Contains(string(mustJSON(resultValue(result))), "quota_not_configured") {
		t.Fatal("machine-readable quota_not_configured marker missing")
	}
}

func TestNodeOnboardingCheckReportsInvalidQuotaAliasesWithoutStartingClosure(t *testing.T) {
	keyPath, _, runner := onboardingQuotaFixtures(t, true)
	cfg := onboardingQuotaConfig(keyPath, "acct_not-valid=Codex A")
	result := onboardingCheckWithQuota(t, cfg, runner, nil)
	if result.OK || result.Status != "onboarding_check_degraded" {
		t.Fatalf("result=%+v", result)
	}
	phases := phaseMap(t, result)
	if phases["quota_alias_config"] != "degraded" || phases["quota_alias_coverage"] != "degraded" || phases["codexbar"] != "complete" {
		t.Fatalf("invalid alias phases=%v", phases)
	}
	body := string(mustJSON(resultValue(result)))
	if strings.Contains(body, "synthetic-product-account") || strings.Contains(body, "Local Name") {
		t.Fatalf("invalid alias check leaked identity: %s", body)
	}
}

func TestNodeOnboardingCheckQuotaLoopPhasesWhenConfigured(t *testing.T) {
	keyPath, aliasSpec, runner := onboardingQuotaFixtures(t, true)
	cfg := onboardingQuotaConfig(keyPath, aliasSpec)
	result := onboardingCheckWithQuota(t, cfg, runner, nil)
	if !result.OK || result.Status != "onboarding_check_complete" {
		t.Fatalf("result=%+v", result)
	}
	phases := phaseMap(t, result)
	for _, name := range []string{"quota_identity_key", "codexbar", "quota_alias_coverage", "quota_snapshot"} {
		if phases[name] != "complete" {
			t.Fatalf("phase %s=%v", name, phases)
		}
	}
	body := string(mustJSON(resultValue(result)))
	if !strings.Contains(body, "acct_") || strings.Contains(body, "synthetic-product-account") || strings.Contains(body, "Local Name") {
		t.Fatalf("alias audit output is missing or leaked identity: %s", body)
	}
}

func TestNodeOnboardingCheckQuotaCoverageFailureDegrades(t *testing.T) {
	keyPath, aliasSpec, runner := onboardingQuotaFixtures(t, false)
	cfg := onboardingQuotaConfig(keyPath, aliasSpec)
	result := onboardingCheckWithQuota(t, cfg, runner, nil)
	if result.OK || result.Status != "onboarding_check_degraded" {
		t.Fatalf("result=%+v", result)
	}
	phases := phaseMap(t, result)
	if phases["quota_alias_coverage"] != "degraded" || phases["codexbar"] != "complete" {
		t.Fatalf("coverage phases=%v", phases)
	}
}

func TestNodeOnboardingCheckCodexBarFailureDegrades(t *testing.T) {
	keyPath, aliasSpec, _ := onboardingQuotaFixtures(t, true)
	cfg := onboardingQuotaConfig(keyPath, aliasSpec)
	result := onboardingCheckWithQuota(t, cfg, stubRunner{fails: true}, nil)
	if result.OK || result.Status != "onboarding_check_degraded" {
		t.Fatalf("result=%+v", result)
	}
	phases := phaseMap(t, result)
	if phases["codexbar"] != "degraded" || phases["quota_alias_coverage"] != "degraded" {
		t.Fatalf("codexbar phases=%v", phases)
	}
}

func TestNodeOnboardingCheckQuotaSnapshotPendingDoesNotClaimComplete(t *testing.T) {
	keyPath, aliasSpec, runner := onboardingQuotaFixtures(t, true)
	cfg := onboardingQuotaConfig(keyPath, aliasSpec)
	result := onboardingCheckWithQuota(t, cfg, runner, func(context.Context, config.Config) string { return "pending" })
	if result.OK || result.Status != "onboarding_check_pending" {
		t.Fatalf("result=%+v", result)
	}
	if phaseMap(t, result)["quota_snapshot"] != "pending" {
		t.Fatal("quota_snapshot pending phase missing")
	}
}

func TestCheckQuotaSnapshotProbesLocalPublicState(t *testing.T) {
	sampledAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"schemaVersion":1,"stateKind":"public","quota":[{"provider":"codex","accountKey":"acct_00000000000000000000000000000001","displayLabel":"Codex A","sourceStatus":"available","sampledAt":%q,"windows":[{}]},{"provider":"codex","accountKey":"acct_00000000000000000000000000000002","displayLabel":"Codex B","sourceStatus":"available","sampledAt":%q,"windows":[{}]},{"provider":"zai","accountKey":"acct_00000000000000000000000000000003","displayLabel":"GLM","sourceStatus":"available","sampledAt":%q,"windows":[{}]}],"sources":{}}`, sampledAt, sampledAt, sampledAt)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Host, cfg.Server.Port = "0.0.0.0", portNum
	if got := checkQuotaSnapshot(context.Background(), cfg); got != "complete" {
		t.Fatalf("wildcard probe snapshot=%q", got)
	}

	empty := http.NewServeMux()
	empty.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"schemaVersion":1,"stateKind":"public","quota":[],"sources":{}}`)
	})
	emptyServer := httptest.NewServer(empty)
	defer emptyServer.Close()
	_, emptyPort, err := net.SplitHostPort(emptyServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var emptyPortNum int
	if _, err := fmt.Sscanf(emptyPort, "%d", &emptyPortNum); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Port = emptyPortNum
	if got := checkQuotaSnapshot(context.Background(), cfg); got != "pending" {
		t.Fatalf("empty snapshot status=%q", got)
	}

	cfg.Server.Port = emptyPortNum + 1
	if got := checkQuotaSnapshot(context.Background(), cfg); got != "degraded" {
		t.Fatalf("unreachable snapshot status=%q", got)
	}
}

func TestCheckQuotaSnapshotRejectsIncompleteUnavailableAndExpiredData(t *testing.T) {
	now := time.Date(2026, 8, 24, 5, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"missing account": `[{"provider":"codex","displayLabel":"Codex A","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]}]`,
		"unavailable":     `[{"provider":"codex","displayLabel":"Codex A","sourceStatus":"unavailable","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]},{"provider":"codex","displayLabel":"Codex B","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]},{"provider":"zai","displayLabel":"GLM","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]}]`,
		"expired":         `[{"provider":"codex","displayLabel":"Codex A","sourceStatus":"available","sampledAt":"2026-08-24T04:00:00Z","windows":[{}]},{"provider":"codex","displayLabel":"Codex B","sourceStatus":"available","sampledAt":"2026-08-24T04:00:00Z","windows":[{}]},{"provider":"zai","displayLabel":"GLM","sourceStatus":"available","sampledAt":"2026-08-24T04:00:00Z","windows":[{}]}]`,
		"duplicate label": `[{"provider":"codex","displayLabel":"Codex A","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]},{"provider":"codex","displayLabel":"Codex A","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]},{"provider":"zai","displayLabel":"GLM","sourceStatus":"available","sampledAt":"2026-08-24T04:59:00Z","windows":[{}]}]`,
	}
	for name, quotaJSON := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"quota":%s}`, quotaJSON)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			var portNum int
			if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.Server.Host, cfg.Server.Port = "127.0.0.1", portNum
			if got := checkQuotaSnapshotAt(context.Background(), cfg, now); got != "degraded" {
				t.Fatalf("snapshot status=%q", got)
			}
		})
	}
}

func quotaSnapshotEntryJSON(provider, accountKey, label, observedBy string, sampledAt time.Time, sourceStatus string, withWindow bool) map[string]any {
	entry := map[string]any{
		"provider":     provider,
		"accountKey":   accountKey,
		"displayLabel": label,
		"sourceStatus": sourceStatus,
		"sampledAt":    sampledAt.UTC().Format(time.RFC3339Nano),
		"observedBy":   observedBy,
		"windows":      []any{},
	}
	if withWindow {
		entry["windows"] = []any{map[string]any{"name": "PRIMARY"}}
	}
	return entry
}

func TestLocalQuotaSnapshotAllowsOnlyTheAccountsObservedByThisNode(t *testing.T) {
	now := time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	keyA := "acct_00000000000000000000000000000001"
	keyB := "acct_00000000000000000000000000000002"
	keyGLM := "acct_00000000000000000000000000000003"
	cases := map[string][]map[string]any{
		"mac a codex a and b": {
			quotaSnapshotEntryJSON("codex", keyA, "Codex A", "mac-a", now.Add(-time.Minute), "available", true),
			quotaSnapshotEntryJSON("codex", keyB, "Codex B", "mac-a", now.Add(-time.Minute), "available", true),
		},
		"mac b glm only": {
			quotaSnapshotEntryJSON("zai", keyGLM, "GLM", "mac-b", now.Add(-time.Minute), "available", true),
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"quota": entries})
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			var portNum int
			if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.Server.Host, cfg.Server.Port = "127.0.0.1", portNum
			if got := checkQuotaSnapshotAt(context.Background(), cfg, now); got != "complete" {
				t.Fatalf("local partial quota status=%q entries=%v", got, entries)
			}
		})
	}
}

func TestHubGlobalQuotaCoverageRequiresAllFreshAccountsFromOnlineHosts(t *testing.T) {
	now := time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	keyA := "acct_00000000000000000000000000000001"
	keyB := "acct_00000000000000000000000000000002"
	keyGLM := "acct_00000000000000000000000000000003"
	baseHosts := []map[string]any{
		{"configuredHostId": "mac-a", "source": map[string]any{"status": "online"}},
		{"configuredHostId": "mac-b", "source": map[string]any{"status": "online"}},
	}
	complete := []map[string]any{
		quotaSnapshotEntryJSON("codex", keyA, "Codex A", "mac-a", now.Add(-time.Minute), "available", true),
		quotaSnapshotEntryJSON("codex", keyB, "Codex B", "mac-a", now.Add(-time.Minute), "available", true),
		quotaSnapshotEntryJSON("zai", keyGLM, "GLM", "mac-b", now.Add(-time.Minute), "available", true),
	}
	cases := map[string]struct {
		hosts []map[string]any
		quota []map[string]any
		want  string
	}{
		"mac a and mac b complete": {hosts: baseHosts, quota: complete, want: "complete"},
		"no first global snapshot": {hosts: baseHosts, quota: nil, want: "pending"},
		"missing glm":              {hosts: baseHosts, quota: complete[:2], want: "degraded"},
		"stale glm host": {
			hosts: []map[string]any{
				baseHosts[0],
				{"configuredHostId": "mac-b", "source": map[string]any{"status": "stale"}},
			},
			quota: complete, want: "degraded",
		},
		"unavailable account": {
			hosts: baseHosts,
			quota: []map[string]any{
				complete[0], complete[1],
				quotaSnapshotEntryJSON("zai", keyGLM, "GLM", "mac-b", now.Add(-time.Minute), "unavailable", true),
			},
			want: "degraded",
		},
		"expired sampledAt": {
			hosts: baseHosts,
			quota: []map[string]any{
				quotaSnapshotEntryJSON("codex", keyA, "Codex A", "mac-a", now.Add(-quota.LastGoodTTL-time.Second), "available", true),
				complete[1], complete[2],
			},
			want: "degraded",
		},
		"no windows": {
			hosts: baseHosts,
			quota: []map[string]any{
				complete[0], complete[1],
				quotaSnapshotEntryJSON("zai", keyGLM, "GLM", "mac-b", now.Add(-time.Minute), "available", false),
			},
			want: "degraded",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"hosts": tc.hosts, "quota": tc.quota})
			}))
			defer server.Close()
			cfg := config.Defaults()
			cfg.Uplink.Endpoint = server.URL
			if got := checkHubQuotaCoverageAt(context.Background(), cfg, now); got != tc.want {
				t.Fatalf("Hub quota status=%q want %q", got, tc.want)
			}
		})
	}
}

func TestPartialNodeOnboardingDoesNotRequireOtherHostsQuota(t *testing.T) {
	keyPath, aliasSpec, runner := onboardingQuotaFixtures(t, true)
	cfg := onboardingQuotaConfig(keyPath, aliasSpec)
	macA := onboardingCheckWithQuota(t, cfg, runner, func(context.Context, config.Config) string { return "complete" })
	if !macA.OK || macA.Status != "onboarding_check_complete" {
		t.Fatalf("Mac A partial quota should be globally closable: %+v", macA)
	}
	macBRunner := stubRunner{responses: map[string][]byte{"codex": []byte(`[]`)}}
	macB := onboardingCheckWithQuota(t, cfg, macBRunner, func(context.Context, config.Config) string { return "complete" })
	if !macB.OK || macB.Status != "onboarding_check_complete" {
		t.Fatalf("Mac B partial quota should be globally closable: %+v", macB)
	}
}

func TestFormalOnboardingAndCheckShareQuotaClosureConclusion(t *testing.T) {
	keyPath, aliasSpec, runner := onboardingQuotaFixtures(t, true)
	aliasPath := filepath.Join(t.TempDir(), "aliases")
	if err := os.WriteFile(aliasPath, []byte(aliasSpec), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "node.yaml")
	base := OnboardingOptions{
		ConfigPath: configPath, NodeID: "mac-a", DisplayName: "Studio Mac", HubEndpoint: "https://hub.example.test", NodeToken: onboardingToken,
		QuotaIdentityKeyFile: keyPath, QuotaAliasFile: aliasPath,
		Service:       func(string) operationResult { return okResult("ok", "", nil) },
		Integration:   func(string, string) operationResult { return okResult("ok", "", nil) },
		SocketCheck:   func() string { return "ready" },
		UplinkCheck:   func(context.Context, config.Config) string { return "complete" },
		HubCheck:      func(context.Context, config.Config, string) string { return "complete" },
		QuotaRunner:   runner,
		QuotaSnapshot: func(context.Context, config.Config) string { return "complete" },
		HubQuotaCheck: func(context.Context, config.Config) string { return "complete" },
	}
	installed := runNodeOnboarding(base)
	if !installed.OK || installed.Status != "onboarding_complete" || installed.Data["closureStatus"] != "complete" {
		t.Fatalf("complete formal onboarding=%+v", installed)
	}
	check := base
	check.Check = true
	check.NodeToken, check.QuotaIdentityKeyFile, check.QuotaAliasFile = "", "", ""
	checked := runNodeOnboarding(check)
	if !checked.OK || checked.Status != "onboarding_check_complete" || checked.Data["closureStatus"] != "complete" {
		t.Fatalf("complete follow-up check=%+v", checked)
	}

	pendingConfig := filepath.Join(t.TempDir(), "pending-node.yaml")
	pending := base
	pending.ConfigPath = pendingConfig
	pending.QuotaSnapshot = func(context.Context, config.Config) string { return "pending" }
	pending.HubQuotaCheck = func(context.Context, config.Config) string { return "pending" }
	installedPending := runNodeOnboarding(pending)
	if installedPending.OK || installedPending.Status != "onboarding_pending" || installedPending.Data["installationStatus"] != "complete" || installedPending.Data["closureStatus"] != "pending" {
		t.Fatalf("pending formal onboarding=%+v", installedPending)
	}
	pendingCheck := pending
	pendingCheck.Check = true
	pendingCheck.NodeToken, pendingCheck.QuotaIdentityKeyFile, pendingCheck.QuotaAliasFile = "", "", ""
	checkedPending := runNodeOnboarding(pendingCheck)
	if checkedPending.OK || checkedPending.Status != "onboarding_check_pending" || checkedPending.Data["closureStatus"] != "pending" {
		t.Fatalf("pending follow-up check=%+v", checkedPending)
	}
}
