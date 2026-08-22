package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTokenA        = "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testTokenB        = "token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testTokenDisabled = "token-cccccccccccccccccccccccccccc"
	sessionAlpha      = "aabbccddeeff00112233445566778899"
	sessionBeta       = "99887766554433221100ffeeddccbbaa"
	sessionGamma      = "0123456789abcdef0123456789abcdef"
)

var m53Base = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(at time.Time) *fakeClock { return &fakeClock{t: at} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeNode struct {
	t      *testing.T
	rt     *Runtime
	clock  *fakeClock
	tokens map[string]string
}

func defaultRegistryEntries() []NodeConfig {
	return []NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: testTokenA},
		{NodeID: "mac-b", DisplayName: "Mac B", Enabled: true, Token: testTokenB},
	}
}

func newFakeNode(t *testing.T, entries []NodeConfig, clock *fakeClock) *fakeNode {
	t.Helper()
	rt, err := NewRuntime(entries, discardLogger, clock.Now)
	if err != nil {
		t.Fatalf("new hub runtime: %v", err)
	}
	tokens := make(map[string]string, len(entries))
	for _, entry := range entries {
		tokens[entry.NodeID] = entry.Token
	}
	return &fakeNode{t: t, rt: rt, clock: clock, tokens: tokens}
}

func snapshotEnvelope(nodeID, sessionID string, sequence int, at time.Time) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"stateKind":     "nodeSnapshot",
		"nodeId":        nodeID,
		"sessionId":     sessionID,
		"sequence":      sequence,
		"sentAt":        at.UTC().Format(time.RFC3339Nano),
		"state": map[string]any{
			"schemaVersion": 1,
			"stateKind":     "public",
			"generatedAt":   at.UTC().Format(time.RFC3339Nano),
			"host":          map[string]any{"id": nodeID, "displayName": "Node " + nodeID},
			"sources":       map[string]any{},
		},
	}
}

func marshalEnvelope(t *testing.T, env map[string]any) []byte {
	body, err := json.Marshal(env)
	if err != nil && t != nil {
		t.Helper()
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}

func (fn *fakeNode) post(body []byte, token string) *httptest.ResponseRecorder {
	fn.t.Helper()
	req := httptest.NewRequest(http.MethodPost, SnapshotRoute, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fn.rt.ServeHTTP(w, req)
	return w
}

func (fn *fakeNode) postSnapshot(nodeID, sessionID string, sequence int, at time.Time, mutate func(map[string]any)) *httptest.ResponseRecorder {
	fn.t.Helper()
	env := snapshotEnvelope(nodeID, sessionID, sequence, at)
	if mutate != nil {
		mutate(env)
	}
	return fn.post(marshalEnvelope(fn.t, env), fn.tokens[nodeID])
}

func (fn *fakeNode) dashboardJSON() string {
	fn.t.Helper()
	body, err := json.Marshal(fn.rt.Store().Dashboard(fn.clock.Now()))
	if err != nil {
		fn.t.Fatalf("marshal dashboard: %v", err)
	}
	return string(body)
}

func hostWrapper(t *testing.T, dashboardJSON, nodeID string) map[string]any {
	t.Helper()
	var dashboard struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(dashboardJSON), &dashboard); err != nil {
		t.Fatalf("unmarshal dashboard: %v", err)
	}
	for _, host := range dashboard.Hosts {
		if host["configuredHostId"] == nodeID {
			return host
		}
	}
	t.Fatalf("node %q missing from dashboard: %s", nodeID, dashboardJSON)
	return nil
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status=%d want=%d body=%q", rec.Code, want, rec.Body.String())
	}
}

// M5.2 §39 acceptance 1: valid mac-a token + mac-a envelope accepted.
func TestM53FakeNodeHappyPath(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	rec := fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil)
	requireStatus(t, rec, http.StatusOK)
	if body := strings.TrimSpace(rec.Body.String()); body != `{"ok":true}` {
		t.Fatalf("ack body=%q", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("ack content-type=%q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("ack cache-control=%q", cc)
	}

	dashboard := fn.dashboardJSON()
	macA := hostWrapper(t, dashboard, "mac-a")
	if macA["state"] == nil || macA["displayName"] != "Mac A" {
		t.Fatalf("mac-a wrapper not populated: %v", macA)
	}
	source := macA["source"].(map[string]any)
	if source["kind"] != "node" || source["status"] != "online" {
		t.Fatalf("mac-a source=%v", source)
	}
	if macA["snapshotFreshness"] != "fresh" {
		t.Fatalf("mac-a freshness=%v", macA["snapshotFreshness"])
	}
	macB := hostWrapper(t, dashboard, "mac-b")
	if macB["state"] != nil {
		t.Fatalf("mac-b fabricated state: %v", macB)
	}
}

// M5.2 §39 acceptance 2/3/6: credential and disabled-node failures.
func TestM53FakeNodeAuthenticationFailures(t *testing.T) {
	clock := newFakeClock(m53Base)
	entries := append(defaultRegistryEntries(), NodeConfig{NodeID: "mac-d", DisplayName: "Mac D", Enabled: false, Token: testTokenDisabled})
	fn := newFakeNode(t, entries, clock)

	env := marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base))
	cases := []struct {
		name   string
		token  string
		header string
		want   int
	}{
		{"missing token", "", "", http.StatusUnauthorized},
		{"invalid token", "token-does-not-exist-at-all-1234567890", "", http.StatusUnauthorized},
		{"malformed scheme", "", "Token " + testTokenA, http.StatusUnauthorized},
		{"empty bearer", "", "Bearer ", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, SnapshotRoute, bytes.NewReader(env))
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		} else if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fn.rt.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s: status=%d want=%d body=%q", tc.name, w.Code, tc.want, w.Body.String())
		}
	}

	rec := fn.postSnapshot("mac-d", sessionAlpha, 1, m53Base, nil)
	requireStatus(t, rec, http.StatusForbidden)

	dashboard := fn.dashboardJSON()
	for _, id := range []string{"mac-a", "mac-b", "mac-d"} {
		wrapper := hostWrapper(t, dashboard, id)
		if wrapper["state"] != nil {
			t.Fatalf("%s state mutated by rejected requests: %v", id, wrapper)
		}
		source := wrapper["source"].(map[string]any)
		if source["lastSuccessAt"] != nil {
			t.Fatalf("%s liveness advanced by rejected requests: %v", id, source)
		}
	}
	if msg := hostWrapper(t, dashboard, "mac-d")["source"].(map[string]any)["message"]; msg != "Node disabled." {
		t.Fatalf("disabled wrapper message=%v", msg)
	}
}

// M5.2 §39 acceptance 4/5: token A must never authorize mac-b identity.
func TestM53FakeNodeIdentityBindingFailures(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"token A + envelope mac-b", func(env map[string]any) { env["nodeId"] = "mac-b" }},
		{"token A + nested host mac-b", func(env map[string]any) { env["state"].(map[string]any)["host"].(map[string]any)["id"] = "mac-b" }},
		{"token A + envelope mac-a + nested host mac-b", func(env map[string]any) { env["state"].(map[string]any)["host"].(map[string]any)["id"] = "mac-b" }},
	}
	for _, tc := range cases {
		rec := fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, tc.mutate)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status=%d body=%q", tc.name, rec.Code, rec.Body.String())
		}
	}

	dashboard := fn.dashboardJSON()
	for _, id := range []string{"mac-a", "mac-b"} {
		if wrapper := hostWrapper(t, dashboard, id); wrapper["state"] != nil {
			t.Fatalf("%s state mutated by identity failures: %v", id, wrapper)
		}
	}
}

// M5.2 §39 acceptance 7-9: malformed bodies and invalid envelopes.
func TestM53FakeNodeEnvelopeSchemaFailures(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	longID := strings.Repeat("a", 65)
	validBody := marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base))
	cases := []struct {
		name   string
		body   []byte
		mutate func(map[string]any)
	}{
		{"invalid json", []byte("{not-json"), nil},
		{"empty body", nil, nil},
		{"trailing data", append(append([]byte{}, validBody...), []byte(` {"extra":1}`)...), nil},
		{"unknown top-level field", nil, func(env map[string]any) { env["extra"] = "x" }},
		{"wrong schemaVersion", nil, func(env map[string]any) { env["schemaVersion"] = 2 }},
		{"wrong stateKind", nil, func(env map[string]any) { env["stateKind"] = "node-snapshot" }},
		{"invalid nodeId space", nil, func(env map[string]any) { env["nodeId"] = "mac a" }},
		{"invalid nodeId too long", nil, func(env map[string]any) { env["nodeId"] = longID }},
		{"invalid sessionId length", nil, func(env map[string]any) { env["sessionId"] = "aabb" }},
		{"invalid sessionId charset", nil, func(env map[string]any) { env["sessionId"] = strings.ToUpper(sessionAlpha) }},
		{"zero sequence", nil, func(env map[string]any) { env["sequence"] = 0 }},
		{"missing sentAt", nil, func(env map[string]any) { delete(env, "sentAt") }},
		{"nested wrong schemaVersion", nil, func(env map[string]any) { env["state"].(map[string]any)["schemaVersion"] = 2 }},
		{"nested internal kind", nil, func(env map[string]any) { env["state"].(map[string]any)["stateKind"] = "internal" }},
		{"nested empty host id", nil, func(env map[string]any) { env["state"].(map[string]any)["host"].(map[string]any)["id"] = "" }},
		{"nested missing generatedAt", nil, func(env map[string]any) { delete(env["state"].(map[string]any), "generatedAt") }},
		{"nested duplicate task ids", nil, func(env map[string]any) {
			env["state"].(map[string]any)["tasks"] = []any{
				map[string]any{"id": "t-1", "title": "a"},
				map[string]any{"id": "t-1", "title": "b"},
			}
		}},
		{"nested empty task id", nil, func(env map[string]any) {
			env["state"].(map[string]any)["tasks"] = []any{map[string]any{"id": "", "title": "a"}}
		}},
		{"nested duplicate agent ids", nil, func(env map[string]any) {
			env["state"].(map[string]any)["agents"] = []any{
				map[string]any{"id": "codex:s1"},
				map[string]any{"id": "codex:s1"},
			}
		}},
		{"nested empty agent id", nil, func(env map[string]any) {
			env["state"].(map[string]any)["agents"] = []any{map[string]any{"id": ""}}
		}},
		{"sentAt differs from generatedAt", nil, func(env map[string]any) {
			env["sentAt"] = m53Base.Add(time.Second).UTC().Format(time.RFC3339Nano)
		}},
		{"future beyond tolerance", nil, func(env map[string]any) { replaceTimes(env, m53Base.Add(FutureTolerance+time.Second)) }},
	}
	for _, tc := range cases {
		body := tc.body
		if body == nil && tc.mutate != nil {
			env := snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base)
			tc.mutate(env)
			body = marshalEnvelope(t, env)
		}
		rec := fn.post(body, testTokenA)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%q", tc.name, rec.Code, rec.Body.String())
		}
	}

	dashboard := fn.dashboardJSON()
	if wrapper := hostWrapper(t, dashboard, "mac-a"); wrapper["state"] != nil {
		t.Fatalf("mac-a state mutated by schema failures: %v", wrapper)
	}
}

func replaceTimes(env map[string]any, at time.Time) {
	env["sentAt"] = at.UTC().Format(time.RFC3339Nano)
	env["state"].(map[string]any)["generatedAt"] = at.UTC().Format(time.RFC3339Nano)
}

// M5.2 §39 acceptance 10 + §7: request body bound and transport classes.
func TestM53FakeNodeTransportBoundFailures(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	oversize := marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base))
	oversize = append(oversize, []byte(strings.Repeat(" ", 4))...)
	oversize = append(oversize, []byte(` `)...)
	padded := snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base)
	padded["state"].(map[string]any)["host"].(map[string]any)["displayName"] = strings.Repeat("x", MaxBodyBytes)
	oversizeValidJSON := marshalEnvelope(t, padded)
	if len(oversizeValidJSON) <= MaxBodyBytes {
		t.Fatalf("oversize fixture too small: %d", len(oversizeValidJSON))
	}
	exactLimit := make([]byte, MaxBodyBytes)
	copy(exactLimit, oversize)

	cases := []struct {
		name    string
		method  string
		query   string
		body    []byte
		headers map[string]string
		want    int
	}{
		{"wrong method GET", http.MethodGet, "", nil, nil, http.StatusMethodNotAllowed},
		{"wrong method PUT", http.MethodPut, "", nil, nil, http.StatusMethodNotAllowed},
		{"query string", http.MethodPost, "x=1", nil, nil, http.StatusBadRequest},
		{"wrong content type", http.MethodPost, "", marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base)), map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"missing content type", http.MethodPost, "", marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base)), map[string]string{"Content-Type": ""}, http.StatusUnsupportedMediaType},
		{"oversize body valid json", http.MethodPost, "", oversizeValidJSON, nil, http.StatusRequestEntityTooLarge},
		{"oversize body invalid json", http.MethodPost, "", exactLimit, nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, SnapshotRoute, bytes.NewReader(tc.body))
		if tc.query != "" {
			req.URL.RawQuery = tc.query
		}
		if ct, ok := tc.headers["Content-Type"]; ok {
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+testTokenA)
		w := httptest.NewRecorder()
		fn.rt.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s: status=%d want=%d body=%q", tc.name, w.Code, tc.want, w.Body.String())
		}
		if tc.want == http.StatusMethodNotAllowed && w.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("405 missing Allow header")
		}
	}

	// Exactly the bound with valid JSON still parses; malformed content at
	// exactly the bound is a payload error, not a size error (both bounded).
	rec := fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil)
	requireStatus(t, rec, http.StatusOK)

	if wrapper := hostWrapper(t, fn.dashboardJSON(), "mac-a"); wrapper["state"] == nil {
		t.Fatal("mac-a never accepted after transport failures")
	}
}

// M5.2 §13 boundary: the admission window and future tolerance edges.
func TestM53FakeNodeTimestampBoundaries(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	rec := fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base.Add(-AdmissionWindow), nil)
	requireStatus(t, rec, http.StatusOK)

	rec = fn.postSnapshot("mac-a", sessionAlpha, 2, m53Base.Add(-AdmissionWindow-time.Millisecond), nil)
	requireStatus(t, rec, http.StatusConflict)

	// Node clock skew within tolerance is still accepted.
	rec = fn.postSnapshot("mac-a", sessionAlpha, 3, m53Base.Add(FutureTolerance), nil)
	requireStatus(t, rec, http.StatusOK)
}

// wireExample is the frozen Docs/contracts/node-snapshot-v1.example.json
// body; the strict decoder must accept the complete nested PublicState.
const wireExample = `{
  "schemaVersion": 1,
  "stateKind": "nodeSnapshot",
  "nodeId": "mac-a",
  "sessionId": "0123456789abcdef0123456789abcdef",
  "sequence": 1,
  "sentAt": "2026-08-22T10:30:00Z",
  "state": {
    "schemaVersion": 1,
    "stateKind": "public",
    "generatedAt": "2026-08-22T10:30:00Z",
    "host": {
      "id": "mac-a",
      "displayName": "MAC A"
    },
    "agents": [],
    "tasks": [],
    "alerts": [],
    "system": {
      "cpuPercent": null,
      "memory": {
        "usedBytes": null,
        "totalBytes": null,
        "percentUsed": null
      },
      "swap": {
        "usedBytes": null,
        "totalBytes": null,
        "percentUsed": null
      },
      "disk": {
        "usedBytes": null,
        "totalBytes": null,
        "percentUsed": null
      },
      "processGroups": []
    },
    "network": {
      "quality": "good",
      "reachable": null,
      "connectLatencyMs": null,
      "probeFailurePercent": null,
      "receiveBytesPerSecond": null,
      "sendBytesPerSecond": null
    },
    "projects": [],
    "quota": [],
    "sources": {},
    "navigationTargets": [],
    "meta": {
      "displayContractVersion": 1,
      "kindleRefreshSeconds": 20,
      "completeHighVisibilitySeconds": 600,
      "completeRetentionSeconds": 1800,
      "safeNavigationEnabled": false,
      "wakeLockMode": "best-effort"
    }
  }
}`

func TestM53FakeNodeAcceptsFrozenWireExample(t *testing.T) {
	at := time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)
	clock := newFakeClock(at)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	rec := fn.post([]byte(wireExample), testTokenA)
	requireStatus(t, rec, http.StatusOK)

	wrapper := hostWrapper(t, fn.dashboardJSON(), "mac-a")
	state := wrapper["state"].(map[string]any)
	if state == nil {
		t.Fatal("wire example not stored")
	}
	if host := state["host"].(map[string]any); host["displayName"] != "MAC A" {
		t.Fatalf("nested state lost fields: %v", state)
	}
	if meta := state["meta"].(map[string]any); meta["kindleRefreshSeconds"] != float64(20) {
		t.Fatalf("nested meta lost fields: %v", meta)
	}

	// Exact retry of the frozen example bytes stays idempotent.
	rec = fn.post([]byte(wireExample), testTokenA)
	requireStatus(t, rec, http.StatusOK)
}
