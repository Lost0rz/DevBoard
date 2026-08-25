package web

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const quotaBarSegments = 16

type ViewModel struct {
	Mock           bool
	Updated        string
	Clock          string
	Agents         []AgentView
	Alerts         []AlertView
	Sources        []SourceView
	System         SystemView
	Projects       []ProjectView
	Quota          []QuotaView
	QuotaConnected bool
	SafeNavigation bool
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
type SourceView struct {
	Name, Label, Status, Message, LastSuccess, StateClass string
}
type SystemView struct {
	CPU, Memory, Swap, Disk string
	SourceStatus            string
	StateClass              string
	Groups                  []ProcessGroupView
}
type ProcessGroupView struct{ Name, CPU, RAM string }
type ProjectView struct{ Name, Branch, Status string }
type QuotaView struct {
	Provider, Status string
	Windows          []QuotaWindowView
}
type QuotaWindowView struct {
	Name             string
	Used             string
	Remaining        string
	RemainingValue   string
	RemainingPercent int
	StatusClass      string
	Bar              string
	Reset            string
}

func BuildViewModel(pub state.PublicState, now time.Time, mock bool, layout string) ViewModel {
	return buildViewModel(pub, now, mock)
}

func buildViewModel(pub state.PublicState, now time.Time, mock bool) ViewModel {
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

	sources := buildSourceViews(pub.Sources, now)
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
	quota, quotaConnected := buildQuota(pub.Quota, now)
	systemSourceStatus := string(state.SourceUnavailable)
	if source, ok := pub.Sources["system"]; ok {
		systemSourceStatus = string(source.Status)
	}
	system := SystemView{CPU: formatPercent(pub.System.CPUPercent), Memory: metricString(pub.System.Memory), Swap: metricString(pub.System.Swap), Disk: metricString(pub.System.Disk), SourceStatus: strings.ToUpper(systemSourceStatus), StateClass: sourceStateClass(systemSourceStatus), Groups: groups}
	return ViewModel{Mock: mock, Updated: now.UTC().Format("15:04:05 UTC"), Clock: now.Format("15:04"), Agents: agents, Alerts: alerts, Sources: sources, System: system, Projects: projects, Quota: quota, QuotaConnected: quotaConnected, SafeNavigation: pub.Meta.SafeNavigationEnabled}
}

func buildSourceViews(sources map[string]state.PublicSourceHealth, now time.Time) []SourceView {
	labels := map[string]string{
		"codex-hooks":  "Codex",
		"claude-hooks": "Claude Code",
		"system":       "System",
		"network":      "Network",
		"git":          "Project identity",
		"quota":        "Quota",
	}
	order := map[string]int{
		"codex-hooks":  0,
		"claude-hooks": 1,
		"system":       2,
		"network":      3,
		"git":          4,
		"quota":        5,
	}
	out := make([]SourceView, 0, len(sources))
	for id, source := range sources {
		label := labels[id]
		if label == "" {
			label = strings.ReplaceAll(id, "-", " ")
		}
		out = append(out, SourceView{
			Name:        id,
			Label:       label,
			Status:      strings.ToUpper(string(source.Status)),
			Message:     source.Message,
			LastSuccess: formatSourceLastSuccess(source.LastSuccessAt, now),
			StateClass:  sourceStateClass(string(source.Status)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, leftKnown := order[out[i].Name]
		right, rightKnown := order[out[j].Name]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && left != right {
			return left < right
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func sourceStateClass(status string) string {
	switch status {
	case string(state.SourceAvailable):
		return "is-online"
	case string(state.SourceDegraded):
		return "is-stale"
	default:
		return "is-offline"
	}
}

func formatSourceLastSuccess(last *time.Time, now time.Time) string {
	if last == nil {
		return "No successful sample"
	}
	age := now.Sub(*last)
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return fmt.Sprintf("Success %ds ago", int(age/time.Second))
	}
	if age < time.Hour {
		return fmt.Sprintf("Success %dm ago", int(age/time.Minute))
	}
	return fmt.Sprintf("Success %dh ago", int(age/time.Hour))
}

func buildQuota(in []state.PublicQuota, now time.Time) ([]QuotaView, bool) {
	out := make([]QuotaView, len(in))
	connected := false
	for i, q := range in {
		v := QuotaView{Provider: q.Provider, Status: string(q.SourceStatus)}
		if q.Windows != nil {
			for _, w := range *q.Windows {
				if w.UsedPercent == nil {
					continue
				}
				used := *w.UsedPercent
				remaining := clampPercent(100 - used)
				v.Windows = append(v.Windows, QuotaWindowView{
					Name:             w.Name,
					Used:             fmt.Sprintf("%.0f%%", used),
					Remaining:        fmt.Sprintf("%.0f%% LEFT", remaining),
					RemainingValue:   fmt.Sprintf("%.0f%%", remaining),
					RemainingPercent: int(math.Round(remaining)),
					StatusClass:      quotaWindowStatusClass(remaining),
					Bar:              quotaBar(remaining),
					Reset:            quotaReset(w.ResetsAt, now),
				})
				connected = true
			}
		}
		out[i] = v
	}
	return out, connected
}

func quotaWindowStatusClass(remaining float64) string {
	switch {
	case remaining <= 0:
		return "pad-quota-empty"
	case remaining <= 20:
		return "pad-quota-warning"
	default:
		return "pad-quota-healthy"
	}
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func quotaBar(remaining float64) string {
	filled := int(math.Round(clampPercent(remaining) * quotaBarSegments / 100))
	if filled < 0 {
		filled = 0
	}
	if filled > quotaBarSegments {
		filled = quotaBarSegments
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", quotaBarSegments-filled) + "]"
}

func quotaReset(reset *time.Time, now time.Time) string {
	if reset == nil {
		return ""
	}
	d := reset.Sub(now)
	if d <= 0 {
		return "reset due"
	}
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		hours := int((d % (24 * time.Hour)) / time.Hour)
		return fmt.Sprintf("reset %dd%02dh", days, hours)
	}
	if d >= time.Hour {
		hours := int(d / time.Hour)
		mins := int((d % time.Hour) / time.Minute)
		return fmt.Sprintf("reset %dh%02dm", hours, mins)
	}
	mins := int(d / time.Minute)
	if mins == 0 {
		return "reset <1m"
	}
	return fmt.Sprintf("reset %dm", mins)
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
