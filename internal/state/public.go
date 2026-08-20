package state

import "time"

type PublicState struct {
	SchemaVersion     int                           `json:"schemaVersion"`
	StateKind         string                        `json:"stateKind"`
	GeneratedAt       time.Time                     `json:"generatedAt"`
	Host              PublicHost                    `json:"host"`
	Agents            []PublicAgent                 `json:"agents"`
	Alerts            []PublicAlert                 `json:"alerts"`
	System            PublicSystem                  `json:"system"`
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
	Provider     string         `json:"provider"`
	Windows      *[]QuotaWindow `json:"windows"`
	SourceStatus SourceStatus   `json:"sourceStatus"`
}

type PublicSourceHealth struct {
	Status        SourceStatus `json:"status"`
	LastAttemptAt *time.Time   `json:"lastAttemptAt"`
	LastSuccessAt *time.Time   `json:"lastSuccessAt"`
	Message       string       `json:"message"`
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
