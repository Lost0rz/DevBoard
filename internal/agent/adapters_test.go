package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func norm(t *testing.T, p Provider, raw string) (AgentEvent, bool) {
	t.Helper()
	e, ok, err := Normalize(p, []byte(raw), time.Unix(1000, 0).UTC(), "evt-fixed")
	if err != nil {
		t.Fatal(err)
	}
	return e, ok
}
func TestCodexCoverage(t *testing.T) {
	cases := []struct {
		name  string
		event string
		turn  bool
	}{{"prompt", "UserPromptSubmit", true}, {"pre", "PreToolUse", true}, {"permission", "PermissionRequest", true}, {"post", "PostToolUse", true}, {"stop", "Stop", true}, {"session", "SessionEnd", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turn := ""
			if tc.turn {
				turn = `,"turn_id":"t1"`
			}
			raw := `{"session_id":"s1","hook_event_name":"` + tc.event + `","cwd":"/private"` + turn + `}`
			e, ok := norm(t, ProviderCodex, raw)
			if !ok || string(e.EventType) != tc.event {
				t.Fatalf("event=%+v ok=%v", e, ok)
			}
			if !tc.turn && e.TurnID != nil {
				t.Fatal("SessionEnd fabricated turn")
			}
		})
	}
}
func TestCodexUnknownAndSubagentIgnored(t *testing.T) {
	for _, raw := range []string{`{"session_id":"s","turn_id":"t","hook_event_name":"StopFailure"}`, `{"session_id":"s","turn_id":"t","hook_event_name":"Stop","agent_id":"sub"}`} {
		_, ok := norm(t, ProviderCodex, raw)
		if ok {
			t.Fatalf("expected ignored: %s", raw)
		}
	}
}
func TestClaudeCoverage(t *testing.T) {
	events := []string{"UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PostToolUseFailure", "PermissionDenied", "Notification", "Stop", "StopFailure", "SessionEnd", "Elicitation", "ElicitationResult"}
	for _, ev := range events {
		t.Run(ev, func(t *testing.T) {
			raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"` + ev + `","tool_name":"tool","notification_type":"permission_prompt","background_tasks":[],"session_crons":[]}`
			e, ok := norm(t, ProviderClaude, raw)
			if !ok || string(e.EventType) != ev {
				t.Fatalf("event=%+v ok=%v", e, ok)
			}
		})
	}
}

func TestClaudeAskUserQuestionIsDerivedFromPreToolUseOnly(t *testing.T) {
	e, ok := norm(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"PreToolUse","tool_name":"AskUserQuestion"}`)
	if !ok || e.EventType != EventAskUserQuestion {
		t.Fatalf("derived event=%+v ok=%v", e, ok)
	}
	if _, ok := norm(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"AskUserQuestion","tool_name":"AskUserQuestion"}`); ok {
		t.Fatal("AskUserQuestion must not be accepted as a raw Claude hook event")
	}
}

func TestClaudeSyntheticBegin(t *testing.T) {
	e, ok := norm(t, ProviderClaude, `{"session_id":"s","hook_event_name":"UserPromptSubmit"}`)
	if !ok || e.TurnID == nil || *e.TurnID != "synthetic:evt-fixed" || !e.Metadata.SyntheticTurnIdentity {
		t.Fatalf("%+v", e)
	}
}
func TestClaudeSubagentIgnored(t *testing.T) {
	_, ok := norm(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"Stop","agent_id":"sub"}`)
	if ok {
		t.Fatal("subagent not ignored")
	}
}
func TestClaudeStopCountsOnly(t *testing.T) {
	raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"Stop","background_tasks":[{"description":"PRIVATE_BACKGROUND","command":"PRIVATE_COMMAND"}],"session_crons":[{"prompt":"PRIVATE_CRON"}]}`
	e, ok := norm(t, ProviderClaude, raw)
	if !ok || e.Metadata.BackgroundTaskCount == nil || *e.Metadata.BackgroundTaskCount != 1 || e.Metadata.SessionCronCount == nil || *e.Metadata.SessionCronCount != 1 {
		t.Fatalf("%+v", e)
	}
	b, _ := json.Marshal(e)
	for _, s := range []string{"PRIVATE_BACKGROUND", "PRIVATE_COMMAND", "PRIVATE_CRON"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("leaked %s", s)
		}
	}
}
func TestPrivacySentinelsExcludedFromNormalizedEvent(t *testing.T) {
	raw := `{"session_id":"s","prompt_id":"p","cwd":"/private/path","hook_event_name":"StopFailure","tool_name":"Bash","error":"rate_limit","prompt":"PRIVATE_PROMPT_SENTINEL","last_assistant_message":"PRIVATE_ASSISTANT_SENTINEL","tool_input":{"command":"PRIVATE_COMMAND_SENTINEL","x":"PRIVATE_TOOL_INPUT_SENTINEL"},"tool_response":"PRIVATE_TOOL_RESPONSE_SENTINEL","transcript_path":"PRIVATE_TRANSCRIPT_SENTINEL","message":"PRIVATE_NOTIFICATION_SENTINEL","error_details":"PRIVATE_ERROR_DETAILS_SENTINEL"}`
	e, ok := norm(t, ProviderClaude, raw)
	if !ok {
		t.Fatal("not normalized")
	}
	if e.Metadata.ErrorType == nil || *e.Metadata.ErrorType != "rate_limit" {
		t.Fatalf("safe error type not normalized: %+v", e.Metadata)
	}
	b, _ := json.Marshal(e)
	for _, s := range []string{"PRIVATE_PROMPT_SENTINEL", "PRIVATE_ASSISTANT_SENTINEL", "PRIVATE_TOOL_INPUT_SENTINEL", "PRIVATE_COMMAND_SENTINEL", "PRIVATE_TOOL_RESPONSE_SENTINEL", "PRIVATE_TRANSCRIPT_SENTINEL", "PRIVATE_NOTIFICATION_SENTINEL", "PRIVATE_ERROR_DETAILS_SENTINEL"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("leaked %s: %s", s, b)
		}
	}
}
func TestNullableEnvelopeCompatible(t *testing.T) {
	var e AgentEvent
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"eventId":"x","provider":"codex","sessionId":"s","turnId":null,"eventType":"SessionEnd","occurredAt":"2026-08-20T06:16:00Z","cwd":null,"metadata":{}}`), &e); err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if e.TurnID != nil || e.Cwd != nil {
		t.Fatal("nullability lost")
	}
}

func TestFrozenM0AskUserQuestionEnvelopeIsSchemaCompatible(t *testing.T) {
	var e AgentEvent
	raw := `{"schemaVersion":1,"eventId":"evt-synth-0001","provider":"claude-code","sessionId":"session-synth-002","turnId":"turn-synth-claude-attention","eventType":"AskUserQuestion","occurredAt":"2026-08-20T06:09:40Z","cwd":"/synthetic/worktrees/gift-main","metadata":{"reason":"user-input-required","capability":"question"}}`
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}
