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
