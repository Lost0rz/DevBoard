package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func m51RoleServer(t *testing.T, role config.RuntimeRole, mock bool, peers *multihost.PeerSnapshotStore, now time.Time) *Server {
	t.Helper()
	var store *state.Store
	if role == config.RuntimeRoleNode {
		root := state.MockInternalState(now, state.HostState{ID: "mac-a", DisplayName: "Mac A"})
		store = state.NewStore(root)
	}
	s, err := NewRoleServer(store, state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, mock, nil, peers, role, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	return s
}

func TestM51NodeStateAndKindleRemainLocal(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	s := m51RoleServer(t, config.RuntimeRoleNode, false, nil, now)

	stateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(stateRec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if stateRec.Code != http.StatusOK {
		t.Fatalf("state status=%d", stateRec.Code)
	}
	var pub state.PublicState
	if err := json.Unmarshal(stateRec.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if pub.StateKind != "public" || pub.Host.ID != "mac-a" {
		t.Fatalf("unexpected node public state: %#v", pub.Host)
	}

	kindleRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(kindleRec, httptest.NewRequest(http.MethodGet, "/display/kindle", nil))
	if kindleRec.Code != http.StatusOK {
		t.Fatalf("kindle status=%d", kindleRec.Code)
	}
}

func TestM51HubHasNoLocalStateAndPeerOnlyDashboard(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	peers := multihost.NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "mac-a", Endpoint: "192.168.1.50:8787"}})
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "mac-a", DisplayName: "Mac A"}, Sources: map[string]state.PublicSourceHealth{}}
	if err := peers.MarkSuccess("mac-a", remote, now, multihost.PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	s := m51RoleServer(t, config.RuntimeRoleHub, false, peers, now)

	stateRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(stateRec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if stateRec.Code != http.StatusNotFound || strings.Contains(stateRec.Body.String(), "192.168.1.50") {
		t.Fatalf("hub state response=%d %q", stateRec.Code, stateRec.Body.String())
	}

	dashRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(dashRec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	var dashboard multihost.DashboardState
	if err := json.Unmarshal(dashRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Hosts) != 1 || dashboard.Hosts[0].ConfiguredHostID != "mac-a" || dashboard.Hosts[0].Source.Kind != multihost.SourcePeer {
		t.Fatalf("unexpected hub dashboard: %#v", dashboard)
	}
	if strings.Contains(dashRec.Body.String(), "192.168.1.50") {
		t.Fatal("peer endpoint leaked in dashboard")
	}
}

func TestM51HubZeroPeersAndKindleUnsupported(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	s := m51RoleServer(t, config.RuntimeRoleHub, false, multihost.NewPeerSnapshotStore(nil), now)

	dashRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(dashRec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	var dashboard multihost.DashboardState
	if err := json.Unmarshal(dashRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.StateKind != "dashboard" || len(dashboard.Hosts) != 0 {
		t.Fatalf("zero-peer dashboard=%#v", dashboard)
	}

	kindleRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(kindleRec, httptest.NewRequest(http.MethodGet, "/display/kindle", nil))
	if kindleRec.Code != http.StatusNotFound {
		t.Fatalf("hub kindle status=%d", kindleRec.Code)
	}
}

func TestM51HubDisplayAutoRefreshAndNoEndpointLeak(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	peers := multihost.NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "mac-a", Endpoint: "192.168.1.50:8787"}})
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "mac-a", DisplayName: "Mac A"}, Sources: map[string]state.PublicSourceHealth{}}
	_ = peers.MarkSuccess("mac-a", remote, now, multihost.PeerAvailable, "Peer snapshot available.")
	s := m51RoleServer(t, config.RuntimeRoleHub, false, peers, now)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/display", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `http-equiv="refresh" content="2"`) {
		t.Fatalf("hub display missing refresh: status=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(body, "Mac A · mac-a") || strings.Contains(body, "192.168.1.50") {
		t.Fatalf("hub display host attribution/privacy failure: %s", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func TestM51HubMockIsPeerOnlyAndDeterministic(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	s := m51RoleServer(t, config.RuntimeRoleHub, true, nil, now)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	var dashboard multihost.DashboardState
	if err := json.Unmarshal(rec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Hosts) != 2 {
		t.Fatalf("mock hosts=%d", len(dashboard.Hosts))
	}
	for _, host := range dashboard.Hosts {
		if host.Source.Kind != multihost.SourcePeer || host.ConfiguredHostID == "local" {
			t.Fatalf("hub mock contains local host: %#v", host)
		}
	}
}
