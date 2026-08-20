package state

import "time"

type Activity string

type Outcome string

type Freshness string

type SourceStatus string

type NavigationKind string

type NavigationAction string

type AlertType string

const (
	ActivityIdle      Activity = "idle"
	ActivityWorking   Activity = "working"
	ActivityAttention Activity = "attention"
	ActivityError     Activity = "error"
)

const (
	OutcomeNone      Outcome = "none"
	OutcomeCompleted Outcome = "completed"
	OutcomeFailed    Outcome = "failed"
)

const (
	FreshnessFresh Freshness = "fresh"
	FreshnessStale Freshness = "stale"
)

const (
	SourceAvailable   SourceStatus = "available"
	SourceDegraded    SourceStatus = "degraded"
	SourceUnavailable SourceStatus = "unavailable"
)

const (
	NavigationAgent   NavigationKind = "agent"
	NavigationProject NavigationKind = "project"
	NavigationApp     NavigationKind = "app"
)

const (
	ActionFocusApp     NavigationAction = "focus_app"
	ActionFocusAgent   NavigationAction = "focus_agent"
	ActionFocusProject NavigationAction = "focus_project"
	ActionOpenProject  NavigationAction = "open_project"
)

const (
	AlertAttention AlertType = "attention"
	AlertError     AlertType = "error"
	AlertComplete  AlertType = "complete"
	AlertStale     AlertType = "stale"
)

type InternalRootState struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	StateKind         string                  `json:"stateKind"`
	GeneratedAt       time.Time               `json:"generatedAt"`
	Host              HostState               `json:"host"`
	Agents            []AgentState            `json:"agents"`
	Alerts            []AlertState            `json:"alerts"`
	System            SystemState             `json:"system"`
	Projects          []ProjectState          `json:"projects"`
	Quota             []QuotaState            `json:"quota"`
	Sources           map[string]SourceHealth `json:"sources"`
	NavigationTargets []NavigationTarget      `json:"navigationTargets"`
	InternalMeta      InternalMeta            `json:"internalMeta"`
}

type HostState struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type AgentState struct {
	ID                 string      `json:"id"`
	Provider           string      `json:"provider"`
	SessionID          string      `json:"sessionId"`
	CurrentTurn        CurrentTurn `json:"currentTurn"`
	NavigationTargetID string      `json:"navigationTargetId,omitempty"`
}

type CurrentTurn struct {
	TurnID      string     `json:"turnId"`
	Activity    Activity   `json:"activity"`
	Outcome     Outcome    `json:"outcome"`
	Freshness   Freshness  `json:"freshness"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type AlertState struct {
	AlertID             string     `json:"alertId"`
	Type                AlertType  `json:"type"`
	AgentID             string     `json:"agentId"`
	TurnID              *string    `json:"turnId"`
	Active              bool       `json:"active"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	HighVisibilityUntil *time.Time `json:"highVisibilityUntil,omitempty"`
	RetainUntil         *time.Time `json:"retainUntil,omitempty"`
}

type SystemState struct {
	CPUPercent    *float64       `json:"cpuPercent"`
	Memory        MetricSet      `json:"memory"`
	Swap          MetricSet      `json:"swap"`
	Disk          MetricSet      `json:"disk"`
	ProcessGroups []ProcessGroup `json:"processGroups"`
}

type MetricSet struct {
	UsedBytes   *uint64  `json:"usedBytes"`
	TotalBytes  *uint64  `json:"totalBytes"`
	PercentUsed *float64 `json:"percentUsed"`
}

type ProcessGroup struct {
	Name                string   `json:"name"`
	MatchedPIDCount     int      `json:"matchedPidCount"`
	ResidentMemoryBytes *uint64  `json:"residentMemoryBytes"`
	CPUPercent          *float64 `json:"cpuPercent"`
}

type ProjectState struct {
	ProjectID          string `json:"projectId"`
	DisplayName        string `json:"displayName"`
	WorktreeID         string `json:"worktreeId"`
	WorktreeRoot       string `json:"worktreeRoot"`
	RepositoryIdentity string `json:"repositoryIdentity"`
	Branch             string `json:"branch"`
	Dirty              bool   `json:"dirty"`
	ModifiedCount      int    `json:"modifiedCount"`
	UntrackedCount     int    `json:"untrackedCount"`
	Ahead              int    `json:"ahead"`
	Behind             int    `json:"behind"`
	NavigationTargetID string `json:"navigationTargetId,omitempty"`
}

type QuotaState struct {
	Provider string         `json:"provider"`
	Windows  *[]QuotaWindow `json:"windows"`
	SourceID string         `json:"sourceId"`
}

type QuotaWindow struct {
	Name        string     `json:"name"`
	UsedPercent *float64   `json:"usedPercent"`
	ResetsAt    *time.Time `json:"resetsAt"`
}

type SourceHealth struct {
	Status        SourceStatus `json:"status"`
	LastAttemptAt *time.Time   `json:"lastAttemptAt"`
	LastSuccessAt *time.Time   `json:"lastSuccessAt"`
	Message       string       `json:"message"`
}

type NavigationTarget struct {
	TargetID       string                 `json:"targetId"`
	Kind           NavigationKind         `json:"kind"`
	HostID         string                 `json:"hostId"`
	AllowedActions []NavigationAction     `json:"allowedActions"`
	Detail         NavigationTargetDetail `json:"detail"`
}

type NavigationTargetDetail struct {
	AgentID      string `json:"agentId,omitempty"`
	Provider     string `json:"provider,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	TurnID       string `json:"turnId,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	WorktreeID   string `json:"worktreeId,omitempty"`
	WorktreeRoot string `json:"worktreeRoot,omitempty"`
	PreferredApp string `json:"preferredApp,omitempty"`
	FocusLocator string `json:"focusLocator,omitempty"`
	AppRef       string `json:"appRef,omitempty"`
}

type InternalMeta struct {
	SnapshotVersion      int    `json:"snapshotVersion"`
	RestoredFromSnapshot bool   `json:"restoredFromSnapshot"`
	PrivateNote          string `json:"privateNote,omitempty"`
}
