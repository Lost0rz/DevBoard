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

func m5TestServer(t *testing.T, mock bool, peers *multihost.PeerSnapshotStore, now time.Time) *Server {
	t.Helper()
	root := state.MockInternalState(now, state.HostState{ID: "local", DisplayName: "Local Mac"})
	server, err := NewServerWithDashboard(state.NewStore(root), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, mock, nil, peers)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return now }
	return server
}

func TestM5APIStateRemainsLocalOnlyAndDashboardAggregates(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	peers := multihost.NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "peer", DisplayName: "Peer Mac"}, Sources: map[string]state.PublicSourceHealth{}}
	if err := peers.MarkSuccess("peer", remote, now, multihost.PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	server := m5TestServer(t, false, peers, now)

	stateReq := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	stateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK || stateRec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("state response = %d %q", stateRec.Code, stateRec.Header().Get("Cache-Control"))
	}
	var local state.PublicState
	if err := json.Unmarshal(stateRec.Body.Bytes(), &local); err != nil {
		t.Fatal(err)
	}
	if local.StateKind != "public" || local.Host.ID != "local" || strings.Contains(stateRec.Body.String(), "Peer Mac") {
		t.Fatalf("/api/state leaked aggregate state: %s", stateRec.Body.String())
	}

	dashReq := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	dashRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(dashRec, dashReq)
	if dashRec.Code != http.StatusOK || dashRec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dashboard response = %d %q", dashRec.Code, dashRec.Header().Get("Cache-Control"))
	}
	var dashboard multihost.DashboardState
	if err := json.Unmarshal(dashRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.StateKind != "dashboard" || len(dashboard.Hosts) != 2 || dashboard.Hosts[0].ConfiguredHostID != "local" || dashboard.Hosts[1].ConfiguredHostID != "peer" {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}
}

func TestM5KindleRemainsLocalOnly(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	peerID := "peer-secret-card"
	peers := multihost.NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: peerID, Endpoint: "192.168.1.2:8787"}})
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: peerID, DisplayName: "Remote Only"}}
	_ = peers.MarkSuccess(peerID, remote, now, multihost.PeerAvailable, "Peer snapshot available.")
	withPeers := m5TestServer(t, false, peers, now)
	withoutPeers := m5TestServer(t, false, nil, now)

	render := func(s *Server) string {
		req := httptest.NewRequest(http.MethodGet, "/display/kindle", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("kindle status = %d", rec.Code)
		}
		return rec.Body.String()
	}
	a := render(withPeers)
	b := render(withoutPeers)
	if a != b || strings.Contains(a, peerID) || strings.Contains(a, "Remote Only") {
		t.Fatal("peer state changed Kindle local-only output")
	}
}

func TestM5MockDashboardHasExactlyTwoHostsAndFrozenScenario(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	server := m5TestServer(t, true, nil, now)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	var dashboard multihost.DashboardState
	if err := json.Unmarshal(rec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Hosts) != 2 || dashboard.Hosts[0].State == nil || dashboard.Hosts[1].Source.Status != multihost.PeerDegraded || dashboard.Hosts[1].State == nil {
		t.Fatalf("unexpected mock dashboard: %#v", dashboard)
	}
	if dashboard.Hosts[1].SnapshotFreshness == nil || *dashboard.Hosts[1].SnapshotFreshness != multihost.SnapshotStale {
		t.Fatal("mock remote must be retained stale/degraded")
	}
	localTasks := dashboard.Hosts[0].State.Tasks
	if len(localTasks) != 2 || localTasks[0].Lifecycle != state.TaskWorking || localTasks[1].Lifecycle != state.TaskComplete || localTasks[1].Completion == nil {
		t.Fatalf("mock local scenario missing active/recent completion: %#v", localTasks)
	}
	remoteTasks := dashboard.Hosts[1].State.Tasks
	if len(remoteTasks) != 1 || remoteTasks[0].Attention == nil || remoteTasks[0].Lifecycle != state.TaskLifecycleAttention {
		t.Fatalf("mock remote scenario missing attention: %#v", remoteTasks)
	}
}
