package state

import (
	"testing"
	"time"
)

func TestDeriveDisplayStatuses(t *testing.T) {
	now := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	high := 10 * time.Minute
	retention := 30 * time.Minute
	completed5 := now.Add(-5 * time.Minute)
	completed10 := now.Add(-10 * time.Minute)
	completed30 := now.Add(-30 * time.Minute)

	cases := []struct {
		name  string
		turn  CurrentTurn
		want  DisplayStatus
		phase CompletionPhase
	}{
		{"working", CurrentTurn{Activity: ActivityWorking, Outcome: OutcomeNone, Freshness: FreshnessFresh}, DisplayWorking, CompletionNone},
		{"attention", CurrentTurn{Activity: ActivityAttention, Outcome: OutcomeNone, Freshness: FreshnessFresh}, DisplayAttention, CompletionNone},
		{"error", CurrentTurn{Activity: ActivityError, Outcome: OutcomeFailed, Freshness: FreshnessFresh}, DisplayError, CompletionNone},
		{"stale active", CurrentTurn{Activity: ActivityWorking, Outcome: OutcomeNone, Freshness: FreshnessStale}, DisplayStale, CompletionNone},
		{"complete high", CurrentTurn{Activity: ActivityIdle, Outcome: OutcomeCompleted, Freshness: FreshnessFresh, CompletedAt: &completed5}, DisplayComplete, CompletionHigh},
		{"complete boundary recent", CurrentTurn{Activity: ActivityIdle, Outcome: OutcomeCompleted, Freshness: FreshnessFresh, CompletedAt: &completed10}, DisplayComplete, CompletionRecent},
		{"complete expired", CurrentTurn{Activity: ActivityIdle, Outcome: OutcomeCompleted, Freshness: FreshnessFresh, CompletedAt: &completed30}, DisplayIdle, CompletionNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveDisplay(tc.turn, now, high, retention)
			if got.Status != tc.want || got.CompletionPhase != tc.phase {
				t.Fatalf("got %+v want status=%s phase=%s", got, tc.want, tc.phase)
			}
		})
	}
}
