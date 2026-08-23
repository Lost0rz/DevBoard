package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestM5DashboardViewScopesSameTaskIDByHost(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	task := state.PublicTask{ID: "same-task", Provider: "codex", Title: "Shared title", Lifecycle: state.TaskWorking, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now}
	local := state.PublicState{Host: state.PublicHost{ID: "local", DisplayName: "Local"}, Tasks: []state.PublicTask{task}}
	remoteTask := task
	remoteTask.Attention = &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Approve remote", At: now}
	remoteTask.Lifecycle = state.TaskLifecycleAttention
	remote := state.PublicState{Host: state.PublicHost{ID: "peer", DisplayName: "Peer"}, Tasks: []state.PublicTask{remoteTask}}
	fresh := multihost.SnapshotFresh
	dashboard := multihost.DashboardState{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: []multihost.DashboardHostSnapshot{
		{ConfiguredHostID: "local", Source: multihost.DashboardHostSource{Kind: multihost.SourceLocal, Status: multihost.PeerAvailable}, SnapshotFreshness: &fresh, State: &local},
		{ConfiguredHostID: "peer", Source: multihost.DashboardHostSource{Kind: multihost.SourcePeer, Status: multihost.PeerAvailable}, SnapshotFreshness: &fresh, State: &remote},
	}}
	vm := buildDashboardViewModel(dashboard, now, false)
	if len(vm.Hosts) != 2 || len(vm.Hosts[0].View.Tasks) != 1 || len(vm.Hosts[1].View.Tasks) != 1 {
		t.Fatalf("unexpected view model: %#v", vm)
	}
	if vm.Hosts[0].View.Tasks[0].ScopedKey == vm.Hosts[1].View.Tasks[0].ScopedKey {
		t.Fatal("same task id on different hosts collapsed to same view identity")
	}
	if len(vm.Attention) != 1 || !strings.Contains(vm.Attention[0].HostLabel, "Peer") {
		t.Fatalf("global attention lost host attribution: %#v", vm.Attention)
	}
}

func TestM5DisplayShowsHostsAndHidesPeerEndpoint(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	peer := config.PeerConfig{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}
	peers := multihost.NewPeerSnapshotStore([]config.PeerConfig{peer})
	remote := state.PublicState{
		SchemaVersion: 1,
		StateKind:     "public",
		GeneratedAt:   now,
		Host:          state.PublicHost{ID: "peer", DisplayName: "Remote Mac"},
		Tasks:         []state.PublicTask{{ID: "remote-task", Provider: "claude-code", Title: "Remote task", Lifecycle: state.TaskLifecycleAttention, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now, Attention: &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Approval required", At: now}}},
	}
	if err := peers.MarkSuccess("peer", remote, now, multihost.PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	server := m5TestServer(t, false, peers, now)
	req := httptest.NewRequest(http.MethodGet, "/display", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("display status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, required := range []string{"GLOBAL ATTENTION", "Local Mac · local", "Remote Mac · peer", "Remote task", "Approval required"} {
		if !strings.Contains(body, required) {
			t.Fatalf("display missing %q", required)
		}
	}
	for _, forbidden := range []string{"192.168.1.2", "8787", peer.Endpoint} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("peer transport endpoint leaked: %q", forbidden)
		}
	}
}

func TestM5FutureLastSeenClampsToZero(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	if got := formatPeerLastSeen(&future, now); got != "LAST RECEIVED 0s AGO" {
		t.Fatalf("future elapsed = %q", got)
	}
}
