package state

import (
	"sync"
	"testing"
	"time"
)

func TestStoreSnapshotIsolation(t *testing.T) {
	initial := MockInternalState(time.Unix(1000, 0).UTC(), HostState{ID: "host", DisplayName: "Host"})
	store := NewStore(initial)

	snap := store.Snapshot()
	snap.Agents[0].ID = "mutated"
	snap.NavigationTargets[0].AllowedActions[0] = ActionFocusApp
	snap.Sources["codex-hooks"] = SourceHealth{Status: SourceUnavailable}
	if snap.System.CPUPercent != nil {
		*snap.System.CPUPercent = 999
	}

	again := store.Snapshot()
	if again.Agents[0].ID == "mutated" || again.NavigationTargets[0].AllowedActions[0] == ActionFocusApp || again.Sources["codex-hooks"].Status == SourceUnavailable {
		t.Fatal("snapshot mutation leaked into store")
	}
	if again.System.CPUPercent != nil && *again.System.CPUPercent == 999 {
		t.Fatal("pointer mutation leaked into store")
	}
}

func TestStoreReplaceCopiesInput(t *testing.T) {
	initial := MockInternalState(time.Unix(1000, 0).UTC(), HostState{ID: "host", DisplayName: "Host"})
	store := NewStore(initial)
	next := MockInternalState(time.Unix(2000, 0).UTC(), HostState{ID: "host2", DisplayName: "Host 2"})
	store.Replace(next)
	next.Host.ID = "mutated"
	if got := store.Snapshot().Host.ID; got != "host2" {
		t.Fatalf("got %q", got)
	}
}

func TestStoreConcurrentReadWrite(t *testing.T) {
	store := NewStore(MockInternalState(time.Unix(1000, 0).UTC(), HostState{ID: "host", DisplayName: "Host"}))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if offset%2 == 0 {
					_ = store.Snapshot()
				} else {
					next := MockInternalState(time.Unix(int64(1000+j), 0).UTC(), HostState{ID: "host", DisplayName: "Host"})
					store.Replace(next)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestStoreUpdateIsAtomicAndCopiesCallbackState(t *testing.T) {
	store := NewStore(LiveInitialState(time.Unix(1000, 0).UTC(), HostState{ID: "host"}))
	var retained *InternalRootState
	if err := store.Update(func(s *InternalRootState) error {
		s.Host.DisplayName = "updated"
		retained = s
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	retained.Host.DisplayName = "leak"
	if got := store.Snapshot().Host.DisplayName; got != "updated" {
		t.Fatalf("callback retained pointer mutated store: %q", got)
	}
}

func TestStoreUpdateIfSkipsUnneededMutationAndWake(t *testing.T) {
	initial := LiveInitialState(time.Now().UTC(), HostState{ID: "host"})
	store := NewStore(initial)
	if err := store.UpdateIf(func(InternalRootState) bool { return false }, func(next *InternalRootState) error {
		next.Host.DisplayName = "must not commit"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != 0 || store.Snapshot().Host.DisplayName != "" {
		t.Fatalf("skipped update committed: revision=%d state=%+v", store.Revision(), store.Snapshot().Host)
	}
	select {
	case <-store.Changes():
		t.Fatal("skipped update emitted a wake")
	default:
	}

	if err := store.UpdateIf(func(root InternalRootState) bool { return root.Host.DisplayName == "" }, func(next *InternalRootState) error {
		next.Host.DisplayName = "committed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != 1 || store.Snapshot().Host.DisplayName != "committed" {
		t.Fatalf("conditional update did not commit: revision=%d state=%+v", store.Revision(), store.Snapshot().Host)
	}
}

func TestStoreConcurrentUpdatesDoNotLoseWrites(t *testing.T) {
	store := NewStore(LiveInitialState(time.Unix(1000, 0).UTC(), HostState{ID: "host"}))
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Update(func(s *InternalRootState) error {
				s.InternalMeta.SnapshotVersion++
				return nil
			})
		}()
	}
	wg.Wait()
	if got := store.Snapshot().InternalMeta.SnapshotVersion; got != 101 {
		t.Fatalf("snapshotVersion=%d want 101", got)
	}
}
