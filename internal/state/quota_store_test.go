package state

import (
	"testing"
	"time"
)

func TestStoreDeepCopiesQuotaSampledAt(t *testing.T) {
	initialAt := time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC)
	initial := LiveInitialState(initialAt, HostState{ID: "mac-a", DisplayName: "Mac A"})
	initial.Quota = []QuotaState{{
		Provider: "codex", AccountKey: "acct_test", SourceID: "quota.codex",
		SampledAt: &initialAt,
	}}
	store := NewStore(initial)

	snapshot := store.Snapshot()
	if snapshot.Quota[0].SampledAt == nil || !snapshot.Quota[0].SampledAt.Equal(initialAt) {
		t.Fatalf("initial sampledAt=%v", snapshot.Quota[0].SampledAt)
	}
	mutated := initialAt.Add(time.Hour)
	*snapshot.Quota[0].SampledAt = mutated
	if got := store.Snapshot().Quota[0].SampledAt; got == nil || !got.Equal(initialAt) {
		t.Fatalf("snapshot mutation changed store: %v", got)
	}

	if err := store.Update(func(next *InternalRootState) error {
		*next.Quota[0].SampledAt = mutated
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	updated := store.Snapshot().Quota[0].SampledAt
	if updated == nil || !updated.Equal(mutated) {
		t.Fatalf("committed update lost sampledAt: %v", updated)
	}
	*updated = initialAt
	if got := store.Snapshot().Quota[0].SampledAt; got == nil || !got.Equal(mutated) {
		t.Fatalf("returned timestamp aliases store: %v", got)
	}
}
