package state

import "time"

type Activity string
type Outcome string
type Freshness string
type SourceStatus string
type NavigationKind string
type NavigationAction string
type AlertType string
type NetworkQuality string

type TaskLifecycle string
type TaskConfidence string
type TaskCheckpointKind string
type TaskAttentionKind string

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
	NetworkUnknown  NetworkQuality = "unknown"
	NetworkGood     NetworkQuality = "good"
	NetworkDegraded NetworkQuality = "degraded"
	NetworkOffline  NetworkQuality = "offline"
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
const (
	TaskWorking            TaskLifecycle = "working"
	TaskLifecycleAttention TaskLifecycle = "attention"
	TaskError              TaskLifecycle = "error"
	TaskComplete           TaskLifecycle = "complete"
)
const (
	TaskConfidenceHigh     TaskConfidence = "high"
	TaskConfidenceDegraded TaskConfidence = "degraded"
)
const (
	CheckpointStarted          TaskCheckpointKind = "started"
	CheckpointInspecting       TaskCheckpointKind = "inspecting"
	CheckpointEditing          TaskCheckpointKind = "editing"
	CheckpointRunning          TaskCheckpointKind = "running"
	CheckpointValidating       TaskCheckpointKind = "validating"
	CheckpointDelegated        TaskCheckpointKind = "delegated"
	CheckpointSubtaskCompleted TaskCheckpointKind = "subtask_completed"
	CheckpointBackgroundWait   TaskCheckpointKind = "background_wait"
)
const (
	AttentionApprovalNeeded         TaskAttentionKind = "approval_needed"
	AttentionQuestionWaiting        TaskAttentionKind = "question_waiting"
	AttentionElicitationWaiting     TaskAttentionKind = "elicitation_waiting"
	AttentionAuthenticationRequired TaskAttentionKind = "authentication_required"
	AttentionBillingRequired        TaskAttentionKind = "billing_required"
	AttentionRateLimited            TaskAttentionKind = "rate_limited"
	AttentionProviderActionRequired TaskAttentionKind = "provider_action_required"
)

type InternalRootState struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	StateKind         string                  `json:"stateKind"`
	GeneratedAt       time.Time               `json:"generatedAt"`
	Host              HostState               `json:"host"`
	Agents            []AgentState            `json:"agents"`
	Tasks             []TaskState             `json:"tasks"`
	Alerts            []AlertState            `json:"alerts"`
	System            SystemState             `json:"system"`
	Network           NetworkState            `json:"network"`
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

type TaskState struct {
	ID         string              `json:"id"`
	Provider   string              `json:"provider"`
	SessionID  string              `json:"sessionId"`
	TurnID     string              `json:"turnId"`
	Project    *TaskProjectContext `json:"project,omitempty"`
	Title      string              `json:"title"`
	Lifecycle  TaskLifecycle       `json:"lifecycle"`
	Freshness  Freshness           `json:"freshness"`
	Confidence TaskConfidence      `json:"confidence"`
	StartedAt  time.Time           `json:"startedAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	Checkpoint *TaskCheckpoint     `json:"checkpoint,omitempty"`
	Attention  *TaskAttention      `json:"attention,omitempty"`
	Completion *TaskCompletion     `json:"completion,omitempty"`
}

type TaskProjectContext struct {
	ProjectName      string `json:"projectName"`
	WorktreeLabel    string `json:"worktreeLabel,omitempty"`
	Branch           string `json:"branch,omitempty"`
	WorktreeIdentity string `json:"worktreeIdentity"`
}
type TaskCheckpoint struct {
	Kind TaskCheckpointKind `json:"kind"`
	Text string             `json:"text,omitempty"`
	At   time.Time          `json:"at"`
}
type TaskAttention struct {
	Kind          TaskAttentionKind `json:"kind"`
	Text          string            `json:"text"`
	At            time.Time         `json:"at"`
	CorrelationID string            `json:"correlationId,omitempty"`
}
type TaskCompletion struct {
	Summary          *string   `json:"summary,omitempty"`
	ResultIdentifier *string   `json:"resultIdentifier,omitempty"`
	At               time.Time `json:"at"`
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
type NetworkState struct {
	Quality               NetworkQuality `json:"quality"`
	Reachable             *bool          `json:"reachable"`
	ConnectLatencyMs      *float64       `json:"connectLatencyMs"`
	ProbeFailurePercent   *float64       `json:"probeFailurePercent"`
	ReceiveBytesPerSecond *float64       `json:"receiveBytesPerSecond"`
	SendBytesPerSecond    *float64       `json:"sendBytesPerSecond"`
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
	Provider     string         `json:"provider"`
	AccountKey   string         `json:"accountKey,omitempty"`
	DisplayLabel string         `json:"displayLabel,omitempty"`
	Windows      *[]QuotaWindow `json:"windows"`
	SampledAt    *time.Time     `json:"sampledAt,omitempty"`
	SourceID     string         `json:"sourceId"`
	ObservedBy   string         `json:"observedBy,omitempty"`
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
