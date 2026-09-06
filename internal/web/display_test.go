package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func renderDisplayViewModel(t *testing.T, vm DisplayViewModel) string {
	t.Helper()
	s := testServer(t)
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "display_fragment.html", vm); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestDisplayUsesKindleContentInResponsiveColourShell(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	working := padTask("working", "codex", "Build the colour Display", state.TaskWorking, now.Add(-2*time.Minute))
	working.Checkpoint = &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Matching the Kindle task layout", At: now}
	ready := padTask("ready", "claude-code", "Review the responsive page", state.TaskLifecycleAttention, now.Add(-time.Minute))
	ready.Attention = &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Choose the viewport", At: now}
	model := padTestDashboard(padTestState(working, ready), dashboard.HostStatus("online"))
	vm := buildDisplayViewModel(model, now, false, "/display")
	body := renderDisplayViewModel(t, vm)

	for _, required := range []string{
		"display-fragment", "ACTIVE TASKS", "REQUEST", "FEEDBACK", "Build the colour Display",
		"Matching the Kindle task layout", "Review the responsive page", "ACTION REQUIRED",
		"HOST HEALTH", "CPU", "MEMORY", "SWAP", "DISK", "QUOTA", "5H", "WEEK",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("colour Display missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{"kindle-fixed-890", "kindle-viewport", "pad-task-card", "AI SIGNALS"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("colour Display retained Kindle/legacy surface marker %q", forbidden)
		}
	}
}

func TestDisplayNavigationReturnsToDisplay(t *testing.T) {
	now := padTestNow()
	task := padTask("clickable", "codex", "Open the task", state.TaskWorking, now)
	task.Navigation = &state.PublicNavigationTarget{TargetID: "opaque-agent", Kind: state.NavigationAgent, AllowedActions: []state.NavigationAction{state.ActionFocusAgent}}
	vm := buildDisplayViewModel(padTestDashboard(padTestState(task), dashboard.HostStatus("online")), now, false, "/display")
	body := renderDisplayViewModel(t, vm)
	for _, required := range []string{`method="post" action="/api/navigation"`, `class="display-task-action-overlay"`, `name="return_to" value="/display"`, "Open the task"} {
		if !strings.Contains(body, required) {
			t.Fatalf("Display navigation contract missing %q: %s", required, body)
		}
	}
}
