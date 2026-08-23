package multihost

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestM51HubDashboardContainsPeersOnlyInConfigOrder(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	store := NewPeerSnapshotStore([]config.PeerConfig{
		{ExpectedHostID: "mac-a", Endpoint: "192.168.1.50:8787"},
		{ExpectedHostID: "mac-b", Endpoint: "192.168.1.51:8787"},
	})
	pub := func(id string) state.PublicState {
		return state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: id}}
	}
	if err := store.MarkSuccess("mac-b", pub("mac-b"), now, PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuccess("mac-a", pub("mac-a"), now, PeerAvailable, "Peer snapshot available."); err != nil {
		t.Fatal(err)
	}

	dashboard := store.DashboardPeers(now)
	if len(dashboard.Hosts) != 2 || dashboard.Hosts[0].ConfiguredHostID != "mac-a" || dashboard.Hosts[1].ConfiguredHostID != "mac-b" {
		t.Fatalf("order/hosts=%#v", dashboard.Hosts)
	}
	for _, host := range dashboard.Hosts {
		if host.Source.Kind != SourcePeer {
			t.Fatalf("non-peer host in hub dashboard: %#v", host)
		}
	}
}

func TestM51HubDashboardZeroPeersHasNoImplicitLocalHost(t *testing.T) {
	dashboard := NewPeerSnapshotStore(nil).DashboardPeers(time.Unix(1000, 0).UTC())
	if dashboard.StateKind != "dashboard" || len(dashboard.Hosts) != 0 {
		t.Fatalf("dashboard=%#v", dashboard)
	}
}

func TestM51PollingDefaults(t *testing.T) {
	if PollInterval != time.Second {
		t.Fatalf("PollInterval=%s", PollInterval)
	}
	if RequestTimeout != 1500*time.Millisecond {
		t.Fatalf("RequestTimeout=%s", RequestTimeout)
	}
	if MaxBodyBytes != 256*1024 {
		t.Fatalf("MaxBodyBytes=%d", MaxBodyBytes)
	}
}

func TestM51HubMockHasNoLocalSourceKind(t *testing.T) {
	dashboard := MockHubDashboard(time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC))
	if len(dashboard.Hosts) != 2 {
		t.Fatalf("hosts=%d", len(dashboard.Hosts))
	}
	for _, host := range dashboard.Hosts {
		if host.Source.Kind != SourcePeer || host.State == nil {
			t.Fatalf("unexpected mock host=%#v", host)
		}
	}
}
