package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func opaqueTaskID(provider Provider, sessionID, turnID string) string {
	h := sha256.New()
	for _, part := range []string{string(provider), sessionID, turnID} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "task-" + hex.EncodeToString(h.Sum(nil)[:16])
}

func findTask(root *state.InternalRootState, provider Provider, sessionID, turnID string) *state.TaskState {
	for i := range root.Tasks {
		t := &root.Tasks[i]
		if t.Provider == string(provider) && t.SessionID == sessionID && t.TurnID == turnID {
			return t
		}
	}
	return nil
}

func currentTask(root *state.InternalRootState, e AgentEvent) *state.TaskState {
	if e.TurnID == nil || *e.TurnID == "" {
		return nil
	}
	return findTask(root, e.Provider, e.SessionID, *e.TurnID)
}

func fallbackTaskTitle(e AgentEvent, project *state.TaskProjectContext) string {
	if e.Metadata.TaskTitle != nil && *e.Metadata.TaskTitle != "" {
		return truncateUTF8(*e.Metadata.TaskTitle, maxTaskTitleBytes)
	}
	provider := "Codex"
	if e.Provider == ProviderClaude {
		provider = "Claude Code"
	}
	if project != nil && project.ProjectName != "" {
		return truncateUTF8(project.ProjectName+" · "+provider, maxTaskTitleBytes)
	}
	return provider + " task"
}

func newTaskState(e AgentEvent, project *state.TaskProjectContext) state.TaskState {
	confidence := state.TaskConfidenceHigh
	if e.Metadata.SyntheticTurnIdentity || project == nil {
		confidence = state.TaskConfidenceDegraded
	}
	turn := ""
	if e.TurnID != nil {
		turn = *e.TurnID
	}
	t := state.TaskState{
		ID:         opaqueTaskID(e.Provider, e.SessionID, turn),
		Provider:   string(e.Provider),
		SessionID:  e.SessionID,
		TurnID:     turn,
		Project:    project,
		Title:      fallbackTaskTitle(e, project),
		Lifecycle:  state.TaskWorking,
		Freshness:  state.FreshnessFresh,
		Confidence: confidence,
		StartedAt:  e.OccurredAt,
		UpdatedAt:  e.OccurredAt,
	}
	setTaskCheckpoint(&t, state.CheckpointStarted, "Task started", e.OccurredAt, false)
	return t
}

func taskCheckpointPriority(k state.TaskCheckpointKind) int {
	switch k {
	case state.CheckpointBackgroundWait:
		return 60
	case state.CheckpointSubtaskCompleted:
		return 50
	case state.CheckpointDelegated:
		return 45
	case state.CheckpointValidating:
		return 40
	case state.CheckpointEditing, state.CheckpointInspecting:
		return 30
	case state.CheckpointRunning:
		return 10
	case state.CheckpointStarted:
		return 0
	default:
		return -1
	}
}

func checkpointText(k state.TaskCheckpointKind, subject *string) string {
	base := ""
	switch k {
	case state.CheckpointStarted:
		base = "Task started"
	case state.CheckpointInspecting:
		base = "Inspecting"
	case state.CheckpointEditing:
		base = "Editing"
	case state.CheckpointRunning:
		base = "Running"
	case state.CheckpointValidating:
		base = "Validating"
	case state.CheckpointDelegated:
		base = "Delegated work"
	case state.CheckpointSubtaskCompleted:
		base = "Subtask completed"
	case state.CheckpointBackgroundWait:
		base = "Waiting for background work"
	}
	if subject != nil && *subject != "" && (k == state.CheckpointDelegated || k == state.CheckpointSubtaskCompleted) {
		base += " · " + *subject
	}
	return truncateUTF8(base, maxTaskCheckpointBytes)
}

func setTaskCheckpoint(t *state.TaskState, k state.TaskCheckpointKind, text string, at time.Time, authoritativeResume bool) {
	if t == nil || taskCheckpointPriority(k) < 0 {
		return
	}
	text = truncateUTF8(normalizeSingleLine(text), maxTaskCheckpointBytes)
	if t.Checkpoint == nil {
		t.Checkpoint = &state.TaskCheckpoint{Kind: k, Text: text, At: at}
		return
	}
	if authoritativeResume && t.Checkpoint.Kind == state.CheckpointBackgroundWait {
		t.Checkpoint = &state.TaskCheckpoint{Kind: k, Text: text, At: at}
		return
	}
	newPriority := taskCheckpointPriority(k)
	oldPriority := taskCheckpointPriority(t.Checkpoint.Kind)
	if newPriority >= oldPriority || (!at.Before(t.Checkpoint.At) && at.Sub(t.Checkpoint.At) >= 30*time.Second) {
		t.Checkpoint = &state.TaskCheckpoint{Kind: k, Text: text, At: at}
	}
}

func toolCheckpoint(tool string) state.TaskCheckpointKind {
	name := strings.ToLower(strings.TrimSpace(tool))
	if name == "" {
		return state.CheckpointRunning
	}
	for _, generic := range []string{"bash", "shell", "exec", "command", "terminal", "run_command"} {
		if name == generic {
			return state.CheckpointRunning
		}
	}
	tokens := strings.FieldsFunc(name, func(r rune) bool {
		switch r {
		case '-', '_', '/', '.', ':', ' ':
			return true
		}
		return false
	})
	for _, token := range tokens {
		switch token {
		case "test", "tests", "testing", "pytest", "gotest", "build", "lint", "vet", "validate", "validation", "typecheck":
			return state.CheckpointValidating
		}
	}
	for _, token := range tokens {
		switch token {
		case "edit", "write", "patch", "applypatch", "apply", "replace", "multiedit", "notebookedit":
			return state.CheckpointEditing
		}
	}
	for _, token := range tokens {
		switch token {
		case "read", "search", "list", "grep", "glob", "find", "browse", "view", "ls":
			return state.CheckpointInspecting
		}
	}
	return state.CheckpointRunning
}

func checkpointForEvent(e AgentEvent) (state.TaskCheckpointKind, string, bool) {
	switch e.EventType {
	case EventPreToolUse, EventPostToolUse, EventPostToolUseFailure:
		tool := ""
		if e.Metadata.ToolName != nil {
			tool = *e.Metadata.ToolName
		}
		k := toolCheckpoint(tool)
		return k, checkpointText(k, nil), true
	case EventSubagentStart, EventTaskCreated:
		return state.CheckpointDelegated, checkpointText(state.CheckpointDelegated, e.Metadata.ChildSubject), true
	case EventSubagentStop, EventTaskCompleted:
		return state.CheckpointSubtaskCompleted, checkpointText(state.CheckpointSubtaskCompleted, e.Metadata.ChildSubject), true
	default:
		return "", "", false
	}
}

func taskAttentionForEvent(e AgentEvent) (*state.TaskAttention, bool) {
	var kind state.TaskAttentionKind
	text := ""
	switch e.EventType {
	case EventPermissionRequest:
		kind, text = state.AttentionApprovalNeeded, "Approval needed"
	case EventAskUserQuestion:
		kind, text = state.AttentionQuestionWaiting, "Question waiting"
	case EventElicitation:
		kind, text = state.AttentionElicitationWaiting, "Elicitation waiting"
	case EventNotification:
		if e.Metadata.NotificationType == nil || *e.Metadata.NotificationType == "idle_prompt" {
			return nil, false
		}
		kind, text = state.AttentionProviderActionRequired, "Provider action required"
	case EventStopFailure:
		if e.Metadata.ErrorType == nil {
			return nil, false
		}
		switch *e.Metadata.ErrorType {
		case "authentication_failed", "oauth_org_not_allowed":
			kind, text = state.AttentionAuthenticationRequired, "Authentication required"
		case "billing_error":
			kind, text = state.AttentionBillingRequired, "Billing action required"
		case "rate_limit":
			kind, text = state.AttentionRateLimited, "Rate limited"
		default:
			return nil, false
		}
	default:
		return nil, false
	}
	correlation := ""
	if e.Metadata.CorrelationID != nil {
		correlation = truncateUTF8(*e.Metadata.CorrelationID, 96)
	}
	return &state.TaskAttention{Kind: kind, Text: truncateUTF8(text, maxAttentionTextBytes), At: e.OccurredAt, CorrelationID: correlation}, true
}

func canClearTaskAttention(a *state.TaskAttention, e AgentEvent) bool {
	if a == nil || e.OccurredAt.Before(a.At) {
		return false
	}
	// ElicitationResult both resolves a pending elicitation and is
	// authoritative same-turn progress, so it clears whichever attention
	// the task currently holds (frozen §9 same-task resolution rules).
	if e.EventType == EventElicitationResult {
		return true
	}
	if e.EventType == EventStop || e.EventType == EventSessionEnd || e.EventType == EventStopFailure {
		return true
	}
	if e.EventType == EventNotification && e.Metadata.NotificationType != nil && *e.Metadata.NotificationType == "idle_prompt" {
		return true
	}
	switch e.EventType {
	case EventPreToolUse, EventPostToolUse, EventPostToolUseFailure, EventPermissionDenied, EventSubagentStart, EventSubagentStop, EventTaskCreated, EventTaskCompleted:
		if a.CorrelationID == "" || e.Metadata.CorrelationID == nil || *e.Metadata.CorrelationID == "" {
			return true
		}
		return a.CorrelationID == *e.Metadata.CorrelationID
	default:
		return false
	}
}

func syncTaskLifecycle(t *state.TaskState, a *state.AgentState, at time.Time) {
	if t == nil || a == nil {
		return
	}
	t.Freshness = a.CurrentTurn.Freshness
	switch {
	case a.CurrentTurn.Activity == state.ActivityError || a.CurrentTurn.Outcome == state.OutcomeFailed:
		t.Lifecycle = state.TaskError
	case a.CurrentTurn.Outcome == state.OutcomeCompleted:
		t.Lifecycle = state.TaskComplete
	case t.Attention != nil || a.CurrentTurn.Activity == state.ActivityAttention:
		t.Lifecycle = state.TaskLifecycleAttention
	default:
		t.Lifecycle = state.TaskWorking
	}
	t.UpdatedAt = at
}

func applyTaskEvent(t *state.TaskState, a *state.AgentState, e AgentEvent) {
	if t == nil {
		return
	}
	if att, ok := taskAttentionForEvent(e); ok {
		t.Attention = att
	} else if canClearTaskAttention(t.Attention, e) {
		t.Attention = nil
	}
	if k, text, resume := checkpointForEvent(e); k != "" {
		setTaskCheckpoint(t, k, text, e.OccurredAt, resume)
	}
	syncTaskLifecycle(t, a, e.OccurredAt)
	if t.Lifecycle != state.TaskComplete {
		t.Completion = nil
	}
}
