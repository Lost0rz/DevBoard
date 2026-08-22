package multihost

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestPeerSnapshotStoreDeepCopyOrderingAndIsolation(t *testing.T) {
	peers := []config.PeerConfig{{ExpectedHostID: "peer-a", Endpoint: "192.168.1.2:8787"}, {ExpectedHostID: "peer-b", Endpoint: "192.168.1.3:8787"}}
	store := NewPeerSnapshotStore(peers)
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	local := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "local", DisplayName: "Local"}, Sources: map[string]state.PublicSourceHealth{}}
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "peer-a", DisplayName: "Peer A"}, Tasks: []state.PublicTask{{ID: "task-1", Title: "original"}}, Sources: map[string]state.PublicSourceHealth{}}
	if err := store.MarkSuccess("peer-a", remote, now, PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	remote.Tasks[0].Title = "mutated"
	if err := store.MarkFailure("peer-b", now, PeerUnavailable, "Peer unavailable."); err != nil {
		t.Fatal(err)
	}

	dashboard := store.Dashboard(local, now)
	if len(dashboard.Hosts) != 3 || dashboard.Hosts[0].ConfiguredHostID != "local" || dashboard.Hosts[1].ConfiguredHostID != "peer-a" || dashboard.Hosts[2].ConfiguredHostID != "peer-b" {
		t.Fatalf("unexpected host order: %#v", dashboard.Hosts)
	}
	if dashboard.Hosts[1].State == nil || dashboard.Hosts[1].State.Tasks[0].Title != "original" {
		t.Fatal("write was not deep copied")
	}
	if dashboard.Hosts[2].State != nil || dashboard.Hosts[2].Source.Status != PeerUnavailable {
		t.Fatal("peer-b failure should not fabricate state")
	}
	dashboard.Hosts[1].State.Tasks[0].Title = "caller mutation"
	again := store.Dashboard(local, now)
	if again.Hosts[1].State.Tasks[0].Title != "original" {
		t.Fatal("snapshot was not deep copied")
	}
}

func TestPeerSnapshotFreshnessFailureRetentionAndExpiry(t *testing.T) {
	store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	local := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "local"}}
	remote := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "peer"}}
	if err := store.MarkSuccess("peer", remote, now, PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	d := store.Dashboard(local, now.Add(10*time.Second))
	if d.Hosts[1].SnapshotFreshness == nil || *d.Hosts[1].SnapshotFreshness != SnapshotFresh {
		t.Fatal("expected fresh snapshot")
	}
	if err := store.MarkFailure("peer", now.Add(11*time.Second), PeerUnavailable, "Peer unavailable."); err != nil {
		t.Fatal(err)
	}
	d = store.Dashboard(local, now.Add(12*time.Second))
	if d.Hosts[1].State == nil || d.Hosts[1].SnapshotFreshness == nil || *d.Hosts[1].SnapshotFreshness != SnapshotStale {
		t.Fatal("failed poll must retain explicitly stale last-good")
	}
	d = store.Dashboard(local, now.Add(RetentionWindow+time.Second))
	if d.Hosts[1].State != nil || d.Hosts[1].ConfiguredHostID != "peer" {
		t.Fatal("expired content must be removed while configured card remains")
	}
}

func TestPeerRecoveryReplacesLastGood(t *testing.T) {
	store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	first := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "peer"}, Tasks: []state.PublicTask{{ID: "one"}}}
	second := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now.Add(time.Minute), Host: state.PublicHost{ID: "peer"}, Tasks: []state.PublicTask{{ID: "two"}}}
	_ = store.MarkSuccess("peer", first, now, PeerAvailable, "Peer snapshot available.")
	_ = store.MarkFailure("peer", now.Add(time.Second), PeerUnavailable, "Peer unavailable.")
	_ = store.MarkSuccess("peer", second, now.Add(time.Minute), PeerAvailable, "Peer snapshot available.")
	d := store.Dashboard(state.PublicState{Host: state.PublicHost{ID: "local"}}, now.Add(time.Minute))
	if d.Hosts[1].State == nil || len(d.Hosts[1].State.Tasks) != 1 || d.Hosts[1].State.Tasks[0].ID != "two" || d.Hosts[1].Source.Status != PeerAvailable {
		t.Fatal("recovery did not atomically replace last-good")
	}
}
