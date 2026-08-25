package quota

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type fakeRunner struct {
	responses map[string][]byte
	errors    map[string]error
}

func (f fakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	provider := args[2]
	if err := f.errors[provider]; err != nil {
		return nil, err
	}
	return f.responses[provider], nil
}

// These fixtures intentionally use the real CodexBar 0.54.0 JSON shape:
// ProviderPayload[] with account + usage.identity + UsageSnapshot windows.
// Identity strings are synthetic/redacted and never leave the parser.
func codexFixture(reverse bool) []byte {
	a := map[string]any{
		"provider": "codex",
		"account":  "Codex A",
		"usage": map[string]any{
			"identity": map[string]any{
				"accountID":    "redacted-codex-account-a",
				"accountEmail": "redacted-a@example.invalid",
				"loginMethod":  "oauth",
			},
			"primary":   map[string]any{"usedPercent": 25.0, "windowMinutes": 300, "resetsAt": "2026-08-24T05:00:00Z"},
			"secondary": map[string]any{"usedPercent": 40.0, "windowMinutes": 10080, "resetsAt": "2026-08-31T00:00:00Z"},
			"tertiary":  map[string]any{"usedPercent": 10.0, "windowMinutes": 1440, "resetsAt": "2026-08-25T00:00:00Z"},
			"extraRateWindows": []any{
				map[string]any{"id": "redacted-extra-a", "title": "Supplemental", "window": map[string]any{"usedPercent": 5.0, "windowMinutes": 60, "resetsAt": "2026-08-24T02:00:00Z"}},
			},
		},
	}
	b := map[string]any{
		"provider": "codex",
		"account":  "Codex B",
		"usage": map[string]any{
			"identity": map[string]any{
				"accountID":    "redacted-codex-account-b",
				"accountEmail": "redacted-b@example.invalid",
				"loginMethod":  "oauth",
			},
			"primary":   map[string]any{"usedPercent": 75.0, "windowMinutes": 300, "resetsAt": "2026-08-24T05:00:00Z"},
			"secondary": map[string]any{"remainingPercent": 20.0, "windowMinutes": 10080, "resetsAt": "2026-08-31T00:00:00Z"},
		},
	}
	accounts := []any{a, b}
	if reverse {
		accounts = []any{b, a}
	}
	body, _ := json.Marshal(accounts)
	return body
}

func zaiFixture() []byte {
	body, _ := json.Marshal([]any{map[string]any{
		"provider": "zai",
		"account":  "GLM",
		"usage": map[string]any{
			"identity": map[string]any{
				"accountID":    "redacted-glm-account",
				"accountEmail": "redacted-glm@example.invalid",
				"loginMethod":  "api",
			},
			"primary":   map[string]any{"usedPercent": 5.0, "windowMinutes": 300},
			"secondary": map[string]any{"usedPercent": 35.0, "windowMinutes": 10080},
		},
	}})
	return body
}

func quotaTestStore() *state.Store {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	return state.NewStore(state.LiveInitialState(now, state.HostState{ID: "mac-a", DisplayName: "Studio Mac"}))
}

func quotaRunner(responses map[string][]byte) *fakeRunner {
	return &fakeRunner{responses: responses, errors: map[string]error{}}
}

func TestCollectorParsesRealCodexBarArrayWithTwoCodexAccountsAndGLM(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	key := []byte("shared-test-identity-key-32-bytes-long")
	store := quotaTestStore()
	collector := NewCollector(store, "mac-a", key, nil)
	collector.SetClock(func() time.Time { return now })
	collector.SetRunner(quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()}))
	collector.SetAliases(map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	})
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if len(root.Quota) != 3 || root.Sources["quota.codex"].Status != state.SourceAvailable || root.Sources["quota.zai"].Status != state.SourceAvailable {
		t.Fatalf("quota=%+v sources=%+v", root.Quota, root.Sources)
	}
	labels := map[string]bool{}
	glmWindows := 0
	for _, item := range root.Quota {
		labels[item.DisplayLabel] = true
		if item.DisplayLabel == "GLM" && item.Windows != nil {
			glmWindows = len(*item.Windows)
		}
		if item.AccountKey == "" || strings.Contains(item.AccountKey, "redacted") || strings.Contains(item.AccountKey, "example.invalid") {
			t.Fatalf("unsafe identity projection=%+v", item)
		}
		if item.SourceID == "" || item.SampledAt == nil || item.Windows == nil {
			t.Fatalf("missing sanitized fields=%+v", item)
		}
	}
	for _, want := range []string{"Codex A", "Codex B", "GLM"} {
		if !labels[want] {
			t.Fatalf("missing label %q: %v", want, labels)
		}
	}
	if glmWindows < 2 {
		t.Fatalf("GLM five-hour/weekly windows missing: %d", glmWindows)
	}
	public := state.ProjectPublic(root, state.RuntimeCapabilities{}, state.ProjectionConfig{}, now)
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "redacted-") || strings.Contains(string(encoded), "example.invalid") {
		t.Fatalf("public quota leaked identity: %s", encoded)
	}
}

func TestCollectorParsesAllRealUsageWindowFields(t *testing.T) {
	key := []byte("shared-test-identity-key-32-bytes-long")
	aliases := map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	}
	items, err := parseProviderWithAliases(codexFixture(false), "codex", "Codex", key, "mac-a", time.Now().UTC(), aliases)
	if err != nil {
		t.Fatal(err)
	}
	var rich *Observation
	for i := range items {
		if items[i].DisplayLabel == "Codex A" {
			rich = &items[i]
		}
	}
	if len(items) != 2 || rich == nil || len(rich.Windows) < 4 {
		t.Fatalf("primary/secondary/tertiary/extra windows missing: %+v", items)
	}
	names := map[string]bool{}
	for _, window := range rich.Windows {
		names[window.Name] = true
	}
	for _, name := range []string{"PRIMARY", "SECONDARY", "TERTIARY", "Supplemental"} {
		if !names[name] {
			t.Fatalf("missing parsed window %q: %v", name, names)
		}
	}
}

func TestCollectorRejectsInventedAccountsWrapper(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"accounts": []any{map[string]any{"account": "Codex A"}}})
	if _, err := parseProviderWithAliases(body, "codex", "Codex", []byte("shared-key"), "mac-a", time.Now(), nil); err == nil {
		t.Fatal("accounts wrapper must not be accepted as the CodexBar protocol")
	}
}

func TestCollectorStableAliasesAndAccountKeysAcrossNodeFixtures(t *testing.T) {
	key := []byte("shared-test-identity-key-32-bytes-long")
	aliases := map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	}
	parse := func(node string, body []byte) []Observation {
		items, err := parseProviderWithAliases(body, "codex", "Codex", key, node, time.Now().UTC(), aliases)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	left := parse("mac-a", codexFixture(false))
	right := parse("mac-b", codexFixture(true))
	if len(left) != 2 || len(right) != 2 {
		t.Fatalf("left=%+v right=%+v", left, right)
	}
	for i := range left {
		if left[i].AccountKey != right[i].AccountKey || left[i].DisplayLabel != right[i].DisplayLabel {
			t.Fatalf("array order changed identity/alias: left=%+v right=%+v", left, right)
		}
	}
	if left[0].AccountKey == left[1].AccountKey {
		t.Fatal("different accounts collapsed")
	}
}

func TestCollectorProviderHealthIsIndependentAndRetainsLastGoodWithinTTL(t *testing.T) {
	store := quotaTestStore()
	clock := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	collector := NewCollector(store, "mac-a", []byte("shared-test-identity-key-32-bytes-long"), nil)
	collector.SetClock(func() time.Time { return clock })
	runner := quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()})
	collector.SetRunner(runner)
	collector.SetAliases(map[string]string{
		AccountKey([]byte("shared-test-identity-key-32-bytes-long"), "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey([]byte("shared-test-identity-key-32-bytes-long"), "codex", "redacted-codex-account-b"): "Codex B",
	})
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstCount := len(store.Snapshot().Quota)
	runner.errors["zai"] = context.DeadlineExceeded
	clock = clock.Add(time.Minute)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if len(root.Quota) != firstCount || root.Sources["quota.codex"].Status != state.SourceAvailable || root.Sources["quota.zai"].Status != state.SourceDegraded || root.Sources["quota"].Status != state.SourceDegraded {
		t.Fatalf("partial health quota=%+v sources=%+v", root.Quota, root.Sources)
	}

	runner.errors["codex"] = context.DeadlineExceeded
	clock = clock.Add(LastGoodTTL + time.Second)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root = store.Snapshot()
	if root.Sources["quota.codex"].Status != state.SourceUnavailable || root.Sources["quota.zai"].Status != state.SourceUnavailable || root.Sources["quota"].Status != state.SourceUnavailable {
		t.Fatalf("expired TTL health sources=%+v", root.Sources)
	}
}

func TestCollectorInvalidCodexDoesNotMarkHealthyZAIAsFailed(t *testing.T) {
	invalid := []byte(`[{"provider":"codex","account":"Codex A","usage":{"identity":{"accountID":"redacted"},"primary":{"usedPercent":101}}}]`)
	store := quotaTestStore()
	collector := NewCollector(store, "mac-a", []byte("shared-test-identity-key-32-bytes-long"), nil)
	collector.SetRunner(quotaRunner(map[string][]byte{"codex": invalid, "zai": zaiFixture()}))
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if root.Sources["quota.codex"].Status != state.SourceUnavailable || root.Sources["quota.zai"].Status != state.SourceAvailable {
		t.Fatalf("provider statuses=%+v", root.Sources)
	}
}

func TestAccountKeyUsesSharedHMACSHA256AndSeparatesIdentities(t *testing.T) {
	key := []byte("shared-test-identity-key-32-bytes-long")
	got := AccountKey(key, "codex", "redacted-account-a")
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("codex\x00redacted-account-a"))
	want := "acct_" + hex.EncodeToString(mac.Sum(nil)[:16])
	if got != want || got == AccountKey(key, "codex", "redacted-account-b") || strings.Contains(got, "redacted") {
		t.Fatalf("account key=%q want=%q", got, want)
	}
	if got != AccountKey(key, "codex", "redacted-account-a") {
		t.Fatal("same shared key and identity must be stable")
	}
}

func TestLoadIdentityKeyNeverCreatesPerMachineSalt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.key")
	if _, err := LoadIdentityKey(path); err == nil {
		t.Fatal("missing shared key must fail instead of being generated")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing key was created: %v", err)
	}
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentityKey(path); err == nil {
		t.Fatal("short shared key must fail")
	}
}

func TestParseAliasesAcceptsOnlySafeLabelsAndAccountKeys(t *testing.T) {
	keyA := "acct_0123456789abcdef0123456789abcdef"
	keyB := "acct_fedcba9876543210fedcba9876543210"
	aliases, err := ParseAliases(keyA + "=Codex A," + keyB + "=Codex B")
	if err != nil || aliases[keyA] != "Codex A" || aliases[keyB] != "Codex B" {
		t.Fatalf("aliases=%v err=%v", aliases, err)
	}
	for _, invalid := range []string{"email@example.test=Codex A", keyA + "=Private Account", keyA + "=Codex A\nsecret"} {
		if _, err := ParseAliases(invalid); err == nil {
			t.Fatalf("invalid alias accepted: %q", invalid)
		}
	}
}

// A missing CodexBar CLI must surface as its own machine-readable reason so
// the menu bar can say "CodexBar CLI unavailable" instead of a generic quota
// failure. No provider subprocess is spawned in this state.
func TestCollectorReportsCodexBarCLIUnavailableAsDistinctReason(t *testing.T) {
	now := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	store := quotaTestStore()
	collector := NewCollector(store, "mac-a", []byte("shared-test-identity-key-32-bytes-long"), nil)
	collector.SetClock(func() time.Time { return now })
	runner := quotaRunner(nil)
	runner.errors["codex"] = ErrCodexBarCLINotFound
	runner.errors["zai"] = ErrCodexBarCLINotFound
	collector.SetRunner(runner)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	for _, id := range []string{"quota", "quota.codex", "quota.zai"} {
		source, ok := root.Sources[id]
		if !ok || source.Status != state.SourceUnavailable {
			t.Fatalf("source %s=%+v", id, source)
		}
		if source.Reason != "cli_unavailable" || source.Message != "CodexBar CLI is unavailable." {
			t.Fatalf("source %s reason/message=%+v", id, source)
		}
	}
	if len(root.Quota) != 0 {
		t.Fatalf("cli-unavailable state must not carry quota rows: %+v", root.Quota)
	}
	public := state.ProjectPublic(root, state.RuntimeCapabilities{}, state.ProjectionConfig{}, now)
	if public.Sources["quota"].Reason != "cli_unavailable" {
		t.Fatalf("public reason did not cross the projection: %+v", public.Sources["quota"])
	}
}

func TestCollectorRetainsLastGoodThenDropsQuotaWhileCLIUnavailable(t *testing.T) {
	key := []byte("shared-test-identity-key-32-bytes-long")
	store := quotaTestStore()
	clock := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	collector := NewCollector(store, "mac-a", key, nil)
	collector.SetClock(func() time.Time { return clock })
	runner := quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()})
	collector.SetRunner(runner)
	collector.SetAliases(map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	})
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	healthy := len(store.Snapshot().Quota)
	if healthy != 3 {
		t.Fatalf("healthy quota rows=%d", healthy)
	}

	// The CLI disappears (for example a Homebrew upgrade removed it). Within
	// the last-good TTL the retained data stays honest as degraded/stale with
	// the explicit CLI reason.
	runner.errors["codex"] = ErrCodexBarCLINotFound
	runner.errors["zai"] = ErrCodexBarCLINotFound
	clock = clock.Add(time.Minute)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if len(root.Quota) != healthy || root.Sources["quota"].Status != state.SourceDegraded || root.Sources["quota"].Reason != "cli_unavailable" {
		t.Fatalf("in-TTL cli-unavailable state quota=%d sources=%+v", len(root.Quota), root.Sources)
	}

	// Past the TTL the retained rows must go so an absent CLI cannot keep
	// stale quota looking connected.
	clock = clock.Add(LastGoodTTL + time.Second)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root = store.Snapshot()
	if len(root.Quota) != 0 || root.Sources["quota"].Status != state.SourceUnavailable || root.Sources["quota"].Reason != "cli_unavailable" {
		t.Fatalf("expired cli-unavailable state quota=%d sources=%+v", len(root.Quota), root.Sources)
	}
}
