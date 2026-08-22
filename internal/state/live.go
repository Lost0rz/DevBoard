package state

import "time"

func LiveInitialState(now time.Time, host HostState) InternalRootState {
	return InternalRootState{SchemaVersion: 1, StateKind: "internal", GeneratedAt: now, Host: host, Agents: []AgentState{}, Alerts: []AlertState{}, System: SystemState{Memory: MetricSet{}, Swap: MetricSet{}, Disk: MetricSet{}, ProcessGroups: []ProcessGroup{}}, Network: NetworkState{Quality: NetworkUnknown}, Projects: []ProjectState{}, Quota: []QuotaState{}, Sources: map[string]SourceHealth{"codex-hooks": {Status: SourceDegraded, Message: "No validated lifecycle event observed yet."}, "claude-hooks": {Status: SourceDegraded, Message: "No validated lifecycle event observed yet."}, "system": {Status: SourceUnavailable, Message: "System metrics not implemented in M2."}, "network": {Status: SourceUnavailable, Message: "Network health sample pending."}, "git": {Status: SourceUnavailable, Message: "Git collection not implemented in M2."}, "quota": {Status: SourceUnavailable, Message: "Quota collection not implemented in M2."}}, NavigationTargets: []NavigationTarget{}, InternalMeta: InternalMeta{SnapshotVersion: 1}}
}
