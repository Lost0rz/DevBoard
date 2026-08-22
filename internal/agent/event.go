package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Provider string
type EventType string

const (
	ProviderCodex  Provider = "codex"
	ProviderClaude Provider = "claude-code"
)
const (
	EventUserPromptSubmit   EventType = "UserPromptSubmit"
	EventPreToolUse         EventType = "PreToolUse"
	EventAskUserQuestion    EventType = "AskUserQuestion"
	EventPermissionRequest  EventType = "PermissionRequest"
	EventPostToolUse        EventType = "PostToolUse"
	EventPostToolUseFailure EventType = "PostToolUseFailure"
	EventPermissionDenied   EventType = "PermissionDenied"
	EventNotification       EventType = "Notification"
	EventSubagentStart      EventType = "SubagentStart"
	EventSubagentStop       EventType = "SubagentStop"
	EventTaskCreated        EventType = "TaskCreated"
	EventTaskCompleted      EventType = "TaskCompleted"
	EventStop               EventType = "Stop"
	EventStopFailure        EventType = "StopFailure"
	EventSessionEnd         EventType = "SessionEnd"
	EventElicitation        EventType = "Elicitation"
	EventElicitationResult  EventType = "ElicitationResult"
)

type Metadata struct {
	ToolName              *string `json:"toolName,omitempty"`
	CorrelationID         *string `json:"correlationId,omitempty"`
	NotificationType      *string `json:"notificationType,omitempty"`
	ErrorType             *string `json:"errorType,omitempty"`
	StopHookActive        *bool   `json:"stopHookActive,omitempty"`
	BackgroundTaskCount   *int    `json:"backgroundTaskCount,omitempty"`
	SessionCronCount      *int    `json:"sessionCronCount,omitempty"`
	SyntheticTurnIdentity bool    `json:"syntheticTurnIdentity,omitempty"`
	TaskTitle             *string `json:"taskTitle,omitempty"`
	ChildSubject          *string `json:"childSubject,omitempty"`
	CompletionSummary     *string `json:"completionSummary,omitempty"`
	ResultIdentifier      *string `json:"resultIdentifier,omitempty"`
}
type AgentEvent struct {
	SchemaVersion int       `json:"schemaVersion"`
	EventID       string    `json:"eventId"`
	Provider      Provider  `json:"provider"`
	SessionID     string    `json:"sessionId"`
	TurnID        *string   `json:"turnId"`
	EventType     EventType `json:"eventType"`
	OccurredAt    time.Time `json:"occurredAt"`
	Cwd           *string   `json:"cwd"`
	Metadata      Metadata  `json:"metadata"`
}

func NewEventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "evt-" + hex.EncodeToString(b), nil
}
func NewAlertID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "alert-" + hex.EncodeToString(b), nil
}
func (e AgentEvent) Validate() error {
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion %d", e.SchemaVersion)
	}
	if strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(e.SessionID) == "" {
		return errors.New("eventId and sessionId are required")
	}
	if e.Provider != ProviderCodex && e.Provider != ProviderClaude {
		return fmt.Errorf("unsupported provider %q", e.Provider)
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurredAt is required")
	}
	switch e.EventType {
	case EventUserPromptSubmit, EventPreToolUse, EventAskUserQuestion, EventPermissionRequest, EventPostToolUse, EventPostToolUseFailure, EventPermissionDenied, EventNotification, EventSubagentStart, EventSubagentStop, EventTaskCreated, EventTaskCompleted, EventStop, EventStopFailure, EventSessionEnd, EventElicitation, EventElicitationResult:
	default:
		return fmt.Errorf("unsupported eventType %q", e.EventType)
	}
	if e.Provider == ProviderCodex {
		switch e.EventType {
		case EventPostToolUseFailure, EventPermissionDenied, EventNotification, EventTaskCreated, EventTaskCompleted, EventStopFailure, EventElicitation, EventElicitationResult, EventAskUserQuestion:
			return fmt.Errorf("unsupported codex eventType %q", e.EventType)
		}
	}
	if e.Metadata.NotificationType != nil {
		if _, ok := safeNotificationType(*e.Metadata.NotificationType); !ok {
			return fmt.Errorf("unsupported notificationType %q", *e.Metadata.NotificationType)
		}
	}
	if e.Metadata.ErrorType != nil && safeErrorType(*e.Metadata.ErrorType) != *e.Metadata.ErrorType {
		return fmt.Errorf("unsupported errorType %q", *e.Metadata.ErrorType)
	}
	if e.Metadata.ToolName != nil && len(*e.Metadata.ToolName) > 96 {
		return errors.New("tool name too large")
	}
	if e.Metadata.CorrelationID != nil && len(*e.Metadata.CorrelationID) > 96 {
		return errors.New("correlation id too large")
	}
	if e.Metadata.TaskTitle != nil && len(*e.Metadata.TaskTitle) > maxTaskTitleBytes {
		return errors.New("task title too large")
	}
	if e.Metadata.ChildSubject != nil && len(*e.Metadata.ChildSubject) > maxTaskTitleBytes {
		return errors.New("child subject too large")
	}
	if e.Metadata.CompletionSummary != nil && len(*e.Metadata.CompletionSummary) > maxCompletionBytes {
		return errors.New("completion summary too large")
	}
	if e.Metadata.ResultIdentifier != nil && len(*e.Metadata.ResultIdentifier) > maxResultIdentifier {
		return errors.New("result identifier too large")
	}
	return nil
}
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	x := s
	return &x
}
func ptrBool(v bool) *bool { x := v; return &x }
func ptrInt(v int) *int    { x := v; return &x }
