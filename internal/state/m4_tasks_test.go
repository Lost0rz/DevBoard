package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func strp(s string) *string { return &s }

func TestM4PublicTaskAllowListAndPrivacy(t *testing.T) {
	now := time.Date(2026, 8, 22, 4, 0, 0, 0, time.UTC)
	root := LiveInitialState(now, HostState{ID: "host", DisplayName: "Host"})
	root.Tasks = []TaskState{{
		ID: "task-safe", Provider: "codex", SessionID: "PRIVATE_SESSION", TurnID: "PRIVATE_TURN",
		Project: &TaskProjectContext{ProjectName: "DevBoard", WorktreeLabel: "m4", Branch: "codex/m4", WorktreeIdentity: "PRIVATE_WORKTREE"},
		Title:   "Implement task observability", Lifecycle: TaskWorking, Freshness: FreshnessFresh, Confidence: TaskConfidenceHigh,
		StartedAt: now, UpdatedAt: now,
		Checkpoint: &TaskCheckpoint{Kind: CheckpointEditing, Text: "Editing", At: now},
		Attention:  &TaskAttention{Kind: AttentionApprovalNeeded, Text: "Approval needed", At: now, CorrelationID: "PRIVATE_CORRELATION"},
		Completion: &TaskCompletion{Summary: strp("Implemented task board; tests pass."), ResultIdentifier: strp("abcdef1234567"), At: now},
	}}
	pub := ProjectPublic(root, RuntimeCapabilities{}, ProjectionConfig{}, now)
	if len(pub.Tasks) != 1 {
		t.Fatalf("tasks=%d", len(pub.Tasks))
	}
	b, err := json.Marshal(pub.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, secret := range []string{"PRIVATE_SESSION", "PRIVATE_TURN", "PRIVATE_WORKTREE", "PRIVATE_CORRELATION", "sessionId", "turnId", "worktreeIdentity", "correlationId"} {
		if strings.Contains(text, secret) {
			t.Fatalf("private field leaked: %q in %s", secret, text)
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"id": true, "provider": true, "project": true, "title": true, "lifecycle": true, "freshness": true, "confidence": true, "startedAt": true, "updatedAt": true, "checkpoint": true, "attention": true, "completion": true}
	if len(obj) != len(want) {
		t.Fatalf("public task keys=%v", obj)
	}
	for k := range obj {
		if !want[k] {
			t.Fatalf("unexpected public task key %q", k)
		}
	}
}

func TestM4TaskStoreDeepClone(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	root := LiveInitialState(now, HostState{ID: "h"})
	root.Tasks = []TaskState{{ID: "t", Project: &TaskProjectContext{ProjectName: "P"}, Checkpoint: &TaskCheckpoint{Kind: CheckpointStarted, At: now}, Attention: &TaskAttention{Kind: AttentionApprovalNeeded, Text: "Approval needed", At: now}, Completion: &TaskCompletion{Summary: strp("Done"), At: now}}}
	st := NewStore(root)
	snap := st.Snapshot()
	snap.Tasks[0].Project.ProjectName = "MUTATED"
	snap.Tasks[0].Checkpoint.Kind = CheckpointRunning
	snap.Tasks[0].Attention.Text = "MUTATED"
	*snap.Tasks[0].Completion.Summary = "MUTATED"
	got := st.Snapshot().Tasks[0]
	if got.Project.ProjectName != "P" || got.Checkpoint.Kind != CheckpointStarted || got.Attention.Text != "Approval needed" || *got.Completion.Summary != "Done" {
		t.Fatalf("store alias leak: %+v", got)
	}
}

func TestM4LiveInitialStateKeepsSchemaAndEmptyTasks(t *testing.T) {
	root := LiveInitialState(time.Unix(0, 0).UTC(), HostState{ID: "h"})
	if root.SchemaVersion != 1 || root.Tasks == nil || len(root.Tasks) != 0 {
		t.Fatalf("unexpected initial state: schema=%d tasks=%v", root.SchemaVersion, root.Tasks)
	}
}
