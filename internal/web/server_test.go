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
			t.Fatalf("leaked %q", forbidden)
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
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
func TestKindleCacheHeadersAndCompatibility(t *testing.T) {
	w := request(t, testServer(t), http.MethodGet, "/display/kindle?layout=portrait&rotate=left")
	if got := w.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control=%q", got)
	}
	body := strings.ToLower(w.Body.String())
	for _, forbidden := range []string{"<script", "fetch(", "promise", "websocket", "eventsource", "display:grid", "<canvas", "<svg", "resizeobserver", "intersectionobserver", "react", "vue"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("kindle contains forbidden %q", forbidden)
		}
	}
	for _, required := range []string{"http-equiv=\"refresh\"", "viewport-shell", "rotation-canvas", "-webkit-transform", "transform-origin"} {
		if !strings.Contains(body, required) {
			t.Fatalf("missing %q", required)
		}
	}
}
func TestKindleLayoutsAndRotationQueries(t *testing.T) {
	s := testServer(t)
	cases := map[string][]string{
		"/display/kindle?layout=portrait&rotate=none":            {"layout-portrait", "rotate-none"},
		"/display/kindle?layout=landscape&rotate=left":           {"layout-landscape", "rotate-left"},
		"/display/kindle?layout=landscape&rotate=right":          {"layout-landscape", "rotate-right"},
		"/display/kindle?layout=invalid&rotate=PRIVATE_SENTINEL": {"layout-landscape", "rotate-none"},
	}
	for path, wants := range cases {
		body := request(t, s, http.MethodGet, path).Body.String()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		if strings.Contains(body, "PRIVATE_SENTINEL") {
			t.Fatal("unsafe rotate reflected")
		}
	}
}
func TestRegisteredNonGETMethodsRejected(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/health", "/api/state", "/display", "/display/kindle"} {
		if w := request(t, s, http.MethodPost, path); w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status=%d", path, w.Code)
		}
	}
}
func TestNoNavigationEndpoint(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/navigation", "/api/navigation", "/actions/focus"} {
		if w := request(t, s, http.MethodPost, path); w.Code != http.StatusNotFound {
			t.Fatalf("navigation endpoint exists: %s", path)
		}
	}
}
func TestDisplayUsesSingleRequestClockSnapshot(t *testing.T) {
	s := testServer(t)
	base := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	calls := 0
	s.now = func() time.Time { calls++; return base.Add(time.Duration(calls) * time.Second) }
	w := request(t, s, http.MethodGet, "/display")
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "14:00:01 UTC") {
		t.Fatalf("calls=%d body=%s", calls, w.Body.String())
	}
}
func TestAPIStateUsesSingleRequestClockSnapshotAndUTCAuthority(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	instant := time.Date(2026, 8, 21, 8, 43, 0, 0, loc)
	s := testServer(t)
	calls := 0
	s.now = func() time.Time { calls++; return instant }
	w := request(t, s, http.MethodGet, "/api/state")
	if calls != 1 {
		t.Fatalf("clock calls=%d", calls)
	}
	var pub state.PublicState
	if err := json.Unmarshal(w.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if !pub.GeneratedAt.Equal(instant.UTC()) {
		t.Fatalf("GeneratedAt=%s want instant=%s", pub.GeneratedAt, instant.UTC())
	}
	if pub.GeneratedAt.Location() != time.UTC {
		t.Fatalf("GeneratedAt location=%v", pub.GeneratedAt.Location())
	}
}
func TestKindleUsesLocalClockWithSameRequestInstant(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	instant := time.Date(2026, 8, 21, 8, 43, 0, 0, loc)
	s := testServer(t)
	calls := 0
	s.now = func() time.Time { calls++; return instant }
	body := request(t, s, http.MethodGet, "/display/kindle?layout=landscape&rotate=none").Body.String()
	if calls != 1 {
		t.Fatalf("clock calls=%d", calls)
	}
	if !strings.Contains(body, "| 08:43") {
		t.Fatalf("local clock missing: %s", body)
	}
	if strings.Contains(body, "00:43") {
		t.Fatalf("UTC clock leaked into Kindle: %s", body)
	}
}
