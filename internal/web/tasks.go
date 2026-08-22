package web

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type TaskView struct {
	ProviderProject string
	Title           string
	Lifecycle       string
	Elapsed         string
	Checkpoint      string
	Attention       string
	Completion      string
	Result          string
	Priority        int
}

func buildTaskViews(tasks []state.PublicTask, now time.Time) []TaskView {
	out := make([]TaskView, 0, len(tasks))
	for _, task := range tasks {
		provider := strings.ToUpper(task.Provider)
		project := "PROJECT UNKNOWN"
		if task.Project != nil && task.Project.ProjectName != "" {
			project = task.Project.ProjectName
			if task.Project.WorktreeLabel != "" {
				project += " · " + task.Project.WorktreeLabel
			}
			if task.Project.Branch != "" {
				project += " / " + task.Project.Branch
			}
		}
		lifecycle := strings.ToUpper(string(task.Lifecycle))
		if task.Freshness == state.FreshnessStale {
			lifecycle += " · STALE"
		}
		v := TaskView{
			ProviderProject: provider + " · " + project,
			Title:           task.Title,
			Lifecycle:       lifecycle,
			Elapsed:         formatTaskElapsed(task, now),
			Priority:        taskDisplayPriority(task),
		}
		if task.Checkpoint != nil {
			v.Checkpoint = task.Checkpoint.Text
			if v.Checkpoint == "" {
				v.Checkpoint = strings.ReplaceAll(strings.ToUpper(string(task.Checkpoint.Kind)), "_", " ")
			}
		}
		if task.Attention != nil {
			v.Attention = task.Attention.Text
		}
		if task.Completion != nil {
			if task.Completion.Summary != nil {
				v.Completion = *task.Completion.Summary
			}
			if task.Completion.ResultIdentifier != nil {
				v.Result = *task.Completion.ResultIdentifier
			}
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].ProviderProject != out[j].ProviderProject {
			return out[i].ProviderProject < out[j].ProviderProject
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func taskDisplayPriority(task state.PublicTask) int {
	if task.Lifecycle == state.TaskError || task.Lifecycle == state.TaskLifecycleAttention || task.Attention != nil {
		return 0
	}
	if task.Freshness == state.FreshnessStale {
		return 2
	}
	if task.Lifecycle == state.TaskWorking {
		return 1
	}
	if task.Lifecycle == state.TaskComplete {
		return 3
	}
	return 4
}

func formatTaskElapsed(task state.PublicTask, now time.Time) string {
	end := now
	if task.Lifecycle == state.TaskComplete || task.Lifecycle == state.TaskError {
		end = task.UpdatedAt
	}
	if task.StartedAt.IsZero() || end.Before(task.StartedAt) {
		return "0m"
	}
	d := end.Sub(task.StartedAt)
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%dh%02dm", h, m)
}
