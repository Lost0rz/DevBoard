package state

import (
	"testing"
	"time"
)

// M5.2 §22 froze the minimal store change-notification contract the M5.4 node
// uplink depends on: committed mutations wake waiters, failed mutations do
// not, and notification delivery never blocks writers.

func waitForWake(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected coalesced wake signal")
	}
}

func assertNoWake(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("unexpected wake signal")
	default:
	}
}

func TestM54ReplaceSignalsChange(t *testing.T) {
	store := NewStore(LiveInitialState(time.Now().UTC(), HostState{ID: "mac-a"}))
	base := store.Revision()

	changed := store.Snapshot()
	changed.System.CPUPercent = ptrFloat(42)
	store.Replace(changed)

	if store.Revision() != base+1 {
		t.Fatalf("expected revision %d after Replace, got %d", base+1, store.Revision())
	}
	waitForWake(t, store.Changes())
}

func TestM54UpdateSignalsChangeOnlyOnCommit(t *testing.T) {
	store := NewStore(LiveInitialState(time.Now().UTC(), HostState{ID: "mac-a"}))
	base := store.Revision()

	err := store.Update(func(root *InternalRootState) error {
		root.Network.Quality = NetworkGood
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if store.Revision() != base+1 {
		t.Fatalf("expected committed update to advance revision, got %d", store.Revision())
	}
	waitForWake(t, store.Changes())

	base = store.Revision()
	err = store.Update(func(root *InternalRootState) error {
		return errAbortUpdate
	})
	if err == nil {
		t.Fatalf("expected aborted update to return its error")
	}
	if store.Revision() != base {
		t.Fatalf("aborted update must not advance revision, got %d", store.Revision())
	}
	assertNoWake(t, store.Changes())
}

var errAbortUpdate = &abortError{}

type abortError struct{}

func (*abortError) Error() string { return "abort" }

func TestM54WakeCoalescesAndNeverBlocksWriters(t *testing.T) {
	store := NewStore(LiveInitialState(time.Now().UTC(), HostState{ID: "mac-a"}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			changed := store.Snapshot()
			changed.System.CPUPercent = ptrFloat(float64(i))
			store.Replace(changed)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("bursted writers blocked on notification delivery")
	}

	if store.Revision() != 100 {
		t.Fatalf("expected 100 committed revisions, got %d", store.Revision())
	}
	// 100 coalesced writes must drain as at most one pending hint.
	waitForWake(t, store.Changes())
	assertNoWake(t, store.Changes())
}

func TestM54SnapshotDoesNotSignalChange(t *testing.T) {
	store := NewStore(LiveInitialState(time.Now().UTC(), HostState{ID: "mac-a"}))
	for i := 0; i < 3; i++ {
		_ = store.Snapshot()
	}
	if store.Revision() != 0 {
		t.Fatalf("reads must not advance revision, got %d", store.Revision())
	}
	assertNoWake(t, store.Changes())
}

func ptrFloat(v float64) *float64 { return &v }
