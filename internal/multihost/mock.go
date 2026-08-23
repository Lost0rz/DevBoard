package multihost

import (
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func MockDashboard(local state.PublicState, now time.Time) DashboardState {
	now = now.UTC()
	localSynthetic, err := clonePublicState(local)
	if err != nil {
		return NewPeerSnapshotStore(nil).Dashboard(local, now)
	}
	completionSummary := "Synthetic validation completed successfully."
	localSynthetic.Tasks = []state.PublicTask{
		{
			ID: "mock-local-working", Provider: "codex", Title: "Implement local dashboard work", Lifecycle: state.TaskWorking, Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh,
			StartedAt: now.Add(-5 * time.Minute), UpdatedAt: now.Add(-10 * time.Second),
			Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Editing implementation", At: now.Add(-30 * time.Second)},
		},
		{
			ID: "mock-local-complete", Provider: "claude-code", Title: "Validate local changes", Lifecycle: state.TaskComplete, Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh,
			StartedAt: now.Add(-16 * time.Minute), UpdatedAt: now.Add(-5 * time.Minute),
			Completion: &state.PublicTaskCompletion{Summary: &completionSummary, At: now.Add(-5 * time.Minute)},
		},
	}

	remoteID := "mock-remote"
	if localSynthetic.Host.ID == remoteID {
		remoteID = "mock-remote-2"
	}
	remote, err := clonePublicState(localSynthetic)
	if err != nil {
		return NewPeerSnapshotStore(nil).Dashboard(localSynthetic, now)
	}
	remote.Host = state.PublicHost{ID: remoteID, DisplayName: "Mock MacBook"}
	remote.GeneratedAt = now.Add(-45 * time.Second)
	cpu := 61.0
	remote.System.CPUPercent = &cpu
	remote.System.ProcessGroups = nil
	reachable := true
	latency := 128.0
	failure := 4.0
	receive := 480.0 * 1024
	send := 96.0 * 1024
	remote.Network = state.PublicNetwork{
		Quality:               state.NetworkDegraded,
		Reachable:             &reachable,
		ConnectLatencyMs:      &latency,
		ProbeFailurePercent:   &failure,
		ReceiveBytesPerSecond: &receive,
		SendBytesPerSecond:    &send,
	}
	remote.Tasks = []state.PublicTask{{
		ID: "mock-remote-attention", Provider: "claude-code", Title: "Review remote approval", Lifecycle: state.TaskLifecycleAttention, Freshness: state.FreshnessStale, Confidence: state.TaskConfidenceHigh,
		StartedAt: now.Add(-7 * time.Minute), UpdatedAt: now.Add(-45 * time.Second),
		Attention: &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Remote task needs approval on the source Mac.", At: now.Add(-45 * time.Second)},
	}}
	remote.NavigationTargets = nil
	remote.Meta.SafeNavigationEnabled = false
	peerStore := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: remoteID, Endpoint: "192.168.255.254:8787"}})
	_ = peerStore.MarkSuccess(remoteID, remote, now, PeerDegraded, "Peer snapshot is stale.")
	return peerStore.Dashboard(localSynthetic, now)
}

// MockHubDashboard is deterministic and peer-only. The NAS itself never
// appears as a monitored host and no outbound polling is required.
func MockHubDashboard(now time.Time) DashboardState {
	now = now.UTC()
	peers := []config.PeerConfig{
		{ExpectedHostID: "mock-mac-a", Endpoint: "192.168.255.253:8787"},
		{ExpectedHostID: "mock-mac-b", Endpoint: "192.168.255.254:8787"},
	}
	store := NewPeerSnapshotStore(peers)

	macA := mockHubPublicState(now, "mock-mac-a", "Mock Mac A")
	macA.Tasks = []state.PublicTask{{
		ID: "mock-a-working", Provider: "codex", Title: "Implement node work", Lifecycle: state.TaskWorking, Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh,
		StartedAt: now.Add(-3 * time.Minute), UpdatedAt: now.Add(-5 * time.Second),
		Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Editing implementation", At: now.Add(-20 * time.Second)},
	}}
	_ = store.MarkSuccess("mock-mac-a", macA, now, PeerAvailable, "Peer snapshot available.")

	macB := mockHubPublicState(now.Add(-45*time.Second), "mock-mac-b", "Mock Mac B")
	macB.Network.Quality = state.NetworkDegraded
	macB.Tasks = []state.PublicTask{{
		ID: "mock-b-attention", Provider: "claude-code", Title: "Review remote approval", Lifecycle: state.TaskLifecycleAttention, Freshness: state.FreshnessStale, Confidence: state.TaskConfidenceHigh,
		StartedAt: now.Add(-7 * time.Minute), UpdatedAt: now.Add(-45 * time.Second),
		Attention: &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Remote task needs approval on the source Mac.", At: now.Add(-45 * time.Second)},
	}}
	_ = store.MarkSuccess("mock-mac-b", macB, now, PeerDegraded, "Peer snapshot is stale.")
	return store.DashboardPeers(now)
}

func mockHubPublicState(generatedAt time.Time, id, displayName string) state.PublicState {
	cpu := 28.0
	memUsed := uint64(8 * 1024 * 1024 * 1024)
	memTotal := uint64(16 * 1024 * 1024 * 1024)
	memPct := 50.0
	diskUsed := uint64(100 * 1024 * 1024 * 1024)
	diskTotal := uint64(500 * 1024 * 1024 * 1024)
	diskPct := 20.0
	reachable := true
	latency := 24.0
	failure := 0.0
	receive := 256.0 * 1024
	send := 64.0 * 1024
	return state.PublicState{
		SchemaVersion: 1,
		StateKind:     "public",
		GeneratedAt:   generatedAt.UTC(),
		Host:          state.PublicHost{ID: id, DisplayName: displayName},
		System: state.PublicSystem{
			CPUPercent: &cpu,
			Memory:     state.PublicMetricSet{UsedBytes: &memUsed, TotalBytes: &memTotal, PercentUsed: &memPct},
			Disk:       state.PublicMetricSet{UsedBytes: &diskUsed, TotalBytes: &diskTotal, PercentUsed: &diskPct},
		},
		Network: state.PublicNetwork{
			Quality:               state.NetworkGood,
			Reachable:             &reachable,
			ConnectLatencyMs:      &latency,
			ProbeFailurePercent:   &failure,
			ReceiveBytesPerSecond: &receive,
			SendBytesPerSecond:    &send,
		},
		Sources: map[string]state.PublicSourceHealth{
			"network": {Status: state.SourceAvailable},
			"system":  {Status: state.SourceAvailable},
		},
		Meta: state.DisplayMeta{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800, SafeNavigationEnabled: false},
	}
}
