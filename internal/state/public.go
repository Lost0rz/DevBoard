package state

import "time"

type PublicState struct {
	SchemaVersion     int                           `json:"schemaVersion"`
	StateKind         string                        `json:"stateKind"`
	GeneratedAt       time.Time                     `json:"generatedAt"`
	Host              PublicHost                    `json:"host"`
	Agents            []PublicAgent                 `json:"agents"`
	Tasks             []PublicTask                  `json:"tasks"`
	Alerts            []PublicAlert                 `json:"alerts"`
	System            PublicSystem                  `json:"system"`
	Network           PublicNetwork                 `json:"network"`
	Projects          []PublicProject               `json:"projects"`
	Quota             []PublicQuota                 `json:"quota"`
	Sources           map[string]PublicSourceHealth `json:"sources"`
	NavigationTargets []PublicNavigationTarget      `json:"navigationTargets"`
	Meta              DisplayMeta                   `json:"meta"`
}
type PublicHost struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}
type PublicAgent struct {
	ID          string                  `json:"id"`
	Provider    string                  `json:"provider"`
	SessionID   string                  `json:"sessionId"`
	CurrentTurn PublicCurrentTurn       `json:"currentTurn"`
	Navigation  *PublicNavigationTarget `json:"navigation,omitempty"`
}
type PublicCurrentTurn struct {
	TurnID      string     `json:"turnId"`
	Activity    Activity   `json:"activity"`
	Outcome     Outcome    `json:"outcome"`
	Freshness   Freshness  `json:"freshness"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}
type PublicTask struct {
	ID         string                `json:"id"`
	Provider   string                `json:"provider"`
	Project    *PublicTaskProject    `json:"project,omitempty"`
	Title      string                `json:"title"`
	Lifecycle  TaskLifecycle         `json:"lifecycle"`
	Freshness  Freshness             `json:"freshness"`
	Confidence TaskConfidence        `json:"confidence"`
	StartedAt  time.Time             `json:"startedAt"`
	UpdatedAt  time.Time             `json:"updatedAt"`
	Checkpoint *PublicTaskCheckpoint `json:"checkpoint,omitempty"`
	Attention  *PublicTaskAttention  `json:"attention,omitempty"`
	Completion *PublicTaskCompletion `json:"completion,omitempty"`
	// Unread is a derived, privacy-safe delivery flag. It is true only for a
	// terminal task whose provider-side read acknowledgement has not arrived.
	Unread bool `json:"unread,omitempty"`
	// SupersededAt mirrors TaskState.SupersededAt: a recovered error that no
	// longer needs user action. It is a derived sanitized field allowed by the
	// observability contract §15 amendment (2026-08-25).
	SupersededAt *time.Time `json:"supersededAt,omitempty"`
}
type PublicTaskProject struct {
	ProjectName   string `json:"projectName"`
	WorktreeLabel string `json:"worktreeLabel,omitempty"`
	Branch        string `json:"branch,omitempty"`
}
type PublicTaskCheckpoint struct {
	Kind TaskCheckpointKind `json:"kind"`
	Text string             `json:"text,omitempty"`
	At   time.Time          `json:"at"`
}
type PublicTaskAttention struct {
	Kind TaskAttentionKind `json:"kind"`
	Text string            `json:"text"`
	At   time.Time         `json:"at"`
}
type PublicTaskCompletion struct {
	Summary          *string   `json:"summary,omitempty"`
	ResultIdentifier *string   `json:"resultIdentifier,omitempty"`
	At               time.Time `json:"at"`
}
type PublicAlert struct {
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
type PublicSystem struct {
	CPUPercent    *float64             `json:"cpuPercent"`
	Memory        PublicMetricSet      `json:"memory"`
	Swap          PublicMetricSet      `json:"swap"`
	Disk          PublicMetricSet      `json:"disk"`
	ProcessGroups []PublicProcessGroup `json:"processGroups"`
}
type PublicMetricSet struct {
	UsedBytes   *uint64  `json:"usedBytes"`
	TotalBytes  *uint64  `json:"totalBytes"`
	PercentUsed *float64 `json:"percentUsed"`
}
type PublicProcessGroup struct {
	Name                string   `json:"name"`
	MatchedPIDCount     int      `json:"matchedPidCount"`
	ResidentMemoryBytes *uint64  `json:"residentMemoryBytes"`
	CPUPercent          *float64 `json:"cpuPercent"`
}
type PublicNetwork struct {
	Quality               NetworkQuality `json:"quality"`
	Reachable             *bool          `json:"reachable"`
	ConnectLatencyMs      *float64       `json:"connectLatencyMs"`
	ProbeFailurePercent   *float64       `json:"probeFailurePercent"`
	ReceiveBytesPerSecond *float64       `json:"receiveBytesPerSecond"`
	SendBytesPerSecond    *float64       `json:"sendBytesPerSecond"`
}
type PublicProject struct {
	ProjectID          string                  `json:"projectId"`
	DisplayName        string                  `json:"displayName"`
	WorktreeID         string                  `json:"worktreeId"`
	RepositoryIdentity string                  `json:"repositoryIdentity"`
	Branch             string                  `json:"branch"`
	Dirty              bool                    `json:"dirty"`
	ModifiedCount      int                     `json:"modifiedCount"`
	UntrackedCount     int                     `json:"untrackedCount"`
	Ahead              int                     `json:"ahead"`
	Behind             int                     `json:"behind"`
	Navigation         *PublicNavigationTarget `json:"navigation,omitempty"`
}
type PublicQuota struct {
	Provider     string               `json:"provider"`
	AccountKey   string               `json:"accountKey,omitempty"`
	DisplayLabel string               `json:"displayLabel,omitempty"`
	Windows      *[]PublicQuotaWindow `json:"windows"`
	SampledAt    *time.Time           `json:"sampledAt,omitempty"`
	SourceStatus SourceStatus         `json:"sourceStatus"`
	ObservedBy   string               `json:"observedBy,omitempty"`
}
type PublicQuotaWindow struct {
	Name        string     `json:"name"`
	UsedPercent *float64   `json:"usedPercent"`
	ResetsAt    *time.Time `json:"resetsAt"`
}
type PublicSourceHealth struct {
	Status        SourceStatus `json:"status"`
	LastAttemptAt *time.Time   `json:"lastAttemptAt"`
	LastSuccessAt *time.Time   `json:"lastSuccessAt"`
	Message       string       `json:"message"`
	// Reason mirrors SourceHealth.Reason: a fixed-vocabulary machine slug
	// (never operator/provider text) that lets menu-bar surfaces distinguish
	// for example a missing CodexBar CLI from a generic quota failure.
	Reason string `json:"reason,omitempty"`
}
type PublicNavigationTarget struct {
	TargetID       string             `json:"targetId"`
	Kind           NavigationKind     `json:"kind"`
	AllowedActions []NavigationAction `json:"allowedActions"`
}
type DisplayMeta struct {
	DisplayContractVersion        int    `json:"displayContractVersion"`
	KindleRefreshSeconds          int    `json:"kindleRefreshSeconds"`
	CompleteHighVisibilitySeconds int    `json:"completeHighVisibilitySeconds"`
	CompleteRetentionSeconds      int    `json:"completeRetentionSeconds"`
	SafeNavigationEnabled         bool   `json:"safeNavigationEnabled"`
	WakeLockMode                  string `json:"wakeLockMode"`
}
