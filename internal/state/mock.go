package state

import "time"

func MockInternalState(now time.Time, host HostState) InternalRootState {
	workingStarted := now.Add(-5 * time.Minute)
	attentionStarted := now.Add(-8 * time.Minute)
	completedStarted := now.Add(-16 * time.Minute)
	completedAt := now.Add(-5 * time.Minute)
	attentionTurn := "turn-mock-claude-attention"
	completeTurn := "turn-mock-codex-complete"

	cpu := 23.5
	memUsed := uint64(12 * 1024 * 1024 * 1024)
	memTotal := uint64(24 * 1024 * 1024 * 1024)
	memPct := 50.0
	diskUsed := uint64(200 * 1024 * 1024 * 1024)
	diskTotal := uint64(465 * 1024 * 1024 * 1024)
	diskPct := 43.0
	codexMem := uint64(2 * 1024 * 1024 * 1024)
	codexCPU := 18.4
	claudeMem := uint64(1536 * 1024 * 1024)
	claudeCPU := 12.1

	return InternalRootState{
		SchemaVersion: 1,
		StateKind:     "internal",
		GeneratedAt:   now,
		Host:          host,
		Agents: []AgentState{
			{
				ID: "codex:session-mock-001", Provider: "codex", SessionID: "session-mock-001", NavigationTargetID: "target-agent-codex-mock-001",
				CurrentTurn: CurrentTurn{TurnID: "turn-mock-codex-working", Activity: ActivityWorking, Outcome: OutcomeNone, Freshness: FreshnessFresh, StartedAt: workingStarted, UpdatedAt: now.Add(-10 * time.Second)},
			},
			{
				ID: "claude-code:session-mock-002", Provider: "claude-code", SessionID: "session-mock-002", NavigationTargetID: "target-agent-claude-mock-002",
				CurrentTurn: CurrentTurn{TurnID: attentionTurn, Activity: ActivityAttention, Outcome: OutcomeNone, Freshness: FreshnessFresh, StartedAt: attentionStarted, UpdatedAt: now.Add(-20 * time.Second)},
			},
			{
				ID: "codex:session-mock-003", Provider: "codex", SessionID: "session-mock-003", NavigationTargetID: "target-agent-codex-mock-003",
				CurrentTurn: CurrentTurn{TurnID: completeTurn, Activity: ActivityIdle, Outcome: OutcomeCompleted, Freshness: FreshnessFresh, StartedAt: completedStarted, CompletedAt: &completedAt, UpdatedAt: completedAt},
			},
		},
		Alerts: []AlertState{
			{AlertID: "alert-mock-attention", Type: AlertAttention, AgentID: "claude-code:session-mock-002", TurnID: &attentionTurn, Active: true, CreatedAt: now.Add(-20 * time.Second), UpdatedAt: now.Add(-20 * time.Second)},
			{AlertID: "alert-mock-complete", Type: AlertComplete, AgentID: "codex:session-mock-003", TurnID: &completeTurn, Active: true, CreatedAt: completedAt, UpdatedAt: completedAt},
		},
		System: SystemState{
			CPUPercent: &cpu,
			Memory:     MetricSet{UsedBytes: &memUsed, TotalBytes: &memTotal, PercentUsed: &memPct},
			Swap:       MetricSet{UsedBytes: nil, TotalBytes: nil, PercentUsed: nil},
			Disk:       MetricSet{UsedBytes: &diskUsed, TotalBytes: &diskTotal, PercentUsed: &diskPct},
			ProcessGroups: []ProcessGroup{
				{Name: "Codex", MatchedPIDCount: 2, ResidentMemoryBytes: &codexMem, CPUPercent: &codexCPU},
				{Name: "Claude", MatchedPIDCount: 1, ResidentMemoryBytes: &claudeMem, CPUPercent: &claudeCPU},
			},
		},
		Projects: []ProjectState{
			{ProjectID: "project-mock", DisplayName: "Synthetic Project", WorktreeID: "wt-mock-main", WorktreeRoot: "/synthetic/mock/worktree", RepositoryIdentity: "synthetic.example/project", Branch: "main", Dirty: true, ModifiedCount: 2, UntrackedCount: 1, Ahead: 1, NavigationTargetID: "target-project-mock-main"},
		},
		Quota: []QuotaState{{Provider: "codex", Windows: nil, SourceID: "quota"}},
		Sources: map[string]SourceHealth{
			"codex-hooks":  {Status: SourceAvailable, LastAttemptAt: timePtr(now.Add(-10 * time.Second)), LastSuccessAt: timePtr(now.Add(-10 * time.Second)), Message: "Synthetic lifecycle source available."},
			"claude-hooks": {Status: SourceAvailable, LastAttemptAt: timePtr(now.Add(-20 * time.Second)), LastSuccessAt: timePtr(now.Add(-20 * time.Second)), Message: "Synthetic lifecycle source available."},
			"system":       {Status: SourceAvailable, LastAttemptAt: timePtr(now.Add(-5 * time.Second)), LastSuccessAt: timePtr(now.Add(-5 * time.Second)), Message: "Synthetic system metrics available."},
			"git":          {Status: SourceAvailable, LastAttemptAt: timePtr(now.Add(-30 * time.Second)), LastSuccessAt: timePtr(now.Add(-30 * time.Second)), Message: "Synthetic project state available."},
			"quota":        {Status: SourceUnavailable, LastAttemptAt: timePtr(now.Add(-2 * time.Minute)), LastSuccessAt: nil, Message: "Optional quota adapter unavailable in M1."},
		},
		NavigationTargets: []NavigationTarget{
			{TargetID: "target-agent-codex-mock-001", Kind: NavigationAgent, HostID: host.ID, AllowedActions: []NavigationAction{ActionFocusAgent}, Detail: NavigationTargetDetail{AgentID: "codex:session-mock-001", Provider: "codex", SessionID: "session-mock-001", TurnID: "turn-mock-codex-working", ProjectID: "project-mock", WorktreeID: "wt-mock-main", PreferredApp: "synthetic-terminal", FocusLocator: "opaque:mock:codex-001"}},
			{TargetID: "target-agent-claude-mock-002", Kind: NavigationAgent, HostID: host.ID, AllowedActions: []NavigationAction{ActionFocusAgent}, Detail: NavigationTargetDetail{AgentID: "claude-code:session-mock-002", Provider: "claude-code", SessionID: "session-mock-002", TurnID: attentionTurn, ProjectID: "project-mock", WorktreeID: "wt-mock-main", PreferredApp: "synthetic-terminal", FocusLocator: "opaque:mock:claude-002"}},
			{TargetID: "target-agent-codex-mock-003", Kind: NavigationAgent, HostID: host.ID, AllowedActions: []NavigationAction{ActionFocusAgent}, Detail: NavigationTargetDetail{AgentID: "codex:session-mock-003", Provider: "codex", SessionID: "session-mock-003", TurnID: completeTurn, ProjectID: "project-mock", WorktreeID: "wt-mock-main", PreferredApp: "synthetic-terminal", FocusLocator: "opaque:mock:codex-003"}},
			{TargetID: "target-project-mock-main", Kind: NavigationProject, HostID: host.ID, AllowedActions: []NavigationAction{ActionFocusProject, ActionOpenProject}, Detail: NavigationTargetDetail{ProjectID: "project-mock", WorktreeID: "wt-mock-main", WorktreeRoot: "/synthetic/mock/worktree", PreferredApp: "synthetic-terminal", FocusLocator: "opaque:mock:project"}},
			{TargetID: "target-app-terminal", Kind: NavigationApp, HostID: host.ID, AllowedActions: []NavigationAction{ActionFocusApp}, Detail: NavigationTargetDetail{AppRef: "app-mock-terminal", FocusLocator: "opaque:mock:app"}},
		},
		InternalMeta: InternalMeta{SnapshotVersion: 1, RestoredFromSnapshot: false},
	}
}

func timePtr(t time.Time) *time.Time { return &t }
