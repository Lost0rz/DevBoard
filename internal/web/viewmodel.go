package web

import (
	"fmt"
	"sort"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	kindleLandscapeCapacity = 3
	kindlePortraitCapacity  = 2
)

type ViewModel struct {
	Mock            bool
	Layout          string
	Rotate          string
	RotationClass   string
	Updated         string
	KindleRefresh   int
	RotationSlot    int64
	Agents          []AgentView
	KindleAgents    []AgentView
	Alerts          []AlertView
	Sources         []SourceView
	System          SystemView
	SystemConnected bool
	SystemBar       string
	Projects        []ProjectView
	Quota           []QuotaView
	QuotaConnected  bool
	SafeNavigation  bool
}

type AgentView struct {
	ID              string
	Provider        string
	Status          state.DisplayStatus
	CompletionPhase state.CompletionPhase
	Priority        int
	Elapsed         string
	Attention       bool
	DeliveryTier    string
}

type AlertView struct{ Type, AgentID, TurnID string }
type SourceView struct{ Name, Status, Message string }
type SystemView struct {
	CPU, Memory, Swap, Disk string
	Groups                  []ProcessGroupView
}
type ProcessGroupView struct{ Name, CPU, RAM string }
type ProjectView struct{ Name, Branch, Status string }
type QuotaView struct {
	Provider, Status string
	Windows          []QuotaWindowView
}
type QuotaWindowView struct{ Name, Used string }

func BuildViewModel(pub state.PublicState, now time.Time, mock bool, layout string) ViewModel {
	return buildViewModel(pub, now, mock, layout, "none", false)
}

func BuildKindleViewModel(pub state.PublicState, now time.Time, mock bool, layout, rotate string) ViewModel {
	return buildViewModel(pub, now, mock, normalizeKindleLayout(layout), normalizeKindleRotate(rotate), true)
}

func buildViewModel(pub state.PublicState, now time.Time, mock bool, layout, rotate string, kindle bool) ViewModel {
	high := time.Duration(pub.Meta.CompleteHighVisibilitySeconds) * time.Second
	retention := time.Duration(pub.Meta.CompleteRetentionSeconds) * time.Second
	agents := make([]AgentView, 0, len(pub.Agents))
	for _, agent := range pub.Agents {
		turn := state.CurrentTurn{TurnID: agent.CurrentTurn.TurnID, Activity: agent.CurrentTurn.Activity, Outcome: agent.CurrentTurn.Outcome, Freshness: agent.CurrentTurn.Freshness, StartedAt: agent.CurrentTurn.StartedAt, CompletedAt: agent.CurrentTurn.CompletedAt, UpdatedAt: agent.CurrentTurn.UpdatedAt}
		derived := state.DeriveDisplay(turn, now, high, retention)
		agents = append(agents, AgentView{ID: agent.ID, Provider: agent.Provider, Status: derived.Status, CompletionPhase: derived.CompletionPhase, Priority: derived.Priority, Elapsed: formatDuration(elapsedDuration(agent.CurrentTurn, now)), Attention: derived.Status == state.DisplayAttention})
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Priority == agents[j].Priority {
			return agents[i].ID < agents[j].ID
		}
		return agents[i].Priority < agents[j].Priority
	})

	alerts := make([]AlertView, 0, len(pub.Alerts))
	for _, alert := range pub.Alerts {
		if !alert.Active || (alert.Type != state.AlertAttention && alert.Type != state.AlertError && alert.Type != state.AlertStale) {
			continue
		}
		turnID := ""
		if alert.TurnID != nil {
			turnID = *alert.TurnID
		}
		alerts = append(alerts, AlertView{Type: string(alert.Type), AgentID: alert.AgentID, TurnID: turnID})
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Type == alerts[j].Type {
			return alerts[i].AgentID < alerts[j].AgentID
		}
		return alerts[i].Type < alerts[j].Type
	})

	sources := make([]SourceView, 0, 2)
	for _, id := range []string{"codex-hooks", "claude-hooks"} {
		if source, ok := pub.Sources[id]; ok {
			sources = append(sources, SourceView{Name: id, Status: string(source.Status), Message: source.Message})
		}
	}
	groups := make([]ProcessGroupView, len(pub.System.ProcessGroups))
	for i, g := range pub.System.ProcessGroups {
		groups[i] = ProcessGroupView{Name: g.Name, CPU: formatPercent(g.CPUPercent), RAM: formatBytes(g.ResidentMemoryBytes)}
	}
	projects := make([]ProjectView, len(pub.Projects))
	for i, p := range pub.Projects {
		status := "CLEAN"
		if p.Dirty {
			status = fmt.Sprintf("DIRTY · %d modified · %d untracked", p.ModifiedCount, p.UntrackedCount)
		}
		projects[i] = ProjectView{Name: p.DisplayName, Branch: p.Branch, Status: status}
	}
	quota, quotaConnected := buildQuota(pub.Quota)
	systemConnected := false
	if source, ok := pub.Sources["system"]; ok && source.Status == state.SourceAvailable {
		systemConnected = true
	}
	system := SystemView{CPU: formatPercent(pub.System.CPUPercent), Memory: metricString(pub.System.Memory), Swap: metricString(pub.System.Swap), Disk: metricString(pub.System.Disk), Groups: groups}
	systemBar := "SYSTEM · NOT CONNECTED"
	if systemConnected {
		systemBar = fmt.Sprintf("CPU %s | MEM %s | SWAP %s | DISK %s | %s", system.CPU, system.Memory, system.Swap, system.Disk, now.Format("15:04"))
	}
	refresh := pub.Meta.KindleRefreshSeconds
	slot := int64(0)
	if refresh > 0 {
		slot = now.Unix() / int64(refresh)
	}
	vm := ViewModel{Mock: mock, Layout: layout, Rotate: rotate, RotationClass: "rotate-" + rotate, Updated: now.UTC().Format("15:04:05 UTC"), KindleRefresh: refresh, RotationSlot: slot, Agents: agents, Alerts: alerts, Sources: sources, System: system, SystemConnected: systemConnected, SystemBar: systemBar, Projects: projects, Quota: quota, QuotaConnected: quotaConnected, SafeNavigation: pub.Meta.SafeNavigationEnabled}
	if kindle {
		capacity := kindleLandscapeCapacity
		if layout == "portrait" {
			capacity = kindlePortraitCapacity
		}
		vm.KindleAgents = selectKindleAgents(pub.Agents, now, high, retention, capacity, slot)
	}
	return vm
}

func normalizeKindleLayout(v string) string {
	if v == "portrait" {
		return "portrait"
	}
	return "landscape"
}
func normalizeKindleRotate(v string) string {
	switch v {
	case "left", "right":
		return v
	default:
		return "none"
	}
}

func selectKindleAgents(agents []state.PublicAgent, now time.Time, high, promotion time.Duration, capacity int, slot int64) []AgentView {
	critical, promoted, active, resting := []AgentView{}, []AgentView{}, []AgentView{}, []AgentView{}
	for _, a := range agents {
		v := kindleAgentView(a, now, high, promotion)
		switch v.DeliveryTier {
		case "critical":
			critical = append(critical, v)
		case "promoted":
			promoted = append(promoted, v)
		case "active":
			active = append(active, v)
		case "resting":
			resting = append(resting, v)
		}
	}
	for _, q := range [][]AgentView{critical, promoted, active, resting} {
		sort.SliceStable(q, func(i, j int) bool { return q[i].ID < q[j].ID })
	}
	if capacity <= 0 {
		return nil
	}
	selected := make([]AgentView, 0, capacity)
	selected = append(selected, rotateTake(critical, capacity, slot)...)
	if len(selected) >= capacity {
		return selected
	}
	remaining := capacity - len(selected)
	if len(active) > 0 {
		if len(promoted) > 0 && remaining == 1 {
			if slot%2 == 0 {
				selected = append(selected, rotateTake(promoted, 1, slot/2)...)
			} else {
				selected = append(selected, rotateTake(active, 1, slot/2)...)
			}
			return selected
		}
		if len(promoted) > 0 && remaining >= 2 {
			selected = append(selected, rotateTake(promoted, 1, slot)...)
			remaining--
		}
		if remaining > 0 {
			take := remaining
			if take > len(active) {
				take = len(active)
			}
			selected = append(selected, rotateTake(active, take, slot)...)
			remaining -= take
		}
		if remaining > 0 {
			selected = appendUniqueRotated(selected, promoted, remaining, slot)
			remaining = capacity - len(selected)
		}
		if remaining > 0 {
			selected = appendUniqueRotated(selected, resting, remaining, slot)
		}
		return selected
	}
	if remaining > 0 {
		selected = appendUniqueRotated(selected, promoted, remaining, slot)
		remaining = capacity - len(selected)
	}
	if remaining > 0 {
		selected = appendUniqueRotated(selected, resting, remaining, slot)
	}
	return selected
}

func kindleAgentView(agent state.PublicAgent, now time.Time, high, promotion time.Duration) AgentView {
	v := AgentView{ID: agent.ID, Provider: agent.Provider, Elapsed: formatDuration(elapsedDuration(agent.CurrentTurn, now)), CompletionPhase: state.CompletionNone}
	t := agent.CurrentTurn
	if t.Activity == state.ActivityAttention {
		v.Status = state.DisplayAttention
		v.DeliveryTier = "critical"
		return v
	}
	if t.Activity == state.ActivityError || t.Outcome == state.OutcomeFailed {
		v.Status = state.DisplayError
		v.DeliveryTier = "critical"
		return v
	}
	if t.Freshness == state.FreshnessStale && t.Activity != state.ActivityIdle {
		v.Status = state.DisplayStale
		v.DeliveryTier = "active"
		return v
	}
	if t.Outcome == state.OutcomeCompleted && t.CompletedAt != nil {
		v.Status = state.DisplayComplete
		age := now.Sub(*t.CompletedAt)
		if age >= 0 && age < high {
			v.CompletionPhase = state.CompletionHigh
		}
		if age >= 0 && age < promotion {
			if v.CompletionPhase == state.CompletionNone {
				v.CompletionPhase = state.CompletionRecent
			}
			v.DeliveryTier = "promoted"
		} else {
			v.DeliveryTier = "resting"
		}
		return v
	}
	if t.Activity == state.ActivityWorking {
		v.Status = state.DisplayWorking
		v.DeliveryTier = "active"
		return v
	}
	v.Status = state.DisplayIdle
	return v
}

func rotateTake(queue []AgentView, n int, slot int64) []AgentView {
	if n <= 0 || len(queue) == 0 {
		return nil
	}
	if n > len(queue) {
		n = len(queue)
	}
	start := int(slot % int64(len(queue)))
	if start < 0 {
		start += len(queue)
	}
	out := make([]AgentView, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, queue[(start+i)%len(queue)])
	}
	return out
}
func appendUniqueRotated(selected, queue []AgentView, n int, slot int64) []AgentView {
	if n <= 0 {
		return selected
	}
	seen := map[string]bool{}
	for _, v := range selected {
		seen[v.ID] = true
	}
	for _, v := range rotateTake(queue, len(queue), slot) {
		if seen[v.ID] {
			continue
		}
		selected = append(selected, v)
		seen[v.ID] = true
		n--
		if n == 0 {
			break
		}
	}
	return selected
}

func buildQuota(in []state.PublicQuota) ([]QuotaView, bool) {
	out := make([]QuotaView, len(in))
	connected := false
	for i, q := range in {
		v := QuotaView{Provider: q.Provider, Status: string(q.SourceStatus)}
		if q.Windows != nil {
			for _, w := range *q.Windows {
				used := "N/A"
				if w.UsedPercent != nil {
					used = fmt.Sprintf("%.0f%%", *w.UsedPercent)
					connected = true
				}
				v.Windows = append(v.Windows, QuotaWindowView{Name: w.Name, Used: used})
			}
		}
		out[i] = v
	}
	return out, connected
}
func elapsedDuration(turn state.PublicCurrentTurn, now time.Time) time.Duration {
	if turn.StartedAt.IsZero() {
		return 0
	}
	if turn.Outcome == state.OutcomeCompleted && turn.CompletedAt != nil {
		return boundedDuration(turn.StartedAt, *turn.CompletedAt)
	}
	if turn.Activity == state.ActivityError || turn.Outcome == state.OutcomeFailed {
		if !turn.UpdatedAt.IsZero() {
			return boundedDuration(turn.StartedAt, turn.UpdatedAt)
		}
		return 0
	}
	return boundedDuration(turn.StartedAt, now)
}
func boundedDuration(start, end time.Time) time.Duration {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start)
}
func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}
func formatPercent(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", *v)
}
func formatBytes(v *uint64) string {
	if v == nil {
		return "N/A"
	}
	const gib = 1024 * 1024 * 1024
	return fmt.Sprintf("%.1f GiB", float64(*v)/gib)
}
func metricString(m state.PublicMetricSet) string {
	if m.UsedBytes == nil || m.TotalBytes == nil {
		return "N/A"
	}
	return fmt.Sprintf("%s / %s", formatBytes(m.UsedBytes), formatBytes(m.TotalBytes))
}
func quotaRailLabel(connected bool) string {
	if !connected {
		return "QUOTA · NOT CONNECTED"
	}
	return "QUOTA"
}
