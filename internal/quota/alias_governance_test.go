package quota

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func aliasTestKey() []byte { return []byte("shared-test-identity-key-32-bytes-long") }

func aliasTestSpec(t *testing.T, key []byte, labels ...string) string {
	t.Helper()
	entries := make([]string, 0, len(labels))
	for i, label := range labels {
		identity := "redacted-codex-account-" + string(rune('a'+i))
		entries = append(entries, AccountKey(key, "codex", identity)+"="+label)
	}
	return strings.Join(entries, ",")
}

// codexSubsetFixture returns a one-account CodexBar payload whose visible
// account is the given identity, carrying a CodexBar-local label that is
// deliberately different from any allow-listed alias. It proves labels come
// from the alias map, never from the Node's visible account set.
func codexSubsetFixture(t *testing.T, key []byte, identity string) []byte {
	t.Helper()
	body, err := json.Marshal([]any{map[string]any{
		"provider": "codex",
		"account":  "Local Display Name",
		"usage": map[string]any{
			"identity": map[string]any{
				"accountID":    identity,
				"accountEmail": "redacted-" + identity + "@example.invalid",
				"loginMethod":  "oauth",
			},
			"primary": map[string]any{"usedPercent": 30.0, "windowMinutes": 300},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseAliasesRejectsDuplicateLabelsAndDuplicateKeys(t *testing.T) {
	keyA := "acct_0123456789abcdef0123456789abcdef"
	keyB := "acct_fedcba9876543210fedcba9876543210"
	for name, spec := range map[string]string{
		"two keys share one label": keyA + "=Codex A," + keyB + "=Codex A",
		"same key two labels":      keyA + "=Codex A," + keyA + "=Codex B",
		"exact duplicate entry":    keyA + "=Codex A," + keyA + "=Codex A",
	} {
		if _, err := ParseAliases(spec); err == nil {
			t.Fatalf("%s was accepted: %q", name, spec)
		}
	}
}

func TestCodexAccountsWithoutAliasCoverageFailClosed(t *testing.T) {
	key := aliasTestKey()
	if _, err := parseProviderWithAliases(codexFixture(false), "codex", "Codex", key, "mac-a", time.Now().UTC(), nil); !errors.Is(err, ErrAliasCoverage) {
		t.Fatalf("alias-less codex parse must fail closed, err=%v", err)
	}
	partial := map[string]string{AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A"}
	if _, err := parseProviderWithAliases(codexFixture(false), "codex", "Codex", key, "mac-a", time.Now().UTC(), partial); !errors.Is(err, ErrAliasCoverage) {
		t.Fatalf("partial alias coverage must fail closed, err=%v", err)
	}
}

func TestCrossMacCodexLabelsAreStableAcrossVisibleAccountSets(t *testing.T) {
	key := aliasTestKey()
	aliases := map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	}
	keyB := AccountKey(key, "codex", "redacted-codex-account-b")
	parse := func(body []byte) map[string]string {
		items, err := parseProviderWithAliases(body, "codex", "Codex", key, "any-node", time.Now().UTC(), aliases)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, item := range items {
			got[item.AccountKey] = item.DisplayLabel
		}
		return got
	}
	// Mac A sees accounts A+B; Mac B sees only account B. The same accountKey
	// must carry the same displayLabel regardless of which accounts a Node can
	// currently see, and reordering the payload must not re-label anything.
	macA := parse(codexFixture(false))
	macAReordered := parse(codexFixture(true))
	macB := parse(codexSubsetFixture(t, key, "redacted-codex-account-b"))
	if macA[keyB] != "Codex B" || macB[keyB] != "Codex B" || macAReordered[keyB] != "Codex B" {
		t.Fatalf("label for account B changed with the visible account set: macA=%v macB=%v reordered=%v", macA, macB, macAReordered)
	}
	for _, label := range append(valuesOf(macA), append(valuesOf(macAReordered), valuesOf(macB)...)...) {
		if !ValidateAliasLabel(label) {
			t.Fatalf("invalid display label leaked: %q", label)
		}
	}
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestCollectMarksConfigurationRequiredAndRetainsLastGood(t *testing.T) {
	key := aliasTestKey()
	store := quotaTestStore()
	clock := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	collector := NewCollector(store, "mac-a", key, nil)
	collector.SetClock(func() time.Time { return clock })
	collector.SetRunner(quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()}))
	collector.SetAliases(map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	})
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	lastGood := store.Snapshot().Quota

	// Alias coverage disappears: no possibly-wrong Codex A/B may be emitted.
	collector.SetAliases(map[string]string{AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A"})
	clock = clock.Add(time.Minute)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	codexHealth := root.Sources["quota.codex"]
	if codexHealth.Status != state.SourceDegraded || !strings.Contains(codexHealth.Message, "configuration_required") {
		t.Fatalf("missing alias must degrade as configuration_required: %+v", codexHealth)
	}
	if root.Sources["quota"].Status != state.SourceDegraded {
		t.Fatalf("aggregate quota source=%+v", root.Sources["quota"])
	}
	if len(root.Quota) != len(lastGood) {
		t.Fatalf("last-good quota not retained: got=%d want=%d", len(root.Quota), len(lastGood))
	}

	// The exact TTL boundary still retains the last-good Codex values as
	// degraded/stale. Z.ai remains healthy throughout the Codex alias failure.
	clock = time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC).Add(LastGoodTTL)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	codexHealth = store.Snapshot().Sources["quota.codex"]
	root = store.Snapshot()
	if codexHealth.Status != state.SourceDegraded || len(root.Quota) != len(lastGood) {
		t.Fatalf("TTL boundary configuration_required health=%+v quota=%+v", codexHealth, root.Quota)
	}

	// One second beyond the TTL removes only Codex quota and makes that source
	// unavailable. Z.ai is independently refreshed and remains available.
	clock = clock.Add(time.Second)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root = store.Snapshot()
	codexHealth = root.Sources["quota.codex"]
	if codexHealth.Status != state.SourceUnavailable || !strings.Contains(codexHealth.Message, "configuration_required") {
		t.Fatalf("expired configuration_required health=%+v", codexHealth)
	}
	expiredMessage := codexHealth.Message
	if root.Sources["quota.zai"].Status != state.SourceAvailable {
		t.Fatalf("Z.ai was affected by Codex alias expiry: %+v", root.Sources["quota.zai"])
	}
	for _, item := range root.Quota {
		if item.Provider == "codex" {
			t.Fatalf("expired Codex quota was retained: %+v", item)
		}
	}

	// Once aliases are restored, Codex can become available again and its
	// sanitized two-account values are repopulated.
	collector.SetAliases(map[string]string{
		AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A",
		AccountKey(key, "codex", "redacted-codex-account-b"): "Codex B",
	})
	clock = clock.Add(time.Minute)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root = store.Snapshot()
	if root.Sources["quota.codex"].Status != state.SourceAvailable {
		t.Fatalf("Codex did not recover after aliases were restored: %+v", root.Sources["quota.codex"])
	}
	if len(root.Quota) != len(lastGood) {
		t.Fatalf("Codex recovery quota=%+v want %d entries", root.Quota, len(lastGood))
	}
	// The degraded message must never carry identity material.
	if strings.Contains(expiredMessage, "redacted") || strings.Contains(expiredMessage, "example.invalid") {
		t.Fatalf("health message leaked identity: %q", expiredMessage)
	}
}

func TestCollectWithoutAliasesFromStartEmitsNoCodexQuota(t *testing.T) {
	store := quotaTestStore()
	collector := NewCollector(store, "mac-a", aliasTestKey(), nil)
	collector.SetRunner(quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()}))
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if root.Sources["quota.codex"].Status != state.SourceUnavailable || !strings.Contains(root.Sources["quota.codex"].Message, "configuration_required") {
		t.Fatalf("alias-less start must be configuration_required: %+v", root.Sources["quota.codex"])
	}
	for _, item := range root.Quota {
		if item.Provider == "codex" {
			t.Fatalf("alias-less collect emitted codex quota: %+v", item)
		}
	}
	if root.Sources["quota.zai"].Status != state.SourceAvailable {
		t.Fatalf("zai must stay independent: %+v", root.Sources["quota.zai"])
	}
}

func TestAliasAuditReportsOnlyMachineSafeFields(t *testing.T) {
	key := aliasTestKey()
	aliases := map[string]string{AccountKey(key, "codex", "redacted-codex-account-a"): "Codex A"}
	audit, err := AuditAliases(context.Background(), quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()}), key, aliases)
	if err != nil {
		t.Fatal(err)
	}
	if audit.OK || len(audit.Accounts) != 2 {
		t.Fatalf("audit ok=%v accounts=%+v", audit.OK, audit.Accounts)
	}
	statuses := map[string]string{}
	for _, entry := range audit.Accounts {
		if entry.AccountKey == "" || entry.Provider != "codex" || (entry.AliasStatus != "configured" && entry.AliasStatus != "missing") {
			t.Fatalf("unsafe or incomplete audit entry: %+v", entry)
		}
		statuses[entry.AccountKey] = entry.AliasStatus
	}
	if statuses[AccountKey(key, "codex", "redacted-codex-account-a")] != "configured" || statuses[AccountKey(key, "codex", "redacted-codex-account-b")] != "missing" {
		t.Fatalf("audit statuses=%v", statuses)
	}
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"redacted", "example.invalid", "Codex A", "Codex B", "Local Display Name"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("audit output leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCheckStartupFailsClosedOnParseAndCoverage(t *testing.T) {
	key := aliasTestKey()
	full := aliasTestSpec(t, key, "Codex A", "Codex B")
	runner := quotaRunner(map[string][]byte{"codex": codexFixture(false), "zai": zaiFixture()})
	failing := &fakeRunner{responses: map[string][]byte{}, errors: map[string]error{"codex": context.DeadlineExceeded}}

	if check := CheckStartup(context.Background(), runner, key, "not-a-valid-spec"); check.StartCollector || check.Reason != "alias_parse_failed" {
		t.Fatalf("parse failure must fail closed: %+v", check)
	}
	if check := CheckStartup(context.Background(), runner, key, ""); check.StartCollector || check.Reason != "alias_config_empty" {
		t.Fatalf("empty alias configuration must fail closed: %+v", check)
	}
	if check := CheckStartup(context.Background(), runner, key, aliasTestSpec(t, key, "Codex A")); check.StartCollector || check.Reason != "alias_coverage_missing" {
		t.Fatalf("uncovered accountKey must fail closed: %+v", check)
	}
	if check := CheckStartup(context.Background(), failing, key, full); !check.StartCollector {
		t.Fatalf("CodexBar unavailability must not block startup: %+v", check)
	}
	check := CheckStartup(context.Background(), runner, key, full)
	if !check.StartCollector || check.Reason != "" || len(check.Aliases) != 2 {
		t.Fatalf("healthy startup check=%+v", check)
	}
}

func TestMarkConfigurationRequiredRecordsDegradedSources(t *testing.T) {
	store := quotaTestStore()
	at := time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)
	if err := MarkConfigurationRequired(store, at); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	for _, sourceID := range []string{"quota", "quota.codex"} {
		health := root.Sources[sourceID]
		if health.Status != state.SourceDegraded || !strings.Contains(health.Message, "configuration_required") {
			t.Fatalf("%s health=%+v", sourceID, health)
		}
		if health.LastAttemptAt == nil || !health.LastAttemptAt.Equal(at) {
			t.Fatalf("%s lastAttempt=%v want %v", sourceID, health.LastAttemptAt, at)
		}
	}
}
