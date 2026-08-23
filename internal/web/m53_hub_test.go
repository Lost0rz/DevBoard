package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/state"
)

var m53WebBase = time.Date(2026, 8, 22, 11, 0, 0, 0, time.UTC)

const (
	m53TokenA = "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	m53TokenB = "token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func m53HubServer(t *testing.T, entries []hub.NodeConfig, at time.Time) (*Server, *hub.Runtime, *m53Clock) {
	t.Helper()
	clock := &m53Clock{t: at}
	rt, err := hub.NewRuntime(entries, slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Now)
	if err != nil {
		t.Fatalf("hub runtime: %v", err)
	}
	s, err := NewHubServer(state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, false, slog.New(slog.NewTextHandler(io.Discard, nil)), rt, 2)
	if err != nil {
		t.Fatalf("hub server: %v", err)
	}
	s.now = clock.Now
	return s, rt, clock
}

type m53Clock struct {
	t time.Time
}

func (c *m53Clock) Now() time.Time { return c.t }

func m53SnapshotBody(nodeID, sessionID string, sequence int, at time.Time) []byte {
	env := map[string]any{
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
	body, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	return body
}

func m53PostSnapshot(t *testing.T, s *Server, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestM53HubServerSnapshotRouteFeedsDashboard(t *testing.T) {
	entries := []hub.NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: m53TokenA},
		{NodeID: "mac-b", DisplayName: "Mac B", Enabled: true, Token: m53TokenB},
	}
	s, _, clock := m53HubServer(t, entries, m53WebBase)

	rec := m53PostSnapshot(t, s, m53SnapshotBody("mac-a", "aabbccddeeff00112233445566778899", 1, m53WebBase), m53TokenA)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Fatalf("snapshot status=%d body=%q", rec.Code, rec.Body.String())
	}

	dashRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(dashRec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashRec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d", dashRec.Code)
	}
	dashboard := dashRec.Body.String()
	if !strings.Contains(dashboard, `"configuredHostId":"mac-a"`) || !strings.Contains(dashboard, `"displayName":"Mac A"`) || !strings.Contains(dashboard, `"kind":"node"`) {
		t.Fatalf("dashboard missing mac-a node wrapper: %s", dashboard)
	}
	if !strings.Contains(dashboard, `"status":"online"`) || !strings.Contains(dashboard, `"snapshotFreshness":"fresh"`) {
		t.Fatalf("dashboard missing online fresh mac-a: %s", dashboard)
	}
	if strings.Contains(dashboard, m53TokenA) || strings.Contains(dashboard, m53TokenB) {
		t.Fatal("dashboard leaked node token")
	}

	// The hub never fabricates local NAS state.
	stateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(stateRec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if stateRec.Code != http.StatusNotFound {
		t.Fatalf("hub /api/state status=%d", stateRec.Code)
	}

	displayRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(displayRec, httptest.NewRequest(http.MethodGet, "/display", nil))
	if displayRec.Code != http.StatusOK {
		t.Fatalf("display status=%d", displayRec.Code)
	}
	display := displayRec.Body.String()
	if !strings.Contains(display, "Mac A · mac-a") {
		t.Fatalf("display missing registry label: %s", display[:400])
	}
	if strings.Contains(display, m53TokenA) {
		t.Fatal("display leaked node token")
	}

	// Node stops: wrapper goes offline through the derived clock.
	clock.t = m53WebBase.Add(31 * time.Second)
	staleRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(staleRec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if !strings.Contains(staleRec.Body.String(), `"status":"offline"`) {
		t.Fatalf("offline transition missing: %s", staleRec.Body.String())
	}

	methodRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(methodRec, httptest.NewRequest(http.MethodGet, hub.SnapshotRoute, nil))
	if methodRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET snapshot status=%d", methodRec.Code)
	}
}

func TestM53HubServerZeroRegisteredNodes(t *testing.T) {
	s, _, _ := m53HubServer(t, nil, m53WebBase)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d", rec.Code)
	}
	var dashboard struct {
		StateKind string `json:"stateKind"`
		Hosts     []any  `json:"hosts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.StateKind != "dashboard" || len(dashboard.Hosts) != 0 {
		t.Fatalf("zero-node dashboard=%+v", dashboard)
	}

	// The machine route still exists; every credential is rejected.
	invalid := m53PostSnapshot(t, s, m53SnapshotBody("mac-a", "aabbccddeeff00112233445566778899", 1, m53WebBase), "token-unregistered-aaaaaaaaaaaaaaaaaaaa")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("unregistered token status=%d", invalid.Code)
	}
}

func TestM53NodeServerDoesNotExposeSnapshotRoute(t *testing.T) {
	s, err := NewRoleServer(nil, state.ProjectionConfig{}, false, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, config.RuntimeRoleNode, 2)
	if err != nil {
		t.Fatal(err)
	}
	rec := m53PostSnapshot(t, s, m53SnapshotBody("mac-a", "aabbccddeeff00112233445566778899", 1, m53WebBase), m53TokenA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("node server snapshot route status=%d", rec.Code)
	}
}

func TestM53HubServerRejectsMockWithRuntime(t *testing.T) {
	clock := &m53Clock{t: m53WebBase}
	rt, err := hub.NewRuntime([]hub.NodeConfig{{NodeID: "mac-a", Enabled: true, Token: m53TokenA}}, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewHubServer(state.ProjectionConfig{}, true, nil, rt, 2); err == nil {
		t.Fatal("mock hub with runtime unexpectedly allowed")
	}
}
