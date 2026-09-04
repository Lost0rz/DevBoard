package web

import (
	"fmt"
	"hash/fnv"
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
	FragmentPath   string
	ReturnPath     string
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
	Pad            PadDashboardViewModel
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

// PadDashboardViewModel is the presentation-only projection for /display.
// It deliberately does not reuse the desktop task fields that contain
// project, worktree, branch, result, source, or lifecycle diagnostics.
type PadDashboardViewModel struct {
	Connection       PadConnectionView
	Tasks            []PadTaskView
	HiddenTaskCount  int
	DeckClass        string
	Hosts            []PadHostView
	HiddenHostCount  int
	Quota            []PadQuotaView
	QuotaConnected   bool
	WebConnected     bool
	WebStale         bool
	WebNotifications []PadWebNotificationView
	LowerBandClass   string
}

type PadConnectionView struct {
	HubStatus    string
	HubClass     string
	HostCount    int
	OnlineCount  int
	StaleCount   int
	OfflineCount int
	MacLabel     string
	MacStatus    string
	MacClass     string
	Updated      string
}

type PadTaskView struct {
	TaskID             string
	ScopedKey          string
	Provider           string
	ProviderClass      string
	State              string
	StateClass         string
	Title              string
	DetailLabel        string
	Detail             string
	HostLabel          string
	HostDisplayName    string
	HostID             string
	HostAccentClass    string
	Age                string
	Unread             bool
	Stale              bool
	StaleLabel         string
	ReadyError         bool
	CompletionPhase    string
	Navigable          bool
	NavigationTargetID string
	NavigationAction   string

	priority int
	sortAt   time.Time
}

type PadHostView struct {
	HostID          string
	Label           string
	AccentClass     string
	Connection      string
	ConnectionClass string
	Stale           bool
	HasState        bool
	CPU             PadMetricView
	Memory          PadMetricView
	Swap            PadMetricView
	Disk            PadMetricView
	activeTasks     int
	healthPriority  int
}

// PadMetricView keeps the large, glanceable percentage separate from the
// lower-priority used/total detail. The percentage is either authoritative or
// derived from the same real used/total pair; it is never estimated.
type PadMetricView struct {
	Percent     string
	Detail      string
	Status      string
	StatusClass string
	RailPercent int
}

type PadQuotaView struct {
	Provider    string
	AccentClass string
	Windows     []QuotaWindowView
	Freshness   string
	ObservedBy  string
	AccountKey  string
}

type PadWebNotificationView struct {
	Service      string
	Status       string
	Conversation string
	Age          string
}

const (
	padTaskCapacity           = 4
	padCompleteHighVisibility = 10 * time.Minute
	padCompleteRetention      = 30 * time.Minute
	// Stale tasks remain in retained state/audit data, but an old snapshot must
	// not occupy the live board indefinitely or look like current work.
	staleTaskRetention = 24 * time.Hour
)

func buildDesktopViewModel(pub state.PublicState, now time.Time, mock bool, layout string) DesktopViewModel {
	return buildDesktopViewModelWithTimezone(pub, now, mock, layout, "")
}

func buildDesktopViewModelWithTimezone(pub state.PublicState, now time.Time, mock bool, layout, timezone string) DesktopViewModel {
	return DesktopViewModel{
		ViewModel: buildViewModelWithTimezone(pub, now, mock, timezone),
		Network:   buildNetworkView(pub),
		Tasks:     buildTaskViews(pub.Tasks, now),
	}
}

func buildDashboardViewModel(model dashboard.State, now time.Time, mock bool) DashboardDesktopViewModel {
	return buildDashboardViewModelWithTimezone(model, now, mock, "")
}

func buildDashboardViewModelWithTimezone(model dashboard.State, now time.Time, mock bool, timezone string) DashboardDesktopViewModel {
	vm := DashboardDesktopViewModel{
		Mock:       mock,
		Updated:    now.UTC().Format("15:04:05 UTC"),
		ReturnPath: "/display",
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
			hostView.View = buildDesktopViewModelWithTimezone(*host.State, now, mock, "auto", timezone)
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
	vm.Pad = buildPadDashboardViewModelWithTimezone(model, now, timezone)
	return vm
}

func buildPadDashboardViewModel(model dashboard.State, now time.Time) PadDashboardViewModel {
	return buildPadDashboardViewModelWithTimezone(model, now, "")
}

func buildPadDashboardViewModelWithTimezone(model dashboard.State, now time.Time, timezone string) PadDashboardViewModel {
	vm := PadDashboardViewModel{
		Tasks:            []PadTaskView{},
		Hosts:            []PadHostView{},
		Quota:            []PadQuotaView{},
		WebNotifications: []PadWebNotificationView{},
	}

	highVisibility, retention := padCompletionWindows(model)
	configuredHostCount := len(model.Hosts)
	quotaByKey := make(map[string]int)
	if len(model.Quota) > 0 {
		globalQuota, _ := buildPadQuotaEntriesWithTimezone(model.Quota, now, timezone)
		for _, quota := range globalQuota {
			key := padQuotaIdentity(quota, len(vm.Quota))
			quotaByKey[key] = len(vm.Quota)
			vm.Quota = append(vm.Quota, quota)
		}
		vm.QuotaConnected = len(vm.Quota) > 0
	}
	for _, host := range model.Hosts {
		label := dashboardHostLabel(host)
		connection := padConnectionStatus(string(host.Source.Status))
		stale := connection != "ONLINE"
		if host.SnapshotFreshness != nil && *host.SnapshotFreshness == dashboard.SnapshotStale {
			stale = true
		}
		padHost := PadHostView{
			HostID:          host.ConfiguredHostID,
			Label:           label,
			AccentClass:     padHostAccentClass(host.ConfiguredHostID),
			Connection:      connection,
			ConnectionClass: padConnectionClass(connection),
			Stale:           stale,
			HasState:        host.State != nil,
			CPU:             padUnavailableMetric(),
			Memory:          padUnavailableMetric(),
			Swap:            padUnavailableMetric(),
			Disk:            padUnavailableMetric(),
		}
		if accent := padHostAccentClassFromName(host.Accent); accent != "" {
			padHost.AccentClass = accent
		}
		if host.State != nil {
			padHost.CPU = padCPUMetric(host.State.System.CPUPercent)
			padHost.Memory = padMetric(host.State.System.Memory)
			padHost.Swap = padMetric(host.State.System.Swap)
			padHost.Disk = padMetric(host.State.System.Disk)
			for _, task := range host.State.Tasks {
				view, ok := buildPadTaskView(task, label, dashboardHostDisplayName(host), host.State.Host.ID, host.Accent, now, highVisibility, retention, stale)
				if !ok {
					continue
				}
				vm.Tasks = append(vm.Tasks, view)
				if task.Lifecycle != state.TaskComplete {
					padHost.activeTasks++
				}
			}
			if len(model.Quota) == 0 {
				padQuota, connected := buildPadQuotaWithTimezone(host.State, now, timezone)
				if connected {
					vm.QuotaConnected = true
					for _, quota := range padQuota {
						key := padQuotaIdentity(quota, len(vm.Quota))
						if index, exists := quotaByKey[key]; exists {
							// A duplicate observation is not allowed to replace a
							// fresher value. The deterministic first observation is
							// already enough for the global Pad projection.
							if quota.Freshness == "" && vm.Quota[index].Freshness != "" {
								vm.Quota[index] = quota
							}
							continue
						}
						quotaByKey[key] = len(vm.Quota)
						vm.Quota = append(vm.Quota, quota)
					}
				}
			}
			if webConnected, webStale := padBrowserSourceStatus(host.State.Sources); webConnected {
				vm.WebConnected = true
				vm.WebStale = vm.WebStale || webStale
			}
		}
		if connection == "OFFLINE" && host.State != nil {
			padHost.CPU = padRetainedMetricUnavailable(padHost.CPU)
			padHost.Memory = padRetainedMetricUnavailable(padHost.Memory)
			padHost.Swap = padRetainedMetricUnavailable(padHost.Swap)
			padHost.Disk = padRetainedMetricUnavailable(padHost.Disk)
		}
		padHost.healthPriority = padHostPriority(connection, padHost.activeTasks)
		vm.Hosts = append(vm.Hosts, padHost)
	}

	if len(vm.Hosts) == 0 {
		vm.Hosts = append(vm.Hosts, PadHostView{
			Label: "MAC NOT CONNECTED", Connection: "OFFLINE", ConnectionClass: padConnectionClass("OFFLINE"),
			Stale: true, CPU: padUnavailableMetric(), Memory: padUnavailableMetric(), Swap: padUnavailableMetric(), Disk: padUnavailableMetric(),
		})
	}

	sortPadTasks(vm.Tasks)
	if len(vm.Tasks) > padTaskCapacity {
		vm.HiddenTaskCount = len(vm.Tasks) - padTaskCapacity
		vm.Tasks = vm.Tasks[:padTaskCapacity]
	}
	sort.SliceStable(vm.Hosts, func(i, j int) bool {
		if vm.Hosts[i].healthPriority != vm.Hosts[j].healthPriority {
			return vm.Hosts[i].healthPriority < vm.Hosts[j].healthPriority
		}
		if vm.Hosts[i].activeTasks != vm.Hosts[j].activeTasks {
			return vm.Hosts[i].activeTasks > vm.Hosts[j].activeTasks
		}
		return vm.Hosts[i].Label < vm.Hosts[j].Label
	})
	if len(vm.Hosts) > 3 {
		vm.HiddenHostCount = len(vm.Hosts) - 3
		vm.Hosts = vm.Hosts[:3]
	}
	vm.DeckClass = fmt.Sprintf("agent-count-%d", len(vm.Tasks))
	if !vm.QuotaConnected && !vm.WebConnected {
		vm.LowerBandClass = "pad-ai-unavailable-layout"
	}

	vm.Connection = PadConnectionView{
		HubStatus: "ONLINE", HubClass: "pad-status-online", HostCount: configuredHostCount,
		OnlineCount:  countPadHostStatus(model.Hosts, "ONLINE"),
		StaleCount:   countPadHostStatus(model.Hosts, "STALE"),
		OfflineCount: countPadHostStatus(model.Hosts, "OFFLINE"),
		Updated:      padUpdatedAge(model.GeneratedAt, now),
	}
	if vm.Connection.StaleCount > 0 || vm.Connection.OfflineCount > 0 || vm.Connection.HostCount == 0 {
		vm.Connection.HubStatus = "DEGRADED"
		vm.Connection.HubClass = "pad-status-stale"
	}
	if len(vm.Hosts) > 0 {
		vm.Connection.MacLabel = vm.Hosts[0].Label
		vm.Connection.MacStatus = vm.Hosts[0].Connection
		vm.Connection.MacClass = vm.Hosts[0].ConnectionClass
	}
	return vm
}

func countPadHostStatus(hosts []dashboard.HostSnapshot, want string) int {
	count := 0
	for _, host := range hosts {
		if padConnectionStatus(string(host.Source.Status)) == want {
			count++
		}
	}
	return count
}

func padHostPriority(connection string, activeTasks int) int {
	// Offline/stale hosts are surfaced before healthy hosts. Active work is
	// the tie-breaker for large registries; host labels finish the ordering.
	base := map[string]int{"OFFLINE": 0, "STALE": 1, "ONLINE": 2}[connection]
	return base*1000 - activeTasks
}

func padRetainedMetricUnavailable(metric PadMetricView) PadMetricView {
	if metric.Percent == "--" {
		return metric
	}
	metric.Status = "UNAVAILABLE"
	metric.StatusClass = "pad-metric-unavailable"
	return metric
}

func padCompletionWindows(model dashboard.State) (time.Duration, time.Duration) {
	highVisibility, retention := padCompleteHighVisibility, padCompleteRetention
	for _, host := range model.Hosts {
		if host.State == nil {
			continue
		}
		if host.State.Meta.CompleteHighVisibilitySeconds > 0 {
			highVisibility = time.Duration(host.State.Meta.CompleteHighVisibilitySeconds) * time.Second
		}
		if host.State.Meta.CompleteRetentionSeconds > 0 {
			retention = time.Duration(host.State.Meta.CompleteRetentionSeconds) * time.Second
		}
		break
	}
	if retention < highVisibility {
		retention = highVisibility
	}
	return highVisibility, retention
}

func buildPadTaskView(task state.PublicTask, hostLabel, hostDisplayName, hostID, hostAccent string, now time.Time, highVisibility, retention time.Duration, hostStale bool) (PadTaskView, bool) {
	if staleTaskExpired(task, now) {
		return PadTaskView{}, false
	}
	provider := padProviderLabel(task.Provider)
	title := truncatePadText(task.Title, 140)
	if title == "" {
		title = "Task title unavailable"
	}
	view := PadTaskView{
		TaskID:          task.ID,
		ScopedKey:       hostID + ":" + task.ID,
		Provider:        provider,
		ProviderClass:   padProviderClass(provider),
		Title:           title,
		HostLabel:       hostLabel,
		HostDisplayName: hostDisplayName,
		HostID:          hostID,
		HostAccentClass: padHostAccentClass(hostID),
		Age:             "AGE UNAVAILABLE",
		Unread:          task.Unread,
		Stale:           hostStale || task.Freshness == state.FreshnessStale,
		StaleLabel:      "DATA STALE",
		sortAt:          task.UpdatedAt,
	}
	if action, ok := taskNavigation(task.Navigation); ok {
		view.Navigable = true
		view.NavigationTargetID = task.Navigation.TargetID
		view.NavigationAction = string(action)
	}
	if accent := padHostAccentClassFromName(hostAccent); accent != "" {
		view.HostAccentClass = accent
	}

	switch {
	case task.Lifecycle == state.TaskError && task.SupersededAt != nil:
		// Recovered error (2026-08-25 amendment): a newer turn of the same
		// session later terminated with a valid terminal Stop, so this error
		// no longer needs user action and must not occupy a Pad READY slot.
		// The card stays auditable in the internal state, not on the Pad.
		return PadTaskView{}, false
	case task.Lifecycle == state.TaskError:
		// A provider failure is not automatically a user decision point. Only
		// the bounded error kinds projected into Attention belong in the READY
		// queue; an unclassified StopFailure stays in diagnostics and must not
		// create a misleading approval card.
		if task.Attention == nil {
			return PadTaskView{}, false
		}
		view.State = "READY"
		view.StateClass = "pad-task-ready pad-task-ready-error"
		view.DetailLabel = "ACTION REQUIRED"
		view.Detail = padAttentionText(task)
		if task.Checkpoint != nil {
			view.Detail = mergePadFeedback(view.Detail, task.Checkpoint.Text)
		}
		view.ReadyError = true
		view.priority = 0
		view.sortAt = padAttentionTime(task)
	case task.Lifecycle == state.TaskLifecycleAttention || task.Attention != nil:
		view.State = "READY"
		view.StateClass = "pad-task-ready"
		view.DetailLabel = "ACTION REQUIRED"
		view.Detail = padAttentionText(task)
		if task.Checkpoint != nil {
			view.Detail = mergePadFeedback(view.Detail, task.Checkpoint.Text)
		}
		view.priority = 1
		view.sortAt = padAttentionTime(task)
	case task.Lifecycle == state.TaskWorking:
		view.State = "WORKING"
		view.StateClass = "pad-task-working"
		view.DetailLabel = "FEEDBACK"
		if view.Stale {
			// Keep the lifecycle as working in retained state for recovery and
			// audit, but never present an old snapshot as live work.
			view.State = "STALE"
			view.DetailLabel = "WAS WORKING"
		}
		if task.Checkpoint != nil {
			view.Detail = truncatePadText(task.Checkpoint.Text, 180)
			if view.Detail == "" {
				view.Detail = strings.ReplaceAll(strings.ToUpper(string(task.Checkpoint.Kind)), "_", " ")
			}
		}
		view.priority = 3
		if view.Stale {
			view.priority = 2
		}
		view.sortAt = task.UpdatedAt
	case task.Lifecycle == state.TaskComplete:
		// The Pad is an actionable home surface, not a completed-task history
		// view. A terminal task remains here only until the user opens it (or a
		// later provider turn acknowledges it). The Hub/local acknowledgement
		// path projects that transition as Unread=false.
		if !task.Unread {
			return PadTaskView{}, false
		}
		completedAt := task.UpdatedAt
		if task.Completion != nil && !task.Completion.At.IsZero() {
			completedAt = task.Completion.At
		}
		if !completedAt.IsZero() {
			age := now.Sub(completedAt)
			if age < 0 {
				age = 0
			}
			if age >= retention && !view.Unread {
				return PadTaskView{}, false
			}
			view.Age = "DONE " + formatPadAge(age) + " AGO"
			view.sortAt = completedAt
			if age < highVisibility {
				view.CompletionPhase = "high"
				view.priority = 4
			} else {
				view.CompletionPhase = "muted"
				view.priority = 5
			}
		} else {
			view.priority = 4
		}
		// An unread terminal result remains visible after current work but ahead
		// of ordinary completed history until the user opens it or the provider
		// confirms a later turn.
		view.priority = 4
		view.State = "COMPLETE"
		view.StateClass = "pad-task-complete"
		if view.CompletionPhase == "muted" {
			view.StateClass += " pad-task-complete-muted"
		}
		view.DetailLabel = "COMPLETION"
		if task.Completion != nil && task.Completion.Summary != nil {
			view.Detail = truncatePadText(*task.Completion.Summary, 180)
		}
		if task.Checkpoint != nil {
			view.Detail = mergePadFeedback(view.Detail, task.Checkpoint.Text)
		}
	default:
		return PadTaskView{}, false
	}

	if view.Stale {
		view.StateClass += " pad-task-stale"
	}
	if task.Lifecycle != state.TaskComplete {
		view.Age = padTaskAge(task, now)
	}
	return view, true
}

// mergePadFeedback keeps the task card to two semantic regions: the request
// title and one bounded feedback block. Checkpoint text is useful context, but
// its old standalone label consumed space without adding a distinct action.
func mergePadFeedback(primary, checkpoint string) string {
	primary = truncatePadText(primary, 118)
	checkpoint = truncatePadText(checkpoint, 118)
	if primary == "" {
		return checkpoint
	}
	if checkpoint == "" {
		return primary
	}
	return truncatePadText(primary+" · "+checkpoint, 180)
}

func padHostAccentClassFromName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "blue", "cyan", "violet", "amber", "green":
		return "pad-host-accent-" + strings.ToLower(strings.TrimSpace(name))
	default:
		return ""
	}
}

func sortPadTasks(tasks []PadTaskView) {
	sort.SliceStable(tasks, func(i, j int) bool {
		left, right := tasks[i], tasks[j]
		if left.priority != right.priority {
			return left.priority < right.priority
		}
		switch left.priority {
		case 0, 1, 2:
			if !left.sortAt.Equal(right.sortAt) {
				return left.sortAt.Before(right.sortAt)
			}
		case 3, 4, 5:
			if !left.sortAt.Equal(right.sortAt) {
				return left.sortAt.After(right.sortAt)
			}
		}
		if left.HostLabel != right.HostLabel {
			return left.HostLabel < right.HostLabel
		}
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		return left.ScopedKey < right.ScopedKey
	})
}

func padProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return "CODEX"
	case "claude-code", "claude code", "claude":
		return "CLAUDE CODE"
	default:
		if strings.TrimSpace(provider) == "" {
			return "AI PROVIDER"
		}
		return strings.ToUpper(strings.TrimSpace(provider))
	}
}

func padProviderClass(provider string) string {
	switch strings.ToUpper(strings.TrimSpace(provider)) {
	case "CLAUDE CODE":
		return "pad-provider-claude"
	case "CODEX":
		return "pad-provider-codex"
	default:
		return "pad-provider-other"
	}
}

// padHostAccentClass provides a deterministic fallback for legacy registries
// without an explicit display-accent field. The identity marker is
// presentation-only; it never participates in node authentication or
// deduplication.
func padHostAccentClass(hostID string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(hostID))))
	return []string{"pad-host-accent-blue", "pad-host-accent-cyan", "pad-host-accent-violet", "pad-host-accent-amber", "pad-host-accent-green"}[h.Sum32()%5]
}

func padAttentionText(task state.PublicTask) string {
	if task.Attention != nil {
		if text := truncatePadText(task.Attention.Text, 180); text != "" {
			return text
		}
	}
	return "Action details unavailable."
}

func padAttentionTime(task state.PublicTask) time.Time {
	if task.Attention != nil && !task.Attention.At.IsZero() {
		return task.Attention.At
	}
	return task.UpdatedAt
}

func padTaskAge(task state.PublicTask, now time.Time) string {
	start := task.StartedAt
	if start.IsZero() {
		return "AGE UNAVAILABLE"
	}
	age := now.Sub(start)
	if age < 0 {
		age = 0
	}
	return formatPadAge(age)
}

func formatPadAge(age time.Duration) string {
	if age < time.Minute {
		return "<1M"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dM", int(age/time.Minute))
	}
	return fmt.Sprintf("%dH%02dM", int(age/time.Hour), int((age%time.Hour)/time.Minute))
}

func padUpdatedAge(at, now time.Time) string {
	if at.IsZero() {
		return "UPDATED --"
	}
	age := now.Sub(at)
	if age < 0 {
		age = 0
	}
	return fmt.Sprintf("UPDATED %s UTC · %s AGO", at.UTC().Format("15:04:05"), formatPadAge(age))
}

func truncatePadText(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func padConnectionStatus(status string) string {
	switch strings.ToUpper(status) {
	case "ONLINE", "LOCAL", "AVAILABLE":
		return "ONLINE"
	case "STALE", "DEGRADED":
		return "STALE"
	default:
		return "OFFLINE"
	}
}

func padConnectionClass(status string) string {
	switch status {
	case "ONLINE":
		return "pad-status-online"
	case "STALE":
		return "pad-status-stale"
	default:
		return "pad-status-offline"
	}
}

func padUnavailableMetric() PadMetricView {
	return PadMetricView{Percent: "--", Detail: "UNAVAILABLE", Status: "UNAVAILABLE", StatusClass: "pad-metric-unavailable"}
}

func padCPUMetric(value *float64) PadMetricView {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return padUnavailableMetric()
	}
	return padMetricFromPercent(*value, "UTILIZATION")
}

func padMetric(metric state.PublicMetricSet) PadMetricView {
	var percent float64
	hasPercent := metric.PercentUsed != nil && !math.IsNaN(*metric.PercentUsed) && !math.IsInf(*metric.PercentUsed, 0) && *metric.PercentUsed >= 0
	if hasPercent {
		percent = *metric.PercentUsed
	} else if metric.UsedBytes != nil && metric.TotalBytes != nil && *metric.TotalBytes > 0 && *metric.UsedBytes >= 0 {
		percent = float64(*metric.UsedBytes) / float64(*metric.TotalBytes) * 100
		hasPercent = true
	}
	if !hasPercent {
		return padUnavailableMetric()
	}

	detail := "USED/TOTAL UNAVAILABLE"
	if metric.UsedBytes != nil && metric.TotalBytes != nil {
		detail = fmt.Sprintf("%.1f / %.1f GiB", float64(*metric.UsedBytes)/(1024*1024*1024), float64(*metric.TotalBytes)/(1024*1024*1024))
	}
	view := padMetricFromPercent(percent, detail)
	return view
}

func padMetricFromPercent(percent float64, detail string) PadMetricView {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
		return padUnavailableMetric()
	}
	rail := int(math.Round(percent))
	if rail < 0 {
		rail = 0
	}
	if rail > 100 {
		rail = 100
	}
	status, class := padMetricStatus(percent)
	return PadMetricView{
		Percent:     fmt.Sprintf("%.0f%%", percent),
		Detail:      detail,
		Status:      status,
		StatusClass: class,
		RailPercent: rail,
	}
}

func padMetricStatus(percent float64) (string, string) {
	switch {
	case percent >= 90:
		return "CRITICAL", "pad-metric-critical"
	case percent >= 70:
		return "WARNING", "pad-metric-warning"
	default:
		return "NORMAL", "pad-metric-normal"
	}
}

func buildPadQuota(pub *state.PublicState, now time.Time) ([]PadQuotaView, bool) {
	return buildPadQuotaWithTimezone(pub, now, "")
}

func buildPadQuotaWithTimezone(pub *state.PublicState, now time.Time, timezone string) ([]PadQuotaView, bool) {
	if pub == nil {
		return nil, false
	}
	return buildPadQuotaEntriesWithTimezone(pub.Quota, now, timezone)
}

func buildPadQuotaEntries(sourceQuotas []state.PublicQuota, now time.Time) ([]PadQuotaView, bool) {
	return buildPadQuotaEntriesWithTimezone(sourceQuotas, now, "")
}

func buildPadQuotaEntriesWithTimezone(sourceQuotas []state.PublicQuota, now time.Time, timezone string) ([]PadQuotaView, bool) {
	out := make([]PadQuotaView, 0, len(sourceQuotas))
	for _, sourceQuota := range sourceQuotas {
		if sourceQuota.SourceStatus != state.SourceAvailable && sourceQuota.SourceStatus != state.SourceDegraded {
			continue
		}
		quota, connected := buildQuotaWithTimezone([]state.PublicQuota{sourceQuota}, now, timezone)
		if !connected || len(quota) != 1 || len(quota[0].Windows) == 0 || strings.TrimSpace(quota[0].Provider) == "" {
			continue
		}
		label := strings.TrimSpace(sourceQuota.DisplayLabel)
		if label == "" {
			label = quota[0].Provider
		}
		if padIsGLMQuota(sourceQuota.Provider, label) {
			quota[0].Windows = padGLMTokenWindow(quota[0].Windows)
			if len(quota[0].Windows) == 0 {
				continue
			}
		}
		freshness := ""
		if sourceQuota.SourceStatus == state.SourceDegraded {
			freshness = "DATA STALE"
			for i := range quota[0].Windows {
				quota[0].Windows[i].StatusClass += " pad-quota-stale"
			}
		}
		out = append(out, PadQuotaView{
			Provider: label, AccentClass: padQuotaAccentClass(label, len(out)),
			Windows: quota[0].Windows, Freshness: freshness,
			ObservedBy: sourceQuota.ObservedBy, AccountKey: sourceQuota.AccountKey,
		})
	}
	return out, len(out) > 0
}

func padIsGLMQuota(provider, label string) bool {
	for _, value := range []string{provider, label} {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "zai" || normalized == "z.ai" || strings.Contains(normalized, "z ai") || strings.Contains(normalized, "glm") {
			return true
		}
	}
	return false
}

// padGLMTokenWindow keeps the GLM token allowance as the single Pad row. MCP
// is an auxiliary window and is intentionally not presented as account quota.
// If CodexBar changes the token window label, the first non-MCP window remains
// a safe fallback while MCP is always excluded.
func padGLMTokenWindow(windows []QuotaWindowView) []QuotaWindowView {
	for _, window := range windows {
		name := strings.ToLower(strings.TrimSpace(window.Name))
		if strings.Contains(name, "mcp") {
			continue
		}
		if strings.Contains(name, "token") || strings.Contains(name, "primary") || strings.Contains(name, "quota") {
			return []QuotaWindowView{window}
		}
	}
	for _, window := range windows {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(window.Name)), "mcp") {
			return []QuotaWindowView{window}
		}
	}
	return nil
}

// padQuotaAccentClass keeps account rows visually distinct without exposing
// account identity. Known default aliases get stable semantic colors; custom
// aliases fall back to the deterministic source order so renaming an account
// does not collapse the three-row visual contract.
func padQuotaAccentClass(label string, index int) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(normalized, "codex a") || strings.Contains(normalized, "codex-a"):
		return "pad-quota-account-blue"
	case strings.Contains(normalized, "codex b") || strings.Contains(normalized, "codex-b"):
		return "pad-quota-account-violet"
	case strings.Contains(normalized, "glm") || strings.Contains(normalized, "z.ai") || strings.Contains(normalized, "z ai"):
		return "pad-quota-account-amber"
	}
	switch index % 3 {
	case 1:
		return "pad-quota-account-violet"
	case 2:
		return "pad-quota-account-amber"
	default:
		return "pad-quota-account-blue"
	}
}

func padQuotaIdentity(quota PadQuotaView, fallback int) string {
	provider := strings.ToLower(strings.TrimSpace(quota.Provider))
	if quota.AccountKey != "" {
		return provider + "\x00" + quota.AccountKey
	}
	// Legacy quota observations without an identity are kept separate rather
	// than guessed to be the same account.
	return fmt.Sprintf("legacy\x00%s\x00%d", provider, fallback)
}

func padBrowserSourceStatus(sources map[string]state.PublicSourceHealth) (bool, bool) {
	for _, name := range []string{"browser-ai-watch", "web-ai-watch", "browser-watch", "web-watch"} {
		source, ok := sources[name]
		if !ok {
			continue
		}
		if source.Status == state.SourceAvailable {
			return true, false
		}
		if source.Status == state.SourceDegraded {
			return true, true
		}
	}
	return false, false
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

func dashboardHostDisplayName(host dashboard.HostSnapshot) string {
	if strings.TrimSpace(host.DisplayName) != "" {
		return strings.TrimSpace(host.DisplayName)
	}
	if host.State != nil && strings.TrimSpace(host.State.Host.DisplayName) != "" {
		return strings.TrimSpace(host.State.Host.DisplayName)
	}
	if strings.TrimSpace(host.ConfiguredHostID) != "" {
		return strings.TrimSpace(host.ConfiguredHostID)
	}
	return "MAC UNAVAILABLE"
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
