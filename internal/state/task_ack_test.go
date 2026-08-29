package state

import (
	"testing"
	"time"
)

func TestAcknowledgeTaskMarksOnlyClickedTerminalTask(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	turn := "turn-1"
	root := LiveInitialState(now, HostState{ID: "mac-a"})
	root.NavigationTargets = []NavigationTarget{{
		TargetID: "target-agent", Kind: NavigationAgent, HostID: "mac-a",
		AllowedActions: []NavigationAction{ActionFocusAgent},
		Detail:         NavigationTargetDetail{AgentID: "codex:session", Provider: "codex", SessionID: "session"},
	}}
	root.Tasks = []TaskState{
		{ID: "complete", Provider: "codex", SessionID: "session", TurnID: turn, Lifecycle: TaskComplete},
		{ID: "working", Provider: "codex", SessionID: "session", TurnID: "turn-2", Lifecycle: TaskWorking},
	}
	if !AcknowledgeTask(&root, "complete", "target-agent", now.Add(time.Second)) {
		t.Fatal("terminal task was not acknowledged")
	}
	if root.Tasks[0].ReadAt == nil {
		t.Fatal("terminal task did not receive ReadAt")
	}
	if root.Tasks[1].ReadAt != nil {
		t.Fatal("working task was acknowledged")
	}
	if AcknowledgeTask(&root, "working", "target-agent", now.Add(2*time.Second)) {
		t.Fatal("working task unexpectedly acknowledged")
	}
}
