package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestM4DesktopTaskHierarchyAndNoOpaqueIDs(t *testing.T) {
	now := time.Date(2026, 8, 22, 4, 20, 0, 0, time.UTC)
	summary := "Implemented M4; tests pass."
	result := "abcdef1234567"
	pub := state.PublicState{Tasks: []state.PublicTask{{
		ID: "task-opaque-should-not-display", Provider: "codex",
		Project: &state.PublicTaskProject{ProjectName: "DevBoard", WorktreeLabel: "m4", Branch: "codex/m4"},
		Title:   "Implement task observability", Lifecycle: state.TaskComplete, Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh,
		StartedAt: now.Add(-20 * time.Minute), UpdatedAt: now,
		Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointValidating, Text: "Validating", At: now.Add(-time.Minute)},
		Completion: &state.PublicTaskCompletion{Summary: &summary, ResultIdentifier: &result, At: now},
	}}}
	views := buildTaskViews(pub.Tasks, now)
	if len(views) != 1 {
		t.Fatalf("views=%d", len(views))
	}
	v := views[0]
	if v.ProviderProject != "CODEX · DevBoard · m4 / codex/m4" || v.Title != "Implement task observability" || v.Lifecycle != "COMPLETE" || v.Elapsed != "20m" || v.Checkpoint != "Validating" || v.Completion != summary || v.Result != result {
		t.Fatalf("view=%+v", v)
	}
	serialized := v.ProviderProject + v.Title + v.Lifecycle + v.Elapsed + v.Checkpoint + v.Attention + v.Completion + v.Result
	if strings.Contains(serialized, pub.Tasks[0].ID) || strings.Contains(serialized, "session") || strings.Contains(serialized, "turn") {
		t.Fatalf("opaque id leaked in view: %+v", v)
	}
}

func TestM4DesktopTaskOrderingPrioritizesAction(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	pub := []state.PublicTask{
		{ID: "complete", Provider: "codex", Title: "Complete", Lifecycle: state.TaskComplete, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now},
		{ID: "working", Provider: "codex", Title: "Working", Lifecycle: state.TaskWorking, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now},
		{ID: "attention", Provider: "claude-code", Title: "Attention", Lifecycle: state.TaskLifecycleAttention, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now, Attention: &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Approval needed", At: now}},
	}
	views := buildTaskViews(pub, now)
	if views[0].Title != "Attention" || views[1].Title != "Working" || views[2].Title != "Complete" {
		t.Fatalf("order=%+v", views)
	}
}

func TestTaskAttentionLifecycleRemainsActionableWithoutFeedbackText(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	views := buildTaskViews([]state.PublicTask{{
		ID: "attention", Provider: "codex", Title: "Needs user", Lifecycle: state.TaskLifecycleAttention,
		Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now,
	}}, now)
	if len(views) != 1 || !views[0].NeedsAttention || views[0].Attention != "Action details unavailable." {
		t.Fatalf("attention fallback=%+v", views)
	}
}
