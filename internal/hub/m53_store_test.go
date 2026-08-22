package hub

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// M5.2 §18/§39 acceptance 20: ONLINE/STALE/OFFLINE derive from the hub clock
// at read time, with the frozen boundary semantics.
func TestM53StoreConnectionStatusBoundaries(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)
	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil), http.StatusOK)

	cases := []struct {
		elapsed   time.Duration
		status    string
		freshness string
	}{
		{0, "online", "fresh"},
		{5 * time.Second, "online", "fresh"},
		{5*time.Second + time.Millisecond, "stale", "stale"},
		{30 * time.Second, "stale", "stale"},
		{30*time.Second + time.Millisecond, "offline", "stale"},
	}
	for _, tc := range cases {
		clock.t = m53Base.Add(tc.elapsed)
		wrapper := hostWrapper(t, fn.dashboardJSON(), "mac-a")
		if got := wrapper["source"].(map[string]any)["status"]; got != tc.status {
			t.Fatalf("elapsed %v: status=%v want=%s", tc.elapsed, got, tc.status)
		}
		if got := wrapper["snapshotFreshness"]; got != tc.freshness {
			t.Fatalf("elapsed %v: freshness=%v want=%s", tc.elapsed, got, tc.freshness)
		}
	}

	// A registered node that never produced a snapshot is OFFLINE / never
	// seen, with no fourth wire status.
	neverSeen := hostWrapper(t, fn.dashboardJSON(), "mac-b")
	source := neverSeen["source"].(map[string]any)
	if source["status"] != "offline" || source["lastSuccessAt"] != nil || neverSeen["state"] != nil {
		t.Fatalf("never-seen wrapper=%v", neverSeen)
	}
}

// M5.2 §20/§39 acceptance 21/22: last-good stays visible and explicitly stale
// through the 30-minute retention, then the nested state is dropped while the
// registered wrapper remains.
func TestM53StoreLastGoodRetentionBoundaries(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)
	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil), http.StatusOK)

	clock.t = m53Base.Add(RetentionWindow)
	wrapper := hostWrapper(t, fn.dashboardJSON(), "mac-a")
	if wrapper["state"] == nil || wrapper["snapshotFreshness"] != "stale" {
		t.Fatalf("state must remain visible exactly at retention: %v", wrapper)
	}
	if wrapper["source"].(map[string]any)["status"] != "offline" {
		t.Fatalf("retained state must be explicitly offline: %v", wrapper["source"])
	}

	clock.t = m53Base.Add(RetentionWindow + time.Second)
	wrapper = hostWrapper(t, fn.dashboardJSON(), "mac-a")
	if wrapper["state"] != nil || wrapper["snapshotFreshness"] != nil {
		t.Fatalf("state must be dropped after retention: %v", wrapper)
	}
	if wrapper["source"].(map[string]any)["status"] != "offline" {
		t.Fatalf("node must remain offline after retention: %v", wrapper["source"])
	}
	if wrapper["displayName"] != "Mac A" {
		t.Fatalf("registered wrapper must remain after retention: %v", wrapper)
	}
}

// M5.2 §14.7/§21/§39 acceptance 24: a fresh hub store starts with no accepted
// snapshots, keeps registry wrappers, and repopulates from the next valid
// snapshot regardless of its process-local sequence.
func TestM53StoreRestartModel(t *testing.T) {
	clock := newFakeClock(m53Base)
	entries := defaultRegistryEntries()

	first := newFakeNode(t, entries, clock)
	requireStatus(t, first.postSnapshot("mac-a", sessionAlpha, 42, m53Base, nil), http.StatusOK)

	second := newFakeNode(t, entries, clock)
	for _, id := range []string{"mac-a", "mac-b"} {
		wrapper := hostWrapper(t, second.dashboardJSON(), id)
		if wrapper["state"] != nil || wrapper["source"].(map[string]any)["status"] != "offline" {
			t.Fatalf("fresh hub must start empty for %s: %v", id, wrapper)
		}
	}
	requireStatus(t, second.postSnapshot("mac-a", sessionBeta, 7, clock.Now(), nil), http.StatusOK)
	if got := stateGeneratedAt(t, second.dashboardJSON(), "mac-a"); got != m53Base.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("restart repopulation failed: %s", got)
	}
}

func TestM53RegistryValidationAndOrder(t *testing.T) {
	if _, err := NewRegistry([]NodeConfig{{NodeID: "mac-a", Enabled: true, Token: testTokenA}, {NodeID: "mac-a", Enabled: true, Token: testTokenB}}); err == nil {
		t.Fatal("duplicate node id accepted")
	}
	if _, err := NewRegistry([]NodeConfig{{NodeID: "mac-a", Enabled: true, Token: testTokenA}, {NodeID: "mac-b", Enabled: true, Token: testTokenA}}); err == nil {
		t.Fatal("duplicate token accepted")
	}
	bad := []NodeConfig{
		{NodeID: "mac a", Enabled: true, Token: testTokenA},
		{NodeID: "", Enabled: true, Token: testTokenA},
		{NodeID: "mac-a", Enabled: true, Token: "short"},
		{NodeID: "mac-a", Enabled: true, Token: strings.Repeat("x", 129)},
		{NodeID: "mac-a", Enabled: true, Token: "bad charset with spaces and , = signs!"},
		{NodeID: "mac=a", Enabled: true, Token: testTokenA},
	}
	for _, entry := range bad {
		if _, err := NewRegistry([]NodeConfig{entry}); err == nil {
			t.Fatalf("invalid entry accepted: %#v", entry)
		}
	}

	registry, err := NewRegistry(defaultRegistryEntries())
	if err != nil {
		t.Fatal(err)
	}
	order := registry.Order()
	if len(order) != 2 || order[0] != "mac-a" || order[1] != "mac-b" {
		t.Fatalf("registry order unstable: %v", order)
	}
}

func TestM53RegistryAuthenticate(t *testing.T) {
	entries := append(defaultRegistryEntries(), NodeConfig{NodeID: "mac-d", Enabled: false, Token: testTokenDisabled})
	registry, err := NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	if node := registry.Authenticate(testTokenA); node == nil || node.ID != "mac-a" || !node.Enabled {
		t.Fatalf("token A must authenticate enabled mac-a: %v", node)
	}
	// A disabled node still authenticates; disabling is a binding failure,
	// not a missing credential.
	if node := registry.Authenticate(testTokenDisabled); node == nil || node.ID != "mac-d" || node.Enabled {
		t.Fatalf("disabled token must resolve to disabled mac-d: %v", node)
	}
	if node := registry.Authenticate("token-unknown-bbbbbbbbbbbbbbbbbbbbbbbbbb"); node != nil {
		t.Fatalf("unknown token authenticated: %v", node)
	}
	if node := registry.Authenticate(""); node != nil {
		t.Fatalf("empty token authenticated: %v", node)
	}
	if node := registry.Authenticate(testTokenA + strings.Repeat("x", 200)); node != nil {
		t.Fatalf("oversized token authenticated: %v", node)
	}
}

func TestM53Grammar(t *testing.T) {
	validIDs := []string{"mac-a", "Mac.B", "studio_mac-01", strings.Repeat("a", 64)}
	for _, id := range validIDs {
		if !ValidNodeID(id) {
			t.Fatalf("expected valid node id %q", id)
		}
	}
	invalidIDs := []string{"", "mac a", " mac-a", "mac-a ", "mac/a", "mac:a", strings.Repeat("a", 65), "mac-a\n"}
	for _, id := range invalidIDs {
		if ValidNodeID(id) {
			t.Fatalf("expected invalid node id %q", id)
		}
	}
	validSessions := []string{sessionAlpha, "00000000000000000000000000000000", "ffffffffffffffffffffffffffffffff"}
	for _, s := range validSessions {
		if !ValidSessionID(s) {
			t.Fatalf("expected valid session %q", s)
		}
	}
	invalidSessions := []string{"", sessionAlpha[:31], sessionAlpha + "0", strings.ToUpper(sessionAlpha), sessionAlpha[:31] + "G"}
	for _, s := range invalidSessions {
		if ValidSessionID(s) {
			t.Fatalf("expected invalid session %q", s)
		}
	}
}

// M5.2 §39 acceptance 25: no token, credential or raw request body may leak
// into responses, the dashboard or hub logs.
func TestM53PrivacyNoTokenOrBodyLeakage(t *testing.T) {
	clock := newFakeClock(m53Base)
	logBuffer := &bytes.Buffer{}
	rt, err := NewRuntime(defaultRegistryEntries(), slog.New(slog.NewTextHandler(logBuffer, nil)), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	fn := &fakeNode{t: t, rt: rt, clock: clock, tokens: map[string]string{"mac-a": testTokenA, "mac-b": testTokenB}}

	const rawBodySentinel = "RAW_BODY_SENTINEL_XYZ"
	battery := func() []string {
		var outputs []string
		outputs = append(outputs, fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), nil).Body.String())
		outputs = append(outputs, fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), nil).Body.String())
		outputs = append(outputs, fn.post([]byte("{"), testTokenA).Body.String())
		outputs = append(outputs, fn.post([]byte(`{"schemaVersion":1,"pad":"`+rawBodySentinel+`"}`), testTokenA).Body.String())
		outputs = append(outputs, fn.post(marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, clock.Now())), "token-invalid-aaaaaaaaaaaaaaaaaaaaaaa").Body.String())
		outputs = append(outputs, fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), func(env map[string]any) { env["nodeId"] = "mac-b" }).Body.String())
		env := snapshotEnvelope("mac-a", sessionAlpha, 2, clock.Now())
		env["state"].(map[string]any)["host"].(map[string]any)["displayName"] = strings.Repeat("x", MaxBodyBytes)
		outputs = append(outputs, fn.post(marshalEnvelope(t, env), testTokenA).Body.String())
		return outputs
	}
	for i, output := range battery() {
		if strings.Contains(output, testTokenA) || strings.Contains(output, testTokenB) || strings.Contains(output, rawBodySentinel) || strings.Contains(output, "Bearer") {
			t.Fatalf("response %d leaked secrets: %q", i, output)
		}
	}

	dashboard := fn.dashboardJSON()
	if strings.Contains(dashboard, testTokenA) || strings.Contains(dashboard, testTokenB) {
		t.Fatal("dashboard leaked token")
	}
	if strings.Contains(dashboard, "Bearer") {
		t.Fatal("dashboard leaked authorization header")
	}

	logs := logBuffer.String()
	for _, secret := range []string{testTokenA, testTokenB, testTokenDisabled, rawBodySentinel, "Bearer"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs leaked %q: %s", secret[:12], logs)
		}
	}

	// The dashboard shows only sanitized public state, never envelope
	// metadata like session/sequence or the receiver rejection classes.
	var parsed struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(dashboard), &parsed); err != nil {
		t.Fatal(err)
	}
	for _, host := range parsed.Hosts {
		hostJSON, _ := json.Marshal(host)
		if strings.Contains(string(hostJSON), sessionAlpha) {
			t.Fatalf("dashboard leaked session id: %s", hostJSON)
		}
	}
}
