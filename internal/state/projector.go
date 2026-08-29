package state

import "time"

type RuntimeCapabilities struct{ SafeNavigation bool }
type ProjectionConfig struct {
	KindleRefreshSeconds          int
	CompleteHighVisibilitySeconds int
	CompleteRetentionSeconds      int
	// Timezone is the IANA location used only when rendering human-readable
	// quota reset moments. Stored/provider timestamps remain absolute instants.
	Timezone string
	// Display route suffixes are carried through to the web server without
	// coupling state projection to the config package.
	PadPath         string
	KindleRightPath string
	KindleLeftPath  string
}

func ProjectPublic(in InternalRootState, caps RuntimeCapabilities, cfg ProjectionConfig, now time.Time) PublicState {
	targets := make([]PublicNavigationTarget, 0, len(in.NavigationTargets))
	targetByID := make(map[string]NavigationTarget, len(in.NavigationTargets))
	for _, target := range in.NavigationTargets {
		if !validTarget(target) || target.HostID != in.Host.ID {
			continue
		}
		if _, exists := targetByID[target.TargetID]; exists {
			continue
		}
		targetByID[target.TargetID] = target
		targets = append(targets, publicTarget(target))
	}
	agents := make([]PublicAgent, 0, len(in.Agents))
	for _, agent := range in.Agents {
		pub := PublicAgent{ID: agent.ID, Provider: agent.Provider, SessionID: agent.SessionID, CurrentTurn: PublicCurrentTurn{TurnID: agent.CurrentTurn.TurnID, Activity: agent.CurrentTurn.Activity, Outcome: agent.CurrentTurn.Outcome, Freshness: agent.CurrentTurn.Freshness, StartedAt: agent.CurrentTurn.StartedAt, CompletedAt: cloneTime(agent.CurrentTurn.CompletedAt), UpdatedAt: agent.CurrentTurn.UpdatedAt}}
		if target, ok := targetByID[agent.NavigationTargetID]; ok && agentTargetMatches(agent, target) {
			t := publicTarget(target)
			pub.Navigation = &t
		}
		agents = append(agents, pub)
	}
	tasks := make([]PublicTask, 0, len(in.Tasks))
	for _, task := range in.Tasks {
		pub := PublicTask{ID: task.ID, Provider: task.Provider, Title: task.Title, Lifecycle: task.Lifecycle, Freshness: task.Freshness, Confidence: task.Confidence, StartedAt: task.StartedAt, UpdatedAt: task.UpdatedAt, Unread: (task.Lifecycle == TaskComplete || task.Lifecycle == TaskError) && task.ReadAt == nil}
		if caps.SafeNavigation {
			for _, target := range targetByID {
				if taskTargetMatches(task, target) {
					t := publicTarget(target)
					pub.Navigation = &t
					break
				}
			}
		}
		if task.Project != nil {
			pub.Project = &PublicTaskProject{ProjectName: task.Project.ProjectName, WorktreeLabel: task.Project.WorktreeLabel, Branch: task.Project.Branch}
		}
		if task.Checkpoint != nil {
			pub.Checkpoint = &PublicTaskCheckpoint{Kind: task.Checkpoint.Kind, Text: task.Checkpoint.Text, At: task.Checkpoint.At}
		}
		if task.Attention != nil {
			pub.Attention = &PublicTaskAttention{Kind: task.Attention.Kind, Text: task.Attention.Text, At: task.Attention.At}
		}
		if task.Completion != nil {
			pub.Completion = &PublicTaskCompletion{Summary: cloneString(task.Completion.Summary), ResultIdentifier: cloneString(task.Completion.ResultIdentifier), At: task.Completion.At}
		}
		pub.SupersededAt = cloneTime(task.SupersededAt)
		tasks = append(tasks, pub)
	}
	projects := make([]PublicProject, 0, len(in.Projects))
	for _, project := range in.Projects {
		pub := PublicProject{ProjectID: project.ProjectID, DisplayName: project.DisplayName, WorktreeID: project.WorktreeID, RepositoryIdentity: project.RepositoryIdentity, Branch: project.Branch, Dirty: project.Dirty, ModifiedCount: project.ModifiedCount, UntrackedCount: project.UntrackedCount, Ahead: project.Ahead, Behind: project.Behind}
		if target, ok := targetByID[project.NavigationTargetID]; ok && projectTargetMatches(project, target) {
			t := publicTarget(target)
			pub.Navigation = &t
		}
		projects = append(projects, pub)
	}
	alerts := make([]PublicAlert, len(in.Alerts))
	for i, alert := range in.Alerts {
		alerts[i] = PublicAlert{AlertID: alert.AlertID, Type: alert.Type, AgentID: alert.AgentID, TurnID: cloneString(alert.TurnID), Active: alert.Active, CreatedAt: alert.CreatedAt, UpdatedAt: alert.UpdatedAt, HighVisibilityUntil: cloneTime(alert.HighVisibilityUntil), RetainUntil: cloneTime(alert.RetainUntil)}
	}
	processGroups := make([]PublicProcessGroup, len(in.System.ProcessGroups))
	for i, group := range in.System.ProcessGroups {
		processGroups[i] = PublicProcessGroup{Name: group.Name, MatchedPIDCount: group.MatchedPIDCount, ResidentMemoryBytes: cloneUint64(group.ResidentMemoryBytes), CPUPercent: cloneFloat64(group.CPUPercent)}
	}
	quota := make([]PublicQuota, len(in.Quota))
	for i, q := range in.Quota {
		status := SourceUnavailable
		if source, ok := in.Sources[q.SourceID]; ok {
			status = source.Status
		}
		quota[i] = PublicQuota{
			Provider: q.Provider, AccountKey: q.AccountKey, DisplayLabel: q.DisplayLabel,
			Windows: projectQuotaWindows(q.Windows), SampledAt: cloneTime(q.SampledAt),
			SourceStatus: status, ObservedBy: q.ObservedBy,
		}
	}
	sources := make(map[string]PublicSourceHealth, len(in.Sources))
	for id, source := range in.Sources {
		sources[id] = PublicSourceHealth{Status: source.Status, LastAttemptAt: cloneTime(source.LastAttemptAt), LastSuccessAt: cloneTime(source.LastSuccessAt), Message: publicSourceMessage(source.Status), Reason: publicSourceReason(source.Reason)}
	}
	return PublicState{SchemaVersion: in.SchemaVersion, StateKind: "public", GeneratedAt: now, Host: PublicHost{ID: in.Host.ID, DisplayName: in.Host.DisplayName}, Agents: agents, Tasks: tasks, Alerts: alerts, System: PublicSystem{CPUPercent: cloneFloat64(in.System.CPUPercent), Memory: publicMetric(in.System.Memory), Swap: publicMetric(in.System.Swap), Disk: publicMetric(in.System.Disk), ProcessGroups: processGroups}, Network: PublicNetwork{Quality: in.Network.Quality, Reachable: cloneBool(in.Network.Reachable), ConnectLatencyMs: cloneFloat64(in.Network.ConnectLatencyMs), ProbeFailurePercent: cloneFloat64(in.Network.ProbeFailurePercent), ReceiveBytesPerSecond: cloneFloat64(in.Network.ReceiveBytesPerSecond), SendBytesPerSecond: cloneFloat64(in.Network.SendBytesPerSecond)}, Projects: projects, Quota: quota, Sources: sources, NavigationTargets: targets, Meta: DisplayMeta{DisplayContractVersion: 1, KindleRefreshSeconds: cfg.KindleRefreshSeconds, CompleteHighVisibilitySeconds: cfg.CompleteHighVisibilitySeconds, CompleteRetentionSeconds: cfg.CompleteRetentionSeconds, SafeNavigationEnabled: caps.SafeNavigation, WakeLockMode: "best-effort"}}
}

// publicSourceMessage is the allow-listed public text for one source health
// entry, derived only from the status. The internal SourceHealth.Message is
// arbitrary provider/operator text — it may contain absolute filesystem
// paths or credentials — so it never crosses the public projection (M0 §18
// sanitized message; M5.2 §34 privacy boundary).
func publicSourceMessage(status SourceStatus) string {
	switch status {
	case SourceAvailable:
		return "Source available."
	case SourceDegraded:
		return "Source degraded."
	default:
		return "Source unavailable."
	}
}

// publicSourceReason allow-lists the machine-readable reason slugs that may
// cross the public projection. An unknown internal reason never leaks; the
// empty string means "no distinct reason".
func publicSourceReason(reason string) string {
	switch reason {
	case "cli_unavailable", "configuration_required", "command_failed":
		return reason
	default:
		return ""
	}
}

func validTarget(target NavigationTarget) bool {
	if target.TargetID == "" || target.HostID == "" || len(target.AllowedActions) == 0 {
		return false
	}
	for _, action := range target.AllowedActions {
		switch target.Kind {
		case NavigationAgent:
			if action != ActionFocusAgent {
				return false
			}
		case NavigationProject:
			if action != ActionFocusProject && action != ActionOpenProject {
				return false
			}
		case NavigationApp:
			if action != ActionFocusApp {
				return false
			}
		default:
			return false
		}
	}
	switch target.Kind {
	case NavigationAgent:
		return target.Detail.AgentID != "" && target.Detail.Provider != "" && target.Detail.SessionID != "" && target.Detail.AgentID == target.Detail.Provider+":"+target.Detail.SessionID
	case NavigationProject:
		return target.Detail.ProjectID != "" && target.Detail.WorktreeID != ""
	case NavigationApp:
		return target.Detail.AppRef != ""
	default:
		return false
	}
}
func agentTargetMatches(agent AgentState, target NavigationTarget) bool {
	if target.Kind != NavigationAgent || !containsAction(target.AllowedActions, ActionFocusAgent) {
		return false
	}
	if target.Detail.AgentID != agent.ID || target.Detail.Provider != agent.Provider || target.Detail.SessionID != agent.SessionID {
		return false
	}
	if agent.CurrentTurn.TurnID != "" && target.Detail.TurnID != "" && agent.CurrentTurn.TurnID != target.Detail.TurnID {
		return false
	}
	return true
}
func taskTargetMatches(task TaskState, target NavigationTarget) bool {
	if target.Kind != NavigationAgent || !containsAction(target.AllowedActions, ActionFocusAgent) {
		return false
	}
	if target.Detail.Provider != task.Provider || target.Detail.SessionID != task.SessionID {
		return false
	}
	return target.Detail.TurnID == "" || target.Detail.TurnID == task.TurnID
}
func projectTargetMatches(project ProjectState, target NavigationTarget) bool {
	if target.Kind != NavigationProject || (!containsAction(target.AllowedActions, ActionFocusProject) && !containsAction(target.AllowedActions, ActionOpenProject)) {
		return false
	}
	return target.Detail.ProjectID == project.ProjectID && target.Detail.WorktreeID == project.WorktreeID
}
func publicTarget(target NavigationTarget) PublicNavigationTarget {
	return PublicNavigationTarget{TargetID: target.TargetID, Kind: target.Kind, AllowedActions: append([]NavigationAction(nil), target.AllowedActions...)}
}
func containsAction(actions []NavigationAction, want NavigationAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}
func publicMetric(m MetricSet) PublicMetricSet {
	return PublicMetricSet{UsedBytes: cloneUint64(m.UsedBytes), TotalBytes: cloneUint64(m.TotalBytes), PercentUsed: cloneFloat64(m.PercentUsed)}
}
func projectQuotaWindows(in *[]QuotaWindow) *[]PublicQuotaWindow {
	if in == nil {
		return nil
	}
	out := make([]PublicQuotaWindow, len(*in))
	for i, window := range *in {
		out[i] = PublicQuotaWindow{Name: window.Name, UsedPercent: cloneFloat64(window.UsedPercent), ResetsAt: cloneTime(window.ResetsAt)}
	}
	return &out
}
func cloneQuotaWindows(in *[]QuotaWindow) *[]QuotaWindow {
	if in == nil {
		return nil
	}
	out := make([]QuotaWindow, len(*in))
	copy(out, *in)
	for i := range out {
		out[i].UsedPercent = cloneFloat64(out[i].UsedPercent)
		out[i].ResetsAt = cloneTime(out[i].ResetsAt)
	}
	return &out
}
func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func cloneUint64(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func cloneFloat64(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
