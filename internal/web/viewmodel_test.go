package web

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestElapsedDurationUsesTerminalCompletionTime(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	complete := start.Add(10 * time.Minute)
	now := start.Add(20 * time.Minute)

	turn := state.PublicCurrentTurn{
		Activity:    state.ActivityIdle,
		Outcome:     state.OutcomeCompleted,
		Freshness:   state.FreshnessFresh,
		StartedAt:   start,
		CompletedAt: &complete,
		UpdatedAt:   complete,
	}
	if got := elapsedDuration(turn, now); got != 10*time.Minute {
		t.Fatalf("completed elapsed=%s want 10m", got)
	}
}

func TestElapsedDurationActiveStatesUseNow(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	now := start.Add(20 * time.Minute)

	for _, activity := range []state.Activity{state.ActivityWorking, state.ActivityAttention} {
		t.Run(string(activity), func(t *testing.T) {
			turn := state.PublicCurrentTurn{
				Activity:  activity,
				Outcome:   state.OutcomeNone,
				Freshness: state.FreshnessFresh,
				StartedAt: start,
				UpdatedAt: start.Add(5 * time.Minute),
			}
			if got := elapsedDuration(turn, now); got != 20*time.Minute {
				t.Fatalf("%s elapsed=%s want 20m", activity, got)
			}
		})
	}
}

func TestElapsedDurationStaleActiveUsesNow(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	now := start.Add(20 * time.Minute)
	turn := state.PublicCurrentTurn{
		Activity:  state.ActivityWorking,
		Outcome:   state.OutcomeNone,
		Freshness: state.FreshnessStale,
		StartedAt: start,
		UpdatedAt: start.Add(5 * time.Minute),
	}
	if got := elapsedDuration(turn, now); got != 20*time.Minute {
		t.Fatalf("stale active elapsed=%s want 20m", got)
	}
}

func TestElapsedDurationFailedUsesUpdatedAt(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	updated := start.Add(7 * time.Minute)
	now := start.Add(20 * time.Minute)
	turn := state.PublicCurrentTurn{
		Activity:  state.ActivityError,
		Outcome:   state.OutcomeFailed,
		Freshness: state.FreshnessFresh,
		StartedAt: start,
		UpdatedAt: updated,
	}
	if got := elapsedDuration(turn, now); got != 7*time.Minute {
		t.Fatalf("failed elapsed=%s want 7m", got)
	}
}

func TestElapsedDurationClampsMalformedTimestamps(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 10, 0, 0, time.UTC)
	before := start.Add(-time.Minute)

	cases := []state.PublicCurrentTurn{
		{
			Activity:    state.ActivityIdle,
			Outcome:     state.OutcomeCompleted,
			Freshness:   state.FreshnessFresh,
			StartedAt:   start,
			CompletedAt: &before,
			UpdatedAt:   before,
		},
		{
			Activity:  state.ActivityError,
			Outcome:   state.OutcomeFailed,
			Freshness: state.FreshnessFresh,
			StartedAt: start,
			UpdatedAt: before,
		},
		{
			Activity:  state.ActivityWorking,
			Outcome:   state.OutcomeNone,
			Freshness: state.FreshnessFresh,
			StartedAt: start,
		},
	}

	for i, turn := range cases {
		if got := elapsedDuration(turn, before); got != 0 {
			t.Fatalf("case %d elapsed=%s want 0", i, got)
		}
	}
}

func TestBuildViewModelKeepsCompletedElapsedBounded(t *testing.T) {
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	complete := start.Add(10 * time.Minute)
	now := start.Add(20 * time.Minute)
	pub := state.PublicState{
		Meta: state.DisplayMeta{CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800},
		Agents: []state.PublicAgent{{
			ID: "completed",
			CurrentTurn: state.PublicCurrentTurn{
				Activity: state.ActivityIdle, Outcome: state.OutcomeCompleted, Freshness: state.FreshnessFresh,
				StartedAt: start, CompletedAt: &complete, UpdatedAt: complete,
			},
		}},
	}
	vm := BuildViewModel(pub, now, false, "auto")
	if got := vm.Agents[0].Elapsed; got != "10m" {
		t.Fatalf("completed elapsed label=%q want 10m", got)
	}
}

func TestBuildViewModelSurfacesActiveAlertsAndHookSources(t *testing.T) {
	now := time.Now().UTC()
	turn := "t"
	pub := state.PublicState{
		Meta:   state.DisplayMeta{CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800},
		Alerts: []state.PublicAlert{{Type: state.AlertError, AgentID: "claude-code:s", TurnID: &turn, Active: true}},
		Sources: map[string]state.PublicSourceHealth{
			"codex-hooks":  {Status: state.SourceDegraded, Message: "No validated lifecycle event observed yet."},
			"claude-hooks": {Status: state.SourceAvailable},
		},
	}
	vm := BuildViewModel(pub, now, false, "auto")
	if len(vm.Alerts) != 1 || vm.Alerts[0].Type != "error" {
		t.Fatalf("alerts=%+v", vm.Alerts)
	}
	if len(vm.Sources) != 2 {
		t.Fatalf("sources=%+v", vm.Sources)
	}
}

func TestQuotaResetInfoUsesConfiguredTimezone(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 8, 24, 5, 0, 0, 0, time.UTC)
	got := quotaResetInfoWithTimezone(&reset, now, "Asia/Shanghai")
	if got != "IN 5h00m · 08/24 13:00" {
		t.Fatalf("quota reset info=%q, want Beijing display", got)
	}
	if !reset.Equal(time.Date(2026, 8, 24, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("display formatting mutated the stored reset instant: %v", reset)
	}
}
