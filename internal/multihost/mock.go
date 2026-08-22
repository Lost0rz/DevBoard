package multihost

import (
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func MockDashboard(local state.PublicState, now time.Time) DashboardState {
	now = now.UTC()
	remoteID := "mock-remote"
	if local.Host.ID == remoteID {
		remoteID = "mock-remote-2"
	}
	remote, err := clonePublicState(local)
	if err != nil {
		return NewPeerSnapshotStore(nil).Dashboard(local, now)
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
	if len(remote.Tasks) == 0 {
		remote.Tasks = []state.PublicTask{{ID: "mock-remote-attention", Provider: "claude-code", Title: "Review remote approval", Lifecycle: state.TaskLifecycleAttention, Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-7 * time.Minute), UpdatedAt: now.Add(-45 * time.Second)}}
	}
	remote.Tasks[0].ID = "mock-remote-attention"
	remote.Tasks[0].Provider = "claude-code"
	remote.Tasks[0].Title = "Review remote approval"
	remote.Tasks[0].Lifecycle = state.TaskLifecycleAttention
	remote.Tasks[0].Freshness = state.FreshnessStale
	remote.Tasks[0].Attention = &state.PublicTaskAttention{Kind: state.AttentionApprovalNeeded, Text: "Remote task needs approval on the source Mac.", At: now.Add(-45 * time.Second)}
	remote.Tasks[0].Completion = nil
	remote.NavigationTargets = nil
	remote.Meta.SafeNavigationEnabled = false
	peerStore := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: remoteID, Endpoint: "192.168.255.254:8787"}})
	_ = peerStore.MarkSuccess(remoteID, remote, now, PeerDegraded, "Peer snapshot is stale.")
	return peerStore.Dashboard(local, now)
}
