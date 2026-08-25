package product

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/quota"
)

type quotaProductTestRunner struct {
	responses map[string][]byte
	err       error
}

func (r quotaProductTestRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.responses[args[2]], nil
}

func productQuotaFixtures() quotaProductTestRunner {
	return quotaProductTestRunner{responses: map[string][]byte{
		"codex": []byte(`[{"provider":"codex","account":"A","usage":{"identity":{"accountID":"account-a","accountEmail":"a@example.invalid"},"primary":{"usedPercent":10}}},{"provider":"codex","account":"B","usage":{"identity":{"accountID":"account-b","accountEmail":"b@example.invalid"},"primary":{"usedPercent":20}}}]`),
		"zai":   []byte(`[{"provider":"zai","account":"GLM","usage":{"identity":{"accountID":"glm","accountEmail":"glm@example.invalid"},"primary":{"usedPercent":30}}}]`),
	}}
}

func TestQuotaDetectGeneratesIdentityAndReturnsSchemaV1SanitizedData(t *testing.T) {
	home := t.TempDir()
	result := RunQuotaCommand("detect", QuotaCommandOptions{Home: home, Runner: productQuotaFixtures(), CLIResolve: quotaTestCLIFound})
	if result.OK || result.SchemaVersion != 1 || result.Status != "quota_configuration_required" {
		t.Fatalf("result=%+v", result)
	}
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quota.LoadIdentityKey(paths.QuotaIdentityKey); err != nil {
		t.Fatalf("generated identity unavailable: %v", err)
	}
	cfg, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(cfg)
	if strings.Contains(encoded, "example.invalid") || strings.Contains(encoded, "account-a") {
		t.Fatalf("config leaked CodexBar identity: %s", encoded)
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"example.invalid", "account-a", "account-b", "glm"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("result leaked %q: %s", forbidden, body)
		}
	}
}

func TestQuotaConfigureRequiresUniqueCoverageAndPersistsAtomically(t *testing.T) {
	home := t.TempDir()
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
	if err != nil {
		t.Fatal(err)
	}
	accountA := quota.AccountKey(key, "codex", "account-a")
	accountB := quota.AccountKey(key, "codex", "account-b")
	runner := productQuotaFixtures()
	result := RunQuotaCommand("configure", QuotaCommandOptions{
		Home: home, Runner: runner, CLIResolve: quotaTestCLIFound,
		Assignments: []QuotaAssignment{{AccountKey: accountA, Label: "Codex A"}, {AccountKey: accountB, Label: "Codex B"}},
	})
	if !result.OK || result.Status != "quota_configured" || result.SchemaVersion != 1 {
		t.Fatalf("configure result=%+v", result)
	}
	cfg, err := config.Load(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	aliases, err := quota.ParseAliases(cfg.Quota.AccountAliases)
	if err != nil || len(aliases) != 2 || aliases[accountA] != "Codex A" || aliases[accountB] != "Codex B" {
		t.Fatalf("persisted aliases=%v err=%v", aliases, err)
	}
	duplicate := RunQuotaCommand("configure", QuotaCommandOptions{
		Home: home, Runner: runner, CLIResolve: quotaTestCLIFound,
		Assignments: []QuotaAssignment{{AccountKey: accountA, Label: "Codex A"}, {AccountKey: accountB, Label: "Codex A"}},
	})
	if duplicate.OK || duplicate.Status != "quota_configuration_required" {
		t.Fatalf("duplicate labels result=%+v", duplicate)
	}
}

func TestQuotaDetectMissingCodexBarIsDegradedAndDoesNotBlockNode(t *testing.T) {
	result := RunQuotaCommand("detect", QuotaCommandOptions{Home: t.TempDir(), Runner: quotaProductTestErrorRunner{}, CLIResolve: quotaTestCLIFound})
	if result.OK || result.Status != "quota_unavailable" || result.SchemaVersion != 1 {
		t.Fatalf("missing CodexBar result=%+v", result)
	}
	if strings.Contains(result.Message, "codexbar") {
		t.Fatalf("result exposed raw command detail: %q", result.Message)
	}
}

func TestQuotaDetectFailsClosedForMultipleGLMAccounts(t *testing.T) {
	runner := productQuotaFixtures()
	runner.responses["zai"] = []byte(`[{"provider":"zai","account":"GLM A","usage":{"identity":{"accountID":"glm-a"},"primary":{"usedPercent":30}}},{"provider":"zai","account":"GLM B","usage":{"identity":{"accountID":"glm-b"},"primary":{"usedPercent":40}}}]`)
	result := RunQuotaCommand("detect", QuotaCommandOptions{Home: t.TempDir(), Runner: runner, CLIResolve: quotaTestCLIFound})
	if result.OK || result.Status != "quota_configuration_required" {
		t.Fatalf("multiple GLM accounts were not fail-closed: %+v", result)
	}
}

func TestQuotaDetectMacAProfileMatrix(t *testing.T) {
	cases := map[string]struct {
		codex []byte
		zai   []byte
		want  string
	}{
		"zero Codex":             {codex: []byte(`[]`), zai: productQuotaFixtures().responses["zai"], want: "not_available"},
		"zero GLM":               {codex: productQuotaFixtures().responses["codex"], zai: []byte(`[]`), want: "not_available"},
		"one Codex":              {codex: []byte(`[ {"provider":"codex","usage":{"identity":{"accountID":"account-a"},"primary":{"usedPercent":10}}} ]`), zai: productQuotaFixtures().responses["zai"], want: "not_available"},
		"two Codex plus one GLM": {codex: productQuotaFixtures().responses["codex"], zai: productQuotaFixtures().responses["zai"], want: "available"},
		"multiple GLM":           {codex: productQuotaFixtures().responses["codex"], zai: []byte(`[ {"provider":"zai","usage":{"identity":{"accountID":"glm-a"},"primary":{"usedPercent":10}}}, {"provider":"zai","usage":{"identity":{"accountID":"glm-b"},"primary":{"usedPercent":20}}} ]`), want: "not_available"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			paths, err := ResolvePaths(home)
			if err != nil {
				t.Fatal(err)
			}
			key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.Quota.IdentityKeyFile = paths.QuotaIdentityKey
			cfg.Quota.AccountAliases = quota.AccountKey(key, "codex", "account-a") + "=Codex A," + quota.AccountKey(key, "codex", "account-b") + "=Codex B"
			if err := config.SaveAtomic(paths.Config, cfg); err != nil {
				t.Fatal(err)
			}
			runner := quotaProductTestRunner{responses: map[string][]byte{"codex": tc.codex, "zai": tc.zai}}
			result := RunQuotaCommand("detect", QuotaCommandOptions{Home: home, Runner: runner, CLIResolve: quotaTestCLIFound})
			if tc.want == "available" {
				if !result.OK || result.Status != "quota_detected" {
					t.Fatalf("full Mac A profile result=%+v", result)
				}
			} else if result.OK || result.Status == "quota_detected" {
				t.Fatalf("incomplete profile was healthy: %+v", result)
			}
		})
	}
}

func TestQuotaConfigureRejectsEmptyAndPreservesLastGoodAliasesOnPartialDetection(t *testing.T) {
	home := t.TempDir()
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
	if err != nil {
		t.Fatal(err)
	}
	accountA := quota.AccountKey(key, "codex", "account-a")
	accountB := quota.AccountKey(key, "codex", "account-b")
	original := accountA + "=Codex A," + accountB + "=Codex B"
	cfg := config.Defaults()
	cfg.Quota.IdentityKeyFile = paths.QuotaIdentityKey
	cfg.Quota.AccountAliases = original
	if err := config.SaveAtomic(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	partial := quotaProductTestRunner{responses: map[string][]byte{
		"codex": []byte(`[{"provider":"codex","usage":{"identity":{"accountID":"account-a"},"primary":{"usedPercent":10}}}]`),
		"zai":   productQuotaFixtures().responses["zai"],
	}}
	result := RunQuotaCommand("configure", QuotaCommandOptions{Home: home, Runner: partial, CLIResolve: quotaTestCLIFound})
	if result.OK || result.Status != "quota_configuration_required" {
		t.Fatalf("partial configure result=%+v", result)
	}
	loaded, err := config.Load(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Quota.AccountAliases != original {
		t.Fatalf("partial detection overwrote last-good aliases: %q", loaded.Quota.AccountAliases)
	}
	empty := RunQuotaCommand("configure", QuotaCommandOptions{Home: home, Runner: productQuotaFixtures(), CLIResolve: quotaTestCLIFound})
	if empty.OK || empty.Status == "quota_configured" {
		t.Fatalf("empty assignment/configuration was accepted: %+v", empty)
	}
}

func TestQuotaStatusConfigurationReadyIsNotAvailableWithoutSnapshot(t *testing.T) {
	home := t.TempDir()
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Quota.IdentityKeyFile = paths.QuotaIdentityKey
	cfg.Quota.AccountAliases = quota.AccountKey(key, "codex", "account-a") + "=Codex A"
	if err := config.SaveAtomic(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	result := RunQuotaCommand("status", QuotaCommandOptions{Home: home})
	if result.Status == "quota_configured" || result.Status == "quota_available" {
		t.Fatalf("configuration readiness was misreported as availability: %+v", result)
	}
}

func TestQuotaStatusUsesFreshSanitizedSnapshotWithoutCodexBar(t *testing.T) {
	now := time.Now().UTC()
	entries := quotaStatusEntries(now, "available", now.Add(-time.Minute), true)
	server := quotaSnapshotServer(t, entries)
	defer server.Close()

	runner := &quotaStatusCountingRunner{}
	result := RunQuotaCommand("status", QuotaCommandOptions{Home: quotaStatusHome(t, server), Runner: runner})
	if !result.OK || result.Status != "quota_available" {
		t.Fatalf("fresh A/B/GLM snapshot result=%+v", result)
	}
	if runner.calls != 0 {
		t.Fatalf("quota status unexpectedly called CodexBar runner %d times", runner.calls)
	}
}

func TestQuotaStatusMapsStaleExpiredEmptyWindowAndProviderDegradedToDegraded(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name    string
		entries []quotaSnapshotEntry
	}{
		{name: "stale", entries: quotaStatusEntries(now, "available", now.Add(-quota.LastGoodTTL-time.Second), true)},
		{name: "expired", entries: quotaStatusEntries(now, "available", now.Add(-quota.LastGoodTTL-10*time.Minute), true)},
		{name: "no window", entries: quotaStatusEntries(now, "available", now.Add(-time.Minute), false)},
		{name: "provider degraded", entries: quotaStatusEntries(now, "degraded", now.Add(-time.Minute), true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := quotaSnapshotServer(t, tc.entries)
			defer server.Close()
			result := RunQuotaCommand("status", QuotaCommandOptions{Home: quotaStatusHome(t, server)})
			if result.OK || result.Status != "quota_degraded" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestQuotaStatusMapsNoEndpointAndEmptySnapshotToUnavailable(t *testing.T) {
	t.Run("empty snapshot", func(t *testing.T) {
		server := quotaSnapshotServer(t, nil)
		defer server.Close()
		result := RunQuotaCommand("status", QuotaCommandOptions{Home: quotaStatusHome(t, server)})
		if result.OK || result.Status != "quota_unavailable" {
			t.Fatalf("empty snapshot result=%+v", result)
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		server := quotaSnapshotServer(t, nil)
		home := quotaStatusHome(t, server)
		server.Close()
		result := RunQuotaCommand("status", QuotaCommandOptions{Home: home})
		if result.OK || result.Status != "quota_unavailable" {
			t.Fatalf("no endpoint result=%+v", result)
		}
	})
}

func quotaStatusEntries(now time.Time, sourceStatus string, sampledAt time.Time, withWindow bool) []quotaSnapshotEntry {
	var windows []json.RawMessage
	if withWindow {
		windows = []json.RawMessage{json.RawMessage(`{"name":"primary","usedPercent":10}`)}
	}
	return []quotaSnapshotEntry{
		{Provider: "codex", AccountKey: "acct_00000000000000000000000000000001", DisplayLabel: "Codex A", SourceStatus: sourceStatus, SampledAt: timePtrForQuotaTest(sampledAt), Windows: windows, ObservedBy: "mac-a"},
		{Provider: "codex", AccountKey: "acct_00000000000000000000000000000002", DisplayLabel: "Codex B", SourceStatus: sourceStatus, SampledAt: timePtrForQuotaTest(sampledAt), Windows: windows, ObservedBy: "mac-a"},
		{Provider: "zai", AccountKey: "acct_00000000000000000000000000000003", DisplayLabel: "GLM", SourceStatus: sourceStatus, SampledAt: timePtrForQuotaTest(sampledAt), Windows: windows, ObservedBy: "mac-a"},
	}
}

func timePtrForQuotaTest(value time.Time) *time.Time { return &value }

func quotaSnapshotServer(t *testing.T, entries []quotaSnapshotEntry) *httptest.Server {
	t.Helper()
	return quotaSnapshotServerWithSources(t, entries, nil)
}

func quotaSnapshotServerWithSources(t *testing.T, entries []quotaSnapshotEntry, sources map[string]quotaSourceHealthJSON) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/state" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Quota   []quotaSnapshotEntry             `json:"quota"`
			Sources map[string]quotaSourceHealthJSON `json:"sources"`
		}{Quota: entries, Sources: sources})
	}))
}

// quotaTestCLIFound keeps fixture-based detect/configure tests hermetic: the
// production resolver depends on the machine's CodexBar installation.
func quotaTestCLIFound() (string, error) { return "/usr/local/bin/codexbar", nil }

func quotaStatusHome(t *testing.T, server *httptest.Server) string {
	t.Helper()
	hostPort := strings.TrimPrefix(server.URL, "http://")
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := quota.EnsureIdentityKey(paths.QuotaIdentityKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Host = host
	cfg.Server.Port = port
	cfg.Quota.IdentityKeyFile = paths.QuotaIdentityKey
	cfg.Quota.AccountAliases = quota.AccountKey(key, "codex", "account-a") + "=Codex A," + quota.AccountKey(key, "codex", "account-b") + "=Codex B"
	if err := config.SaveAtomic(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	return home
}

type quotaStatusCountingRunner struct{ calls int }

func (r *quotaStatusCountingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	r.calls++
	return nil, errors.New("unexpected CodexBar call")
}

type quotaProductTestErrorRunner struct{}

func (quotaProductTestErrorRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("codexbar missing")
}

// The menu bar must be able to say "CodexBar CLI unavailable" instead of a
// generic quota failure. detect/configure gate on the same absolute-path
// resolution the LaunchAgent collector uses, without leaking install paths.
func TestQuotaDetectDistinguishesMissingCodexBarCLI(t *testing.T) {
	home := t.TempDir()
	result := RunQuotaCommand("detect", QuotaCommandOptions{
		Home: home, Runner: productQuotaFixtures(),
		CLIResolve: func() (string, error) { return "", quota.ErrCodexBarCLINotFound },
	})
	if result.OK || result.Status != "quota_cli_unavailable" || result.SchemaVersion != 1 {
		t.Fatalf("missing CLI result=%+v", result)
	}
	body, _ := json.Marshal(result)
	if strings.Contains(string(body), "/opt/homebrew") || strings.Contains(string(body), "/usr/local") {
		t.Fatalf("result leaked an install path: %s", body)
	}
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quota.LoadIdentityKey(paths.QuotaIdentityKey); err != nil {
		t.Fatalf("identity generation must still work with the CLI missing: %v", err)
	}
}

func TestQuotaConfigureGatesOnMissingCodexBarCLI(t *testing.T) {
	result := RunQuotaCommand("configure", QuotaCommandOptions{
		Home:       t.TempDir(),
		Runner:     productQuotaFixtures(),
		CLIResolve: func() (string, error) { return "", quota.ErrCodexBarCLINotFound },
	})
	if result.OK || result.Status != "quota_cli_unavailable" {
		t.Fatalf("configure result=%+v", result)
	}
}

// The Node's own sanitized public state carries the cli_unavailable reason;
// quota status must surface it distinctly so the menu bar label stays honest.
func TestQuotaStatusSurfacesNodeCLIUnavailableReason(t *testing.T) {
	now := time.Now().UTC()
	entries := quotaStatusEntries(now, "unavailable", now.Add(-time.Minute), true)
	server := quotaSnapshotServerWithSources(t, entries, map[string]quotaSourceHealthJSON{
		"quota": {Status: "unavailable", Reason: "cli_unavailable"},
	})
	defer server.Close()
	result := RunQuotaCommand("status", QuotaCommandOptions{Home: quotaStatusHome(t, server)})
	if result.OK || result.Status != "quota_cli_unavailable" {
		t.Fatalf("result=%+v", result)
	}
	if data := result.Data["cliAvailable"]; data == nil {
		t.Fatalf("machine-readable cliAvailable marker missing: %+v", result.Data)
	}
}
