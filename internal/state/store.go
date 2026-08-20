package state

import "sync"

type Store struct {
	mu    sync.RWMutex
	state InternalRootState
}

func NewStore(initial InternalRootState) *Store {
	return &Store{state: CloneInternalRootState(initial)}
}

func (s *Store) Snapshot() InternalRootState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CloneInternalRootState(s.state)
}

func (s *Store) Replace(next InternalRootState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = CloneInternalRootState(next)
}

func CloneInternalRootState(in InternalRootState) InternalRootState {
	out := in
	out.Agents = append([]AgentState(nil), in.Agents...)
	for i := range out.Agents {
		out.Agents[i].CurrentTurn.CompletedAt = cloneTime(out.Agents[i].CurrentTurn.CompletedAt)
	}
	out.Alerts = append([]AlertState(nil), in.Alerts...)
	for i := range out.Alerts {
		out.Alerts[i].TurnID = cloneString(out.Alerts[i].TurnID)
		out.Alerts[i].HighVisibilityUntil = cloneTime(out.Alerts[i].HighVisibilityUntil)
		out.Alerts[i].RetainUntil = cloneTime(out.Alerts[i].RetainUntil)
	}
	out.System.CPUPercent = cloneFloat64(in.System.CPUPercent)
	out.System.Memory = cloneMetric(in.System.Memory)
	out.System.Swap = cloneMetric(in.System.Swap)
	out.System.Disk = cloneMetric(in.System.Disk)
	out.System.ProcessGroups = append([]ProcessGroup(nil), in.System.ProcessGroups...)
	for i := range out.System.ProcessGroups {
		out.System.ProcessGroups[i].ResidentMemoryBytes = cloneUint64(out.System.ProcessGroups[i].ResidentMemoryBytes)
		out.System.ProcessGroups[i].CPUPercent = cloneFloat64(out.System.ProcessGroups[i].CPUPercent)
	}
	out.Projects = append([]ProjectState(nil), in.Projects...)
	out.Quota = append([]QuotaState(nil), in.Quota...)
	for i := range out.Quota {
		out.Quota[i].Windows = cloneQuotaWindows(out.Quota[i].Windows)
	}
	out.Sources = make(map[string]SourceHealth, len(in.Sources))
	for k, v := range in.Sources {
		v.LastAttemptAt = cloneTime(v.LastAttemptAt)
		v.LastSuccessAt = cloneTime(v.LastSuccessAt)
		out.Sources[k] = v
	}
	out.NavigationTargets = append([]NavigationTarget(nil), in.NavigationTargets...)
	for i := range out.NavigationTargets {
		out.NavigationTargets[i].AllowedActions = append([]NavigationAction(nil), in.NavigationTargets[i].AllowedActions...)
	}
	return out
}

func cloneMetric(in MetricSet) MetricSet {
	return MetricSet{UsedBytes: cloneUint64(in.UsedBytes), TotalBytes: cloneUint64(in.TotalBytes), PercentUsed: cloneFloat64(in.PercentUsed)}
}
