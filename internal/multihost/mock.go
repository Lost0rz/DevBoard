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
