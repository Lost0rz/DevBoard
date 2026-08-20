package web

import (
	"fmt"
	"sort"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type ViewModel struct {
	Mock           bool
	Layout         string
	Updated        string
	KindleRefresh  int
	Agents         []AgentView
	System         SystemView
	Projects       []ProjectView
	Quota          []QuotaView
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
}

type SystemView struct {
	CPU    string
	Memory string
	Swap   string
	Disk   string
	Groups []ProcessGroupView
}

type ProcessGroupView struct {
	Name string
	CPU  string
	RAM  string
}

type ProjectView struct {
	Name   string
	Branch string
	Status string
}

type QuotaView struct {
	Provider string
	Status   string
}

func BuildViewModel(pub state.PublicState, now time.Time, mock bool, layout string) ViewModel {
	high := time.Duration(pub.Meta.CompleteHighVisibilitySeconds) * time.Second
	retention := time.Duration(pub.Meta.CompleteRetentionSeconds) * time.Second
	agents := make([]AgentView, 0, len(pub.Agents))
	for _, agent := range pub.Agents {
		turn := state.CurrentTurn{
			TurnID: agent.CurrentTurn.TurnID, Activity: agent.CurrentTurn.Activity, Outcome: agent.CurrentTurn.Outcome,
			Freshness: agent.CurrentTurn.Freshness, StartedAt: agent.CurrentTurn.StartedAt, CompletedAt: agent.CurrentTurn.CompletedAt, UpdatedAt: agent.CurrentTurn.UpdatedAt,
		}
		derived := state.DeriveDisplay(turn, now, high, retention)
		elapsed := now.Sub(turn.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		agents = append(agents, AgentView{
			ID: agent.ID, Provider: agent.Provider, Status: derived.Status, CompletionPhase: derived.CompletionPhase,
			Priority: derived.Priority, Elapsed: formatDuration(elapsed), Attention: derived.Status == state.DisplayAttention,
		})
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Priority == agents[j].Priority {
			return agents[i].ID < agents[j].ID
		}
		return agents[i].Priority < agents[j].Priority
	})

	groups := make([]ProcessGroupView, len(pub.System.ProcessGroups))
	for i, group := range pub.System.ProcessGroups {
		groups[i] = ProcessGroupView{Name: group.Name, CPU: formatPercent(group.CPUPercent), RAM: formatBytes(group.ResidentMemoryBytes)}
	}
	projects := make([]ProjectView, len(pub.Projects))
	for i, p := range pub.Projects {
		status := "CLEAN"
		if p.Dirty {
			status = fmt.Sprintf("DIRTY · %d modified · %d untracked", p.ModifiedCount, p.UntrackedCount)
		}
		projects[i] = ProjectView{Name: p.DisplayName, Branch: p.Branch, Status: status}
	}
	quota := make([]QuotaView, len(pub.Quota))
	for i, q := range pub.Quota {
		quota[i] = QuotaView{Provider: q.Provider, Status: string(q.SourceStatus)}
	}

	return ViewModel{
		Mock: mock, Layout: layout, Updated: now.UTC().Format("15:04:05 UTC"), KindleRefresh: pub.Meta.KindleRefreshSeconds,
		Agents:   agents,
		System:   SystemView{CPU: formatPercent(pub.System.CPUPercent), Memory: metricString(pub.System.Memory), Swap: metricString(pub.System.Swap), Disk: metricString(pub.System.Disk), Groups: groups},
		Projects: projects, Quota: quota, SafeNavigation: pub.Meta.SafeNavigationEnabled,
	}
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
