package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/state"
)

type DesktopViewModel struct {
	ViewModel
	Network NetworkView
	Tasks   []TaskView
}

type NetworkView struct {
	Quality      string
	Reachable    string
	Latency      string
	Failure      string
	Receive      string
	Send         string
	SourceStatus string
}

type DashboardDesktopViewModel struct {
	Mock           bool
	Updated        string
	SingleHost     bool
	SafeNavigation bool
	RefreshSeconds int
	Hosts          []DashboardHostView
	Attention      []DashboardAttentionView
}

type DashboardHostView struct {
	ConfiguredHostID string
	Label            string
	PeerStatus       string
	PeerMessage      string
	LastSeen         string
	HasState         bool
	View             DesktopViewModel
}

type DashboardAttentionView struct {
	ScopedKey string
	HostLabel string
	Task      TaskView
}

func buildDesktopViewModel(pub state.PublicState, now time.Time, mock bool, layout string) DesktopViewModel {
	return DesktopViewModel{
		ViewModel: BuildViewModel(pub, now, mock, layout),
		Network:   buildNetworkView(pub),
		Tasks:     buildTaskViews(pub.Tasks, now),
	}
}

func buildDashboardViewModel(model dashboard.State, now time.Time, mock bool) DashboardDesktopViewModel {
	vm := DashboardDesktopViewModel{
		Mock:       mock,
		Updated:    now.UTC().Format("15:04:05 UTC"),
		SingleHost: len(model.Hosts) == 1,
		Hosts:      make([]DashboardHostView, 0, len(model.Hosts)),
	}
	for i, host := range model.Hosts {
		label := host.ConfiguredHostID
		if host.DisplayName != "" {
			// Registry display name is the trusted cross-node label.
			label = host.DisplayName + " · " + host.ConfiguredHostID
		} else if host.State != nil && host.State.Host.DisplayName != "" {
			label = host.State.Host.DisplayName + " · " + host.State.Host.ID
		}
		peerStatus := strings.ToUpper(string(host.Source.Status))
		if host.Source.Kind == dashboard.HostSourceLocal {
			peerStatus = "LOCAL"
		} else if host.SnapshotFreshness != nil && *host.SnapshotFreshness == dashboard.SnapshotStale {
			peerStatus += " · STALE"
		}
		hostView := DashboardHostView{
			ConfiguredHostID: host.ConfiguredHostID,
			Label:            label,
			PeerStatus:       peerStatus,
			PeerMessage:      host.Source.Message,
			LastSeen:         formatPeerLastSeen(host.Source.LastSuccessAt, now),
			HasState:         host.State != nil,
		}
		if host.State != nil {
			hostView.View = buildDesktopViewModel(*host.State, now, mock, "auto")
			for j := range hostView.View.Tasks {
				hostView.View.Tasks[j].ScopedKey = host.State.Host.ID + ":" + hostView.View.Tasks[j].Identity
				if hostView.View.Tasks[j].Attention != "" {
					vm.Attention = append(vm.Attention, DashboardAttentionView{ScopedKey: hostView.View.Tasks[j].ScopedKey, HostLabel: label, Task: hostView.View.Tasks[j]})
				}
			}
			if i == 0 {
				vm.SafeNavigation = host.State.Meta.SafeNavigationEnabled
			}
		}
		vm.Hosts = append(vm.Hosts, hostView)
	}
	return vm
}

func formatPeerLastSeen(last *time.Time, now time.Time) string {
	if last == nil {
		return ""
	}
	age := now.Sub(*last)
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return fmt.Sprintf("LAST SEEN %ds AGO", int(age/time.Second))
	}
	if age < time.Hour {
		return fmt.Sprintf("LAST SEEN %dm AGO", int(age/time.Minute))
	}
	return fmt.Sprintf("LAST SEEN %dh%02dm AGO", int(age/time.Hour), int((age%time.Hour)/time.Minute))
}

func buildNetworkView(pub state.PublicState) NetworkView {
	quality := strings.ToUpper(string(pub.Network.Quality))
	if quality == "" {
		quality = strings.ToUpper(string(state.NetworkUnknown))
	}
	sourceStatus := string(state.SourceUnavailable)
	if source, ok := pub.Sources["network"]; ok {
		sourceStatus = string(source.Status)
	}
	return NetworkView{Quality: quality, Reachable: formatReachable(pub.Network.Reachable), Latency: formatMilliseconds(pub.Network.ConnectLatencyMs), Failure: formatFailurePercent(pub.Network.ProbeFailurePercent), Receive: formatByteRate(pub.Network.ReceiveBytesPerSecond), Send: formatByteRate(pub.Network.SendBytesPerSecond), SourceStatus: sourceStatus}
}

func formatReachable(v *bool) string {
	if v == nil {
		return "N/A"
	}
	if *v {
		return "YES"
	}
	return "NO"
}
func formatMilliseconds(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.0f ms", *v)
}
func formatFailurePercent(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 100 {
		return "N/A"
	}
	if math.Abs(*v-math.Round(*v)) < 0.05 {
		return fmt.Sprintf("%.0f%%", *v)
	}
	return fmt.Sprintf("%.1f%%", *v)
}
func formatByteRate(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return "N/A"
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case *v >= gib:
		return fmt.Sprintf("%.1f GiB/s", *v/gib)
	case *v >= mib:
		return fmt.Sprintf("%.1f MiB/s", *v/mib)
	case *v >= kib:
		return fmt.Sprintf("%.1f KiB/s", *v/kib)
	default:
		return fmt.Sprintf("%.0f B/s", *v)
	}
}
