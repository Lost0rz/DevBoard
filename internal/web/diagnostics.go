package web

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agentquota"
	"github.com/Lost0rz/DevBoard/internal/diagnostics"
)

// Diagnostic is the bounded, redacted application diagnostic projection used
// by the Operator Console. It intentionally has no free-form attributes,
// paths, request bodies or credentials.
type Diagnostic struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Component string `json:"component"`
	EventCode string `json:"eventCode"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
}

type diagnosticKey struct {
	component string
	event     string
}

var diagnosticCatalog = map[diagnosticKey]string{
	{component: "admin", event: "login_rejected"}:                   "Admin login rejected",
	{component: "admin", event: "login_accepted"}:                   "Admin login accepted",
	{component: "admin", event: "registry_saved"}:                   "Node registry saved",
	{component: "admin", event: "registry_rejected"}:                "Node registry change rejected",
	{component: "admin", event: "settings_saved"}:                   "Operator settings saved",
	{component: "admin", event: "settings_rejected"}:                "Operator settings change rejected",
	{component: "admin", event: "password_initialized"}:             "Admin password initialized",
	{component: "admin", event: "password_changed"}:                 "Admin password changed",
	{component: "admin", event: "password_change_rejected"}:         "Admin password change rejected",
	{component: "hub", event: "snapshot_accepted"}:                  "Node snapshot accepted",
	{component: "hub", event: "snapshot_rejected"}:                  "Node snapshot rejected",
	{component: "hub", event: "runtime_started"}:                    "Hub runtime started",
	{component: "hub", event: "runtime_unavailable"}:                "Hub runtime unavailable",
	{component: "web", event: "display_slow"}:                       "Display page slow",
	{component: "web", event: "display_fragment_slow"}:              "Display refresh slow",
	{component: "web", event: "dashboard_slow"}:                     "Dashboard API slow",
	{component: "web", event: "request_failed"}:                     "Web request failed",
	{component: "agent-quota", event: "activation_due"}:             "GLM activation schedule fired",
	{component: "agent-quota", event: "activation_attempt"}:         "GLM activation request sent",
	{component: "agent-quota", event: "activation_succeeded"}:       "GLM activation verified",
	{component: "agent-quota", event: "activation_failed"}:          "GLM activation failed",
	{component: "agent-quota", event: "activation_retry_scheduled"}: "GLM activation retry scheduled",
	{component: "agent-quota", event: "activation_skipped"}:         "GLM activation slot skipped",
	{component: "agent-quota", event: "activation_deferred"}:        "GLM activation deferred",
}

var diagnosticLevels = map[string]int{"info": 0, "warn": 1, "error": 2}

// DiagnosticsRing is an in-process bounded ring. Only catalogued events can
// enter it; callers never provide a message or arbitrary attributes.
type DiagnosticsRing struct {
	mu       sync.RWMutex
	capacity int
	minLevel string
	entries  []Diagnostic
	now      func() time.Time
}

var _ diagnostics.Recorder = (*DiagnosticsRing)(nil)

func NewDiagnosticsRing(capacity int, minLevel string) *DiagnosticsRing {
	if capacity < 1 {
		capacity = 200
	}
	if minLevel == "" {
		minLevel = "info"
	}
	return &DiagnosticsRing{capacity: capacity, minLevel: minLevel, now: time.Now}
}

func (r *DiagnosticsRing) Record(level, component, event string) {
	r.record(level, component, event, "")
}

// RecordDetail is used only by in-process producers that build the detail
// from fixed route names, numeric status codes and durations. It keeps the
// external diagnostics API useful while preserving the no-request-data rule.
func (r *DiagnosticsRing) RecordDetail(level, component, event, detail string) {
	r.record(level, component, event, detail)
}

// RecordAgentQuota projects scheduler events into the redacted diagnostics
// ring. Only bounded reason categories and numeric HTTP statuses are exposed.
func (r *DiagnosticsRing) RecordAgentQuota(event agentquota.Event) {
	level := "info"
	if event.Code == "activation_failed" {
		level = "error"
		if event.Reason == "api_key_unavailable" {
			level = "warn"
		}
	}
	detail := agentQuotaEventDetail(event)
	r.record(level, "agent-quota", event.Code, detail)
}

func (r *DiagnosticsRing) record(level, component, event, detail string) {
	message, ok := diagnosticCatalog[diagnosticKey{component: component, event: event}]
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	levelRank, levelOK := diagnosticLevels[level]
	minRank, minOK := diagnosticLevels[r.minLevel]
	if !levelOK || !minOK || levelRank < minRank {
		return
	}
	entry := Diagnostic{
		Timestamp: r.now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Component: component,
		EventCode: event,
		Message:   message,
		Detail:    detail,
	}
	r.entries = append(r.entries, entry)
	if len(r.entries) > r.capacity {
		r.entries = append([]Diagnostic(nil), r.entries[len(r.entries)-r.capacity:]...)
	}
}

func agentQuotaEventDetail(event agentquota.Event) string {
	prefix := ""
	if event.Trigger == "manual" {
		prefix = "Manual test: "
	}
	if event.Reason == "http_status" && event.HTTPStatus >= 100 && event.HTTPStatus <= 599 {
		return prefix + fmt.Sprintf("HTTP %d", event.HTTPStatus)
	}
	status := ""
	if event.HTTPStatus >= 100 && event.HTTPStatus <= 599 {
		status = fmt.Sprintf(" (HTTP %d)", event.HTTPStatus)
	}
	provider := ""
	if event.ProviderCode != "" {
		provider = " · provider code " + event.ProviderCode
	}
	reset := ""
	if !event.ResetAt.IsZero() {
		reset = " · reset " + event.ResetAt.Format("2006-01-02 15:04:05 MST")
	} else if event.ResetText != "" {
		reset = " · reset " + event.ResetText
	}
	switch event.Reason {
	case "scheduled_time":
		return prefix + "Scheduled time reached"
	case "http_request":
		return prefix + "Outbound request started"
	case "manual_test":
		return "Manual test: outbound request started"
	case "response_verified":
		return prefix + "Model response received and verified"
	case "manual_test_verified":
		return "Manual test: model response received and verified"
	case "api_key_unavailable":
		return prefix + "GLM API key unavailable"
	case "request_build":
		return prefix + "Request could not be built"
	case "endpoint_invalid":
		return prefix + "Endpoint is invalid"
	case "transport":
		return prefix + "Network or timeout failure"
	case "response_read":
		return prefix + "Response could not be read" + status
	case "response_unverified":
		return prefix + "Response did not contain a verifiable model result" + status
	case "provider_error":
		return prefix + "Provider returned an error payload" + status + provider + reset
	case "retryable_provider_result":
		return prefix + "Retry scheduled after provider response" + provider + reset
	case "missed_trigger_grace":
		return prefix + "Hub did not claim this time within the two-minute trigger window"
	case "cycle_busy":
		return prefix + "A previous activation cycle is still active"
	default:
		return "No additional detail"
	}
}

func (r *DiagnosticsRing) SetPolicy(capacity int, minLevel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if capacity > 0 {
		r.capacity = capacity
	}
	if minLevel != "" {
		r.minLevel = minLevel
	}
	if len(r.entries) > r.capacity {
		r.entries = append([]Diagnostic(nil), r.entries[len(r.entries)-r.capacity:]...)
	}
}

func (r *DiagnosticsRing) Capacity() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capacity
}

func (r *DiagnosticsRing) Query(level, component string, limit int) []Diagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > r.capacity {
		limit = r.capacity
	}
	wantLevel := strings.ToLower(strings.TrimSpace(level))
	wantComponent := strings.ToLower(strings.TrimSpace(component))
	filtered := make([]Diagnostic, 0, len(r.entries))
	for i := len(r.entries) - 1; i >= 0 && len(filtered) < limit; i-- {
		entry := r.entries[i]
		if wantLevel != "" && entry.Level != wantLevel {
			continue
		}
		if wantComponent != "" && entry.Component != wantComponent {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func diagnosticComponents() []string { return []string{"admin", "agent-quota", "hub", "web"} }

func diagnosticEventAllowed(component, event string) bool {
	_, ok := diagnosticCatalog[diagnosticKey{component: component, event: event}]
	return ok
}
