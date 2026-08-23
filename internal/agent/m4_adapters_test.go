package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func normalizeRaw(t *testing.T, p Provider, raw string) (AgentEvent, bool) {
	t.Helper()
	e, ok, err := Normalize(p, []byte(raw), time.Unix(1000, 0).UTC(), "evt-test")
	if err != nil {
		t.Fatal(err)
	}
	return e, ok
}

func TestM4CodexHookMatrixExact(t *testing.T) {
	supported := map[string]EventType{
		"UserPromptSubmit":  EventUserPromptSubmit,
		"PreToolUse":        EventPreToolUse,
		"PermissionRequest": EventPermissionRequest,
		"PostToolUse":       EventPostToolUse,
		"SubagentStart":     EventSubagentStart,
		"SubagentStop":      EventSubagentStop,
		"Stop":              EventStop,
		"SessionEnd":        EventSessionEnd,
	}
	for hook, want := range supported {
		t.Run(hook, func(t *testing.T) {
			raw := `{"session_id":"s","turn_id":"t","hook_event_name":"` + hook + `"}`
			if hook == "SessionEnd" {
				raw = `{"session_id":"s","hook_event_name":"SessionEnd"}`
			}
			e, ok := normalizeRaw(t, ProviderCodex, raw)
			if !ok || e.EventType != want {
				t.Fatalf("hook %s => ok=%v event=%s", hook, ok, e.EventType)
			}
		})
	}
	for _, unsupported := range []string{"TaskCreated", "TaskCompleted", "Notification", "StopFailure", "PermissionDenied", "MessageDisplay"} {
		t.Run("reject_"+unsupported, func(t *testing.T) {
			_, ok := normalizeRaw(t, ProviderCodex, `{"session_id":"s","turn_id":"t","hook_event_name":"`+unsupported+`"}`)
			if ok {
				t.Fatalf("fabricated unsupported Codex hook %s", unsupported)
			}
		})
	}
}

func TestM4ClaudeHookMatrixAndMessageDisplayRejection(t *testing.T) {
	supported := []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest", "PermissionDenied", "Notification", "SubagentStart", "SubagentStop", "TaskCreated", "TaskCompleted", "Stop", "StopFailure", "SessionEnd", "Elicitation", "ElicitationResult"}
	for _, hook := range supported {
		t.Run(hook, func(t *testing.T) {
			extra := ""
			if hook == "Notification" {
				extra = `,"notification_type":"permission_prompt"`
			}
			if hook == "Stop" {
				extra = `,"background_tasks":[],"session_crons":[]`
			}
			raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"` + hook + `"` + extra + `}`
			if hook == "SessionEnd" {
				raw = `{"session_id":"s","hook_event_name":"SessionEnd"}`
			}
			_, ok := normalizeRaw(t, ProviderClaude, raw)
			if !ok {
				t.Fatalf("supported Claude hook ignored: %s", hook)
			}
		})
	}
	if _, ok := normalizeRaw(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"MessageDisplay","message":"PRIVATE_STREAM"}`); ok {
		t.Fatal("MessageDisplay must be rejected")
	}
}

func TestM4ClaudeAskUserQuestionMapsFromToolNameOnly(t *testing.T) {
	e, ok := normalizeRaw(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"question":"PRIVATE_QUESTION"}}`)
	if !ok || e.EventType != EventAskUserQuestion {
		t.Fatalf("event=%+v ok=%v", e, ok)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "PRIVATE_QUESTION") {
		t.Fatalf("question payload leaked: %s", b)
	}
}

func TestM4ChildIdentifiersDescriptionsAndFinalMessagesAreDiscarded(t *testing.T) {
	raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"TaskCreated","task_id":"PRIVATE_TASK_ID","agent_id":"","task_subject":"Inspect safe behavior","task_description":"PRIVATE_TASK_DESCRIPTION","last_assistant_message":"PRIVATE_CHILD_FINAL"}`
	e, ok := normalizeRaw(t, ProviderClaude, raw)
	if !ok || e.Metadata.ChildSubject == nil || *e.Metadata.ChildSubject != "Inspect safe behavior" {
		t.Fatalf("event=%+v ok=%v", e, ok)
	}
	b, _ := json.Marshal(e)
	for _, secret := range []string{"PRIVATE_TASK_ID", "PRIVATE_TASK_DESCRIPTION", "PRIVATE_CHILD_FINAL"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("child private field leaked %q: %s", secret, b)
		}
	}
}

func TestM4ClaudeSyntheticIdentityWhenPromptIDMissing(t *testing.T) {
	e, ok := normalizeRaw(t, ProviderClaude, `{"session_id":"s","hook_event_name":"UserPromptSubmit","prompt":"Audit task reducer"}`)
	if !ok || e.TurnID == nil || !strings.HasPrefix(*e.TurnID, "synthetic:") || !e.Metadata.SyntheticTurnIdentity {
		t.Fatalf("synthetic event=%+v ok=%v", e, ok)
	}
}

func TestM4AgentEventValidationRejectsCrossProviderFabrication(t *testing.T) {
	e := AgentEvent{SchemaVersion: 1, EventID: "e", Provider: ProviderCodex, SessionID: "s", TurnID: ptrString("t"), EventType: EventTaskCreated, OccurredAt: time.Unix(1, 0).UTC()}
	if err := e.Validate(); err == nil {
		t.Fatal("Codex TaskCreated accepted")
	}
}
