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
	EventStop               EventType = "Stop"
	EventStopFailure        EventType = "StopFailure"
	EventSessionEnd         EventType = "SessionEnd"
	EventElicitation        EventType = "Elicitation"
	EventElicitationResult  EventType = "ElicitationResult"
)

type Metadata struct {
	ToolName              *string `json:"toolName,omitempty"`
	NotificationType      *string `json:"notificationType,omitempty"`
	ErrorType             *string `json:"errorType,omitempty"`
	StopHookActive        *bool   `json:"stopHookActive,omitempty"`
	BackgroundTaskCount   *int    `json:"backgroundTaskCount,omitempty"`
	SessionCronCount      *int    `json:"sessionCronCount,omitempty"`
	SyntheticTurnIdentity bool    `json:"syntheticTurnIdentity,omitempty"`
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
	case EventUserPromptSubmit, EventPreToolUse, EventAskUserQuestion, EventPermissionRequest, EventPostToolUse, EventPostToolUseFailure, EventPermissionDenied, EventNotification, EventStop, EventStopFailure, EventSessionEnd, EventElicitation, EventElicitationResult:
	default:
		return fmt.Errorf("unsupported eventType %q", e.EventType)
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
