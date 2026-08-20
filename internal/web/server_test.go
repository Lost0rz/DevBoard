package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	now := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	store := state.NewStore(state.MockInternalState(now, state.HostState{ID: "host", DisplayName: "Host"}))
	server, err := NewServer(store, state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return now }
	return server
}

func request(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestHealth(t *testing.T) {
	w := request(t, testServer(t), http.MethodGet, "/health")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body=%v", body)
	}
}

func TestAPIStateIsPublicAndPrivateFree(t *testing.T) {
	s := testServer(t)
	internal := s.store.Snapshot()
	internal.Projects[0].WorktreeRoot = "/Users/private/example/project"
	internal.NavigationTargets[0].Detail.FocusLocator = "PRIVATE_FOCUS_LOCATOR_SENTINEL"
	internal.InternalMeta.PrivateNote = "PRIVATE_SECRET_SENTINEL"
	s.store.Replace(internal)
	w := request(t, s, http.MethodGet, "/api/state")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"worktreeRoot", "focusLocator", "PRIVATE_", "/Users/private/example/project"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("leaked %q in %s", forbidden, body)
		}
	}
	var pub state.PublicState
	if err := json.Unmarshal(w.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if pub.StateKind != "public" || pub.Meta.SafeNavigationEnabled {
		t.Fatalf("unexpected public state: %+v", pub.Meta)
	}
}

func TestDisplays(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/display", "/display/kindle", "/display/kindle?layout=portrait", "/display/kindle?layout=landscape", "/display/kindle?layout=bogus"} {
		w := request(t, s, http.MethodGet, path)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s status=%d headers=%v", path, w.Code, w.Header())
		}
		if !strings.Contains(w.Body.String(), "M1") {
			t.Fatalf("%s missing mock marker", path)
		}
	}
}

func TestKindleCacheHeadersAndCompatibility(t *testing.T) {
	w := request(t, testServer(t), http.MethodGet, "/display/kindle?layout=portrait")
	if got := w.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if w.Header().Get("Pragma") != "no-cache" || w.Header().Get("Expires") != "0" {
		t.Fatalf("headers=%v", w.Header())
	}
	body := strings.ToLower(w.Body.String())
	for _, forbidden := range []string{"<script", "fetch(", "websocket", "eventsource", "display:grid", "<canvas"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("kindle page contains forbidden implementation feature %q", forbidden)
		}
	}
	for _, required := range []string{"http-equiv=\"refresh\"", "action required", "working", "attention", "complete", "layout portrait"} {
		if !strings.Contains(body, required) {
			t.Fatalf("kindle page missing %q", required)
		}
	}
}

func TestKindleLayouts(t *testing.T) {
	s := testServer(t)
	cases := map[string]string{
		"/display/kindle?layout=portrait":  "layout portrait",
		"/display/kindle?layout=landscape": "layout landscape",
		"/display/kindle?layout=invalid":   "layout auto",
	}
	for path, want := range cases {
		w := request(t, s, http.MethodGet, path)
		if !strings.Contains(strings.ToLower(w.Body.String()), want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}
}

func TestRegisteredNonGETMethodsRejected(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/health", "/api/state", "/display", "/display/kindle"} {
		w := request(t, s, http.MethodPost, path)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status=%d", path, w.Code)
		}
	}
}

func TestNoNavigationEndpoint(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/navigation", "/api/navigation", "/actions/focus"} {
		w := request(t, s, http.MethodPost, path)
		if w.Code != http.StatusNotFound {
			t.Fatalf("navigation endpoint %s unexpectedly exists: %d", path, w.Code)
		}
	}
}

func TestAgentPrioritySorting(t *testing.T) {
	s := testServer(t)
	pub := s.publicState()
	vm := BuildViewModel(pub, s.now(), true, "auto")
	if len(vm.Agents) < 3 {
		t.Fatal("expected mock agents")
	}
	if vm.Agents[0].Status != state.DisplayAttention || vm.Agents[1].Status != state.DisplayComplete || vm.Agents[2].Status != state.DisplayWorking {
		t.Fatalf("unexpected order: %+v", vm.Agents)
	}
}

func TestFullPrioritySorting(t *testing.T) {
	now := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	completed := now.Add(-5 * time.Minute)
	pub := state.PublicState{
		Meta: state.DisplayMeta{CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800},
		Agents: []state.PublicAgent{
			{ID: "idle", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityIdle, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: now}},
			{ID: "working", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityWorking, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: now}},
			{ID: "complete", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityIdle, Outcome: state.OutcomeCompleted, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Hour), CompletedAt: &completed}},
			{ID: "stale", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityWorking, Outcome: state.OutcomeNone, Freshness: state.FreshnessStale, StartedAt: now}},
			{ID: "error", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityError, Outcome: state.OutcomeFailed, Freshness: state.FreshnessFresh, StartedAt: now}},
			{ID: "attention", CurrentTurn: state.PublicCurrentTurn{Activity: state.ActivityAttention, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: now}},
		},
	}
	vm := BuildViewModel(pub, now, false, "auto")
	want := []state.DisplayStatus{state.DisplayAttention, state.DisplayError, state.DisplayStale, state.DisplayComplete, state.DisplayWorking, state.DisplayIdle}
	for i, status := range want {
		if vm.Agents[i].Status != status {
			t.Fatalf("index %d got %s want %s; agents=%+v", i, vm.Agents[i].Status, status, vm.Agents)
		}
	}
}
