package state

import "sync"

type Store struct {
	mu       sync.RWMutex
	state    InternalRootState
	revision uint64
	wake     chan struct{}
}

func NewStore(initial InternalRootState) *Store {
	return &Store{state: CloneInternalRootState(initial), wake: make(chan struct{}, 1)}
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
	s.notifyChangedLocked()
}
func (s *Store) Update(fn func(*InternalRootState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := CloneInternalRootState(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	s.state = CloneInternalRootState(next)
	s.notifyChangedLocked()
	return nil
}

// UpdateIf applies a state mutation only when predicate says maintenance is
// needed. The predicate runs under the store lock and must be read-only. This
// prevents periodic maintenance checks from cloning the full state and
// waking every downstream consumer when nothing has changed.
func (s *Store) UpdateIf(predicate func(InternalRootState) bool, fn func(*InternalRootState) error) error {
	if predicate == nil {
		return s.Update(fn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !predicate(s.state) {
		return nil
	}
	next := CloneInternalRootState(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	s.state = CloneInternalRootState(next)
	s.notifyChangedLocked()
	return nil
}

// Revision returns the number of committed state changes. It exists so the
// M5.4 node uplink can observe store progress without polling producers.
func (s *Store) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Changes returns the coalescing state-change wake channel (M5.2 §22). Every
// committed Replace/Update refreshes the hint; the buffered slot holds at most
// one pending wake, so writers never block and bursts collapse into a single
// hint. It is a wake-up signal, not a lossless event queue: consumers always
// re-read the latest state after waking.
func (s *Store) Changes() <-chan struct{} {
	return s.wake
}

// notifyChangedLocked advances the revision and refreshes the coalescing wake
// hint. The channel send is non-blocking by construction.
func (s *Store) notifyChangedLocked() {
	s.revision++
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func CloneInternalRootState(in InternalRootState) InternalRootState {
	out := in
	out.Agents = append([]AgentState(nil), in.Agents...)
	for i := range out.Agents {
		out.Agents[i].CurrentTurn.CompletedAt = cloneTime(out.Agents[i].CurrentTurn.CompletedAt)
	}
	out.Tasks = append([]TaskState(nil), in.Tasks...)
	for i := range out.Tasks {
		out.Tasks[i].Project = cloneTaskProject(out.Tasks[i].Project)
		out.Tasks[i].Checkpoint = cloneTaskCheckpoint(out.Tasks[i].Checkpoint)
		out.Tasks[i].Attention = cloneTaskAttention(out.Tasks[i].Attention)
		out.Tasks[i].Completion = cloneTaskCompletion(out.Tasks[i].Completion)
		out.Tasks[i].ReadAt = cloneTime(out.Tasks[i].ReadAt)
		out.Tasks[i].SupersededAt = cloneTime(out.Tasks[i].SupersededAt)
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
	out.Network.Reachable = cloneBool(in.Network.Reachable)
	out.Network.ConnectLatencyMs = cloneFloat64(in.Network.ConnectLatencyMs)
	out.Network.ProbeFailurePercent = cloneFloat64(in.Network.ProbeFailurePercent)
	out.Network.ReceiveBytesPerSecond = cloneFloat64(in.Network.ReceiveBytesPerSecond)
	out.Network.SendBytesPerSecond = cloneFloat64(in.Network.SendBytesPerSecond)
	out.Projects = append([]ProjectState(nil), in.Projects...)
	out.Quota = append([]QuotaState(nil), in.Quota...)
	for i := range out.Quota {
		out.Quota[i].Windows = cloneQuotaWindows(out.Quota[i].Windows)
		out.Quota[i].SampledAt = cloneTime(in.Quota[i].SampledAt)
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
func cloneTaskProject(in *TaskProjectContext) *TaskProjectContext {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneTaskCheckpoint(in *TaskCheckpoint) *TaskCheckpoint {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneTaskAttention(in *TaskAttention) *TaskAttention {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func cloneTaskCompletion(in *TaskCompletion) *TaskCompletion {
	if in == nil {
		return nil
	}
	out := *in
	out.Summary = cloneString(in.Summary)
	out.ResultIdentifier = cloneString(in.ResultIdentifier)
	return &out
}
func cloneMetric(in MetricSet) MetricSet {
	return MetricSet{UsedBytes: cloneUint64(in.UsedBytes), TotalBytes: cloneUint64(in.TotalBytes), PercentUsed: cloneFloat64(in.PercentUsed)}
}
