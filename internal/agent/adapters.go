package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type arrayCount int

func (c *arrayCount) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf("expected array")
	}
	n := 0
	for d.More() {
		if err := skipJSONValue(d); err != nil {
			return err
		}
		n++
	}
	tok, err = d.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim(']') {
		return fmt.Errorf("unterminated array")
	}
	*c = arrayCount(n)
	return nil
}
func skipJSONValue(d *json.Decoder) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	var end json.Delim
	switch delim {
	case '{':
		end = '}'
	case '[':
		end = ']'
	default:
		return nil
	}
	for d.More() {
		if delim == '{' {
			if _, err := d.Token(); err != nil {
				return err
			}
		}
		if err := skipJSONValue(d); err != nil {
			return err
		}
	}
	tok, err = d.Token()
	if err != nil {
		return err
	}
	if tok != end {
		return fmt.Errorf("invalid json")
	}
	return nil
}

type codexPayload struct {
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	ToolName       string `json:"tool_name"`
	ToolUseID      string `json:"tool_use_id"`
	StopHookActive bool   `json:"stop_hook_active"`
	AgentID        string `json:"agent_id"`
}

type claudePayload struct {
	SessionID        string      `json:"session_id"`
	PromptID         string      `json:"prompt_id"`
	Cwd              string      `json:"cwd"`
	HookEventName    string      `json:"hook_event_name"`
	ToolName         string      `json:"tool_name"`
	AgentID          string      `json:"agent_id"`
	NotificationType string      `json:"notification_type"`
	ErrorType        string      `json:"error_type"`
	Error            string      `json:"error"`
	BackgroundTasks  *arrayCount `json:"background_tasks"`
	SessionCrons     *arrayCount `json:"session_crons"`
}

func Normalize(provider Provider, raw []byte, occurredAt time.Time, eventID string) (AgentEvent, bool, error) {
	if eventID == "" {
		return AgentEvent{}, false, fmt.Errorf("event id required")
	}
	occurredAt = occurredAt.UTC()
	switch provider {
	case ProviderCodex:
		return normalizeCodex(raw, occurredAt, eventID)
	case ProviderClaude:
		return normalizeClaude(raw, occurredAt, eventID)
	default:
		return AgentEvent{}, false, fmt.Errorf("unsupported provider")
	}
}
func normalizeCodex(raw []byte, at time.Time, eventID string) (AgentEvent, bool, error) {
	var p codexPayload
	if err := decodeOne(raw, &p); err != nil {
		return AgentEvent{}, false, err
	}
	if strings.TrimSpace(p.SessionID) == "" || strings.TrimSpace(p.HookEventName) == "" {
		return AgentEvent{}, false, fmt.Errorf("missing required codex fields")
	}
	if p.AgentID != "" {
		return AgentEvent{}, false, nil
	}
	et, supported := mapCodexEvent(p.HookEventName)
	if !supported {
		return AgentEvent{}, false, nil
	}
	if et != EventSessionEnd && p.TurnID == "" {
		return AgentEvent{}, false, fmt.Errorf("codex %s missing turn_id", et)
	}
	m := Metadata{}
	if p.ToolName != "" {
		m.ToolName = ptrString(p.ToolName)
	}
	if et == EventStop {
		m.StopHookActive = ptrBool(p.StopHookActive)
	}
	e := AgentEvent{SchemaVersion: 1, EventID: eventID, Provider: ProviderCodex, SessionID: p.SessionID, TurnID: ptrString(p.TurnID), EventType: et, OccurredAt: at, Cwd: ptrString(p.Cwd), Metadata: m}
	return e, true, nil
}
func mapCodexEvent(s string) (EventType, bool) {
	switch s {
	case "UserPromptSubmit":
		return EventUserPromptSubmit, true
	case "PreToolUse":
		return EventPreToolUse, true
	case "PermissionRequest":
		return EventPermissionRequest, true
	case "PostToolUse":
		return EventPostToolUse, true
	case "Stop":
		return EventStop, true
	case "SessionEnd":
		return EventSessionEnd, true
	default:
		return "", false
	}
}
func normalizeClaude(raw []byte, at time.Time, eventID string) (AgentEvent, bool, error) {
	var p claudePayload
	if err := decodeOne(raw, &p); err != nil {
		return AgentEvent{}, false, err
	}
	if strings.TrimSpace(p.SessionID) == "" || strings.TrimSpace(p.HookEventName) == "" {
		return AgentEvent{}, false, fmt.Errorf("missing required claude fields")
	}
	if p.AgentID != "" {
		return AgentEvent{}, false, nil
	}
	et, supported := mapClaudeEvent(p.HookEventName)
	if !supported {
		return AgentEvent{}, false, nil
	}
	turn := p.PromptID
	m := Metadata{}
	if et == EventUserPromptSubmit && turn == "" {
		turn = "synthetic:" + eventID
		m.SyntheticTurnIdentity = true
	}
	if p.ToolName != "" {
		m.ToolName = ptrString(p.ToolName)
	}
	if et == EventPreToolUse && p.ToolName == "AskUserQuestion" {
		et = EventAskUserQuestion
	}
	if p.NotificationType != "" {
		m.NotificationType = ptrString(p.NotificationType)
	}
	if p.Error != "" {
		m.ErrorType = ptrString(p.Error)
	} else if p.ErrorType != "" {
		m.ErrorType = ptrString(p.ErrorType)
	}
	if et == EventStop {
		if p.BackgroundTasks != nil {
			m.BackgroundTaskCount = ptrInt(int(*p.BackgroundTasks))
		}
		if p.SessionCrons != nil {
			m.SessionCronCount = ptrInt(int(*p.SessionCrons))
		}
	}
	e := AgentEvent{SchemaVersion: 1, EventID: eventID, Provider: ProviderClaude, SessionID: p.SessionID, TurnID: ptrString(turn), EventType: et, OccurredAt: at, Cwd: ptrString(p.Cwd), Metadata: m}
	return e, true, nil
}
func mapClaudeEvent(s string) (EventType, bool) {
	switch s {
	case "UserPromptSubmit":
		return EventUserPromptSubmit, true
	case "PreToolUse":
		return EventPreToolUse, true
	case "PermissionRequest":
		return EventPermissionRequest, true
	case "PostToolUse":
		return EventPostToolUse, true
	case "PostToolUseFailure":
		return EventPostToolUseFailure, true
	case "PermissionDenied":
		return EventPermissionDenied, true
	case "Notification":
		return EventNotification, true
	case "Stop":
		return EventStop, true
	case "StopFailure":
		return EventStopFailure, true
	case "SessionEnd":
		return EventSessionEnd, true
	case "Elicitation":
		return EventElicitation, true
	case "ElicitationResult":
		return EventElicitationResult, true
	default:
		return "", false
	}
}
func decodeOne(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("multiple json values")
	}
	return nil
}
