package web

import (
	"fmt"
	"math"
	"sort"
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
	StateClass   string
}

type DashboardDesktopViewModel struct {
	Mock           bool
	Updated        string
	ProductRole    string
	LegacyRefresh  bool
	SingleHost     bool
	SafeNavigation bool
	RefreshSeconds int
	HostCount      int
	OnlineCount    int
	StaleCount     int
	OfflineCount   int
	TaskCount      int
	WorkingCount   int
	CompleteCount  int
	Hosts          []DashboardHostView
	Tasks          []DashboardTaskView
	Attention      []DashboardAttentionView
	QuotaHosts     []DashboardQuotaHostView
	QuotaConnected bool
}

type DashboardHostView struct {
	ConfiguredHostID string
	Label            string
	ConnectionStatus string
	ConnectionClass  string
	SnapshotStatus   string
	SnapshotClass    string
	PeerMessage      string
	LastSeen         string
	HasState         bool
	EmptyTitle       string
	EmptyDetail      string
	View             DesktopViewModel
}

type DashboardTaskView struct {
	ScopedKey       string
	HostLabel       string
	ConnectionClass string
	Connection      string
	Snapshot        string
	Task            TaskView
}

type DashboardAttentionView struct {
	ScopedKey string
	HostLabel string
	Task      TaskView
}

type DashboardQuotaHostView struct {
	HostLabel string
	Quota     []QuotaView
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
		HostCount:  len(model.Hosts),
		Hosts:      make([]DashboardHostView, 0, len(model.Hosts)),
		Tasks:      []DashboardTaskView{},
		Attention:  []DashboardAttentionView{},
		QuotaHosts: []DashboardQuotaHostView{},
	}
	for i, host := range model.Hosts {
		label := dashboardHostLabel(host)
		connectionStatus := strings.ToUpper(string(host.Source.Status))
		if host.Source.Kind == dashboard.HostSourceLocal {
			connectionStatus = "LOCAL"
		}
		snapshotStatus := "NONE"
		if host.State != nil {
			snapshotStatus = "CURRENT"
			if host.SnapshotFreshness != nil && *host.SnapshotFreshness == dashboard.SnapshotStale {
				snapshotStatus = "RETAINED"
			}
		}
		hostView := DashboardHostView{
			ConfiguredHostID: host.ConfiguredHostID,
			Label:            label,
			ConnectionStatus: connectionStatus,
			ConnectionClass:  connectionStateClass(connectionStatus),
			SnapshotStatus:   snapshotStatus,
			SnapshotClass:    snapshotStateClass(snapshotStatus),
			PeerMessage:      host.Source.Message,
			LastSeen:         formatPeerLastSeen(host.Source.LastSuccessAt, now),
			HasState:         host.State != nil,
		}
		countConnection(&vm, connectionStatus)
		if host.State != nil {
			hostView.View = buildDesktopViewModel(*host.State, now, mock, "auto")
			for j := range hostView.View.Tasks {
				hostView.View.Tasks[j].ScopedKey = host.State.Host.ID + ":" + hostView.View.Tasks[j].Identity
				item := DashboardTaskView{
					ScopedKey:       hostView.View.Tasks[j].ScopedKey,
					HostLabel:       label,
					ConnectionClass: hostView.ConnectionClass,
					Connection:      connectionStatus,
					Snapshot:        snapshotStatus,
					Task:            hostView.View.Tasks[j],
				}
				vm.Tasks = append(vm.Tasks, item)
				if hostView.View.Tasks[j].Attention != "" {
					vm.Attention = append(vm.Attention, DashboardAttentionView{ScopedKey: hostView.View.Tasks[j].ScopedKey, HostLabel: label, Task: hostView.View.Tasks[j]})
				}
				countTask(&vm, hostView.View.Tasks[j])
			}
			if hostView.View.QuotaConnected {
				vm.QuotaConnected = true
				vm.QuotaHosts = append(vm.QuotaHosts, DashboardQuotaHostView{HostLabel: label, Quota: hostView.View.Quota})
			}
			if i == 0 || host.State.Meta.SafeNavigationEnabled {
				vm.SafeNavigation = host.State.Meta.SafeNavigationEnabled
			}
		} else if host.Source.LastSuccessAt == nil {
			hostView.EmptyTitle = "Awaiting first snapshot"
			hostView.EmptyDetail = "This registered Node has not delivered an accepted snapshot yet."
		} else {
			hostView.EmptyTitle = "No retained snapshot"
			hostView.EmptyDetail = "The Node remains registered, but its last accepted state is no longer retained."
		}
		vm.Hosts = append(vm.Hosts, hostView)
	}
	sortDashboardTasks(vm.Tasks)
	return vm
}

func dashboardHostLabel(host dashboard.HostSnapshot) string {
	label := host.ConfiguredHostID
	if host.DisplayName != "" {
		// Registry display name is the trusted cross-node label.
		if host.DisplayName == host.ConfiguredHostID {
			return host.DisplayName
		}
		return host.DisplayName + " · " + host.ConfiguredHostID
	}
	if host.State != nil && host.State.Host.DisplayName != "" {
		if host.State.Host.DisplayName == host.State.Host.ID {
			return host.State.Host.DisplayName
		}
		return host.State.Host.DisplayName + " · " + host.State.Host.ID
	}
	return label
}

func countConnection(vm *DashboardDesktopViewModel, status string) {
	switch status {
	case "ONLINE", "LOCAL", "AVAILABLE":
		vm.OnlineCount++
	case "STALE", "DEGRADED":
		vm.StaleCount++
	default:
		vm.OfflineCount++
	}
}

func countTask(vm *DashboardDesktopViewModel, task TaskView) {
	vm.TaskCount++
	switch {
	case strings.HasPrefix(task.Lifecycle, "WORKING"):
		vm.WorkingCount++
	case strings.HasPrefix(task.Lifecycle, "COMPLETE"):
		vm.CompleteCount++
	}
}

func sortDashboardTasks(tasks []DashboardTaskView) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Task.Priority != tasks[j].Task.Priority {
			return tasks[i].Task.Priority < tasks[j].Task.Priority
		}
		if tasks[i].HostLabel != tasks[j].HostLabel {
			return tasks[i].HostLabel < tasks[j].HostLabel
		}
		if tasks[i].Task.Provider != tasks[j].Task.Provider {
			return tasks[i].Task.Provider < tasks[j].Task.Provider
		}
		return tasks[i].Task.Title < tasks[j].Task.Title
	})
}

func connectionStateClass(status string) string {
	switch status {
	case "ONLINE", "LOCAL", "AVAILABLE":
		return "is-online"
	case "STALE", "DEGRADED":
		return "is-stale"
	default:
		return "is-offline"
	}
}

func snapshotStateClass(status string) string {
	switch status {
	case "CURRENT":
		return "is-current"
	case "RETAINED":
		return "is-retained"
	default:
		return "is-none"
	}
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
		return fmt.Sprintf("LAST RECEIVED %ds AGO", int(age/time.Second))
	}
	if age < time.Hour {
		return fmt.Sprintf("LAST RECEIVED %dm AGO", int(age/time.Minute))
	}
	return fmt.Sprintf("LAST RECEIVED %dh%02dm AGO", int(age/time.Hour), int((age%time.Hour)/time.Minute))
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
	return NetworkView{Quality: quality, Reachable: formatReachable(pub.Network.Reachable), Latency: formatMilliseconds(pub.Network.ConnectLatencyMs), Failure: formatFailurePercent(pub.Network.ProbeFailurePercent), Receive: formatByteRate(pub.Network.ReceiveBytesPerSecond), Send: formatByteRate(pub.Network.SendBytesPerSecond), SourceStatus: sourceStatus, StateClass: networkStateClass(pub.Network.Quality, sourceStatus)}
}

func networkStateClass(quality state.NetworkQuality, sourceStatus string) string {
	if sourceStatus == string(state.SourceUnavailable) || quality == state.NetworkOffline {
		return "is-offline"
	}
	if sourceStatus == string(state.SourceDegraded) || quality == state.NetworkDegraded {
		return "is-stale"
	}
	if quality == state.NetworkGood {
		return "is-online"
	}
	return "is-unknown"
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
