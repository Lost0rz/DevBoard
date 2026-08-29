package hub

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/navigation"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestNavigationQueueBindsHostAndRequiresSafeSnapshot(t *testing.T) {
	registry, err := NewRegistry([]NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewNodeStateStore(registry)
	now := time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC)
	pub := state.PublicState{
		GeneratedAt:       now,
		Host:              state.PublicHost{ID: "mac-a"},
		Meta:              state.DisplayMeta{SafeNavigationEnabled: true},
		NavigationTargets: []state.PublicNavigationTarget{{TargetID: "opaque", Kind: state.NavigationAgent, AllowedActions: []state.NavigationAction{state.ActionFocusAgent}}},
	}
	if _, rej := store.Apply(registry.nodes["mac-a"], NodeSnapshot{SchemaVersion: 1, StateKind: "nodeSnapshot", NodeID: "mac-a", SessionID: "session", Sequence: 1, SentAt: now, State: pub}, [32]byte{1}, now); rej != nil {
		t.Fatalf("apply snapshot: %v", rej)
	}
	item, err := store.EnqueueNavigation("mac-a", "opaque", state.ActionFocusAgent, now)
	if err != nil || item.ID == "" {
		t.Fatalf("enqueue item=%+v err=%v", item, err)
	}
	queued := store.PendingNavigation("mac-a")
	if len(queued) != 1 || queued[0].TargetID != "opaque" {
		t.Fatalf("queued=%+v", queued)
	}
	if !store.AckNavigation("mac-a", item.ID, navigation.Result{OK: true}) || len(store.PendingNavigation("mac-a")) != 0 {
		t.Fatal("ack did not remove action")
	}
}

func TestNavigationAcknowledgementHidesClickedCompleteOnHub(t *testing.T) {
	registry, err := NewRegistry([]NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewNodeStateStore(registry)
	now := time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC)
	target := state.PublicNavigationTarget{TargetID: "opaque", Kind: state.NavigationAgent, AllowedActions: []state.NavigationAction{state.ActionFocusAgent}}
	pub := state.PublicState{
		GeneratedAt:       now,
		Host:              state.PublicHost{ID: "mac-a"},
		Meta:              state.DisplayMeta{SafeNavigationEnabled: true},
		NavigationTargets: []state.PublicNavigationTarget{target},
		Tasks: []state.PublicTask{{
			ID: "complete", Provider: "codex", Lifecycle: state.TaskComplete, Unread: true,
			Navigation: &target,
		}},
	}
	if _, rej := store.Apply(registry.nodes["mac-a"], NodeSnapshot{SchemaVersion: 1, StateKind: "nodeSnapshot", NodeID: "mac-a", SessionID: "session", Sequence: 1, SentAt: now, State: pub}, [32]byte{2}, now); rej != nil {
		t.Fatalf("apply snapshot: %v", rej)
	}
	item, err := store.EnqueueNavigationForTask("mac-a", "opaque", "complete", state.ActionFocusAgent, now)
	if err != nil {
		t.Fatalf("enqueue task navigation: %v", err)
	}
	if !store.MarkTaskRead("mac-a", item.TargetID, item.TaskID, now.Add(time.Second)) {
		t.Fatal("task read acknowledgement was not accepted")
	}
	dashboard := store.Dashboard(now.Add(2 * time.Second))
	if len(dashboard.Hosts) != 1 || dashboard.Hosts[0].State == nil || len(dashboard.Hosts[0].State.Tasks) != 1 || dashboard.Hosts[0].State.Tasks[0].Unread {
		t.Fatalf("clicked complete remained unread: %+v", dashboard.Hosts)
	}
}
