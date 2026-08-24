package web

import (
	"strings"
	"sync"
	"time"

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
}

type diagnosticKey struct {
	component string
	event     string
}

var diagnosticCatalog = map[diagnosticKey]string{
	{component: "admin", event: "login_rejected"}:    "Admin login rejected",
	{component: "admin", event: "login_accepted"}:    "Admin login accepted",
	{component: "admin", event: "registry_saved"}:    "Node registry saved",
	{component: "admin", event: "registry_rejected"}: "Node registry change rejected",
	{component: "admin", event: "settings_saved"}:    "Operator settings saved",
	{component: "admin", event: "settings_rejected"}: "Operator settings change rejected",
	{component: "hub", event: "snapshot_accepted"}:   "Node snapshot accepted",
	{component: "hub", event: "snapshot_rejected"}:   "Node snapshot rejected",
	{component: "hub", event: "runtime_started"}:     "Hub runtime started",
	{component: "hub", event: "runtime_unavailable"}: "Hub runtime unavailable",
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
	}
	r.entries = append(r.entries, entry)
	if len(r.entries) > r.capacity {
		r.entries = append([]Diagnostic(nil), r.entries[len(r.entries)-r.capacity:]...)
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

func diagnosticComponents() []string { return []string{"admin", "hub"} }

func diagnosticEventAllowed(component, event string) bool {
	_, ok := diagnosticCatalog[diagnosticKey{component: component, event: event}]
	return ok
}
