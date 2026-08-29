package web

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type TaskView struct {
	Identity           string
	ScopedKey          string
	ProviderProject    string
	Provider           string
	Project            string
	Worktree           string
	Branch             string
	HasProject         bool
	Title              string
	Lifecycle          string
	StateClass         string
	Freshness          string
	Confidence         string
	Elapsed            string
	Checkpoint         string
	Attention          string
	NeedsAttention     bool
	Completion         string
	Result             string
	HasCompletion      bool
	Navigable          bool
	NavigationTargetID string
	NavigationAction   string
	Unread             bool
	Priority           int
}

func buildTaskViews(tasks []state.PublicTask, now time.Time) []TaskView {
	out := make([]TaskView, 0, len(tasks))
	for _, task := range tasks {
		provider := strings.ToUpper(task.Provider)
		if provider == "" {
			provider = "PROVIDER UNAVAILABLE"
		}
		project := "PROJECT UNKNOWN"
		projectName := ""
		worktree := ""
		branch := ""
		if task.Project != nil && task.Project.ProjectName != "" {
			projectName = task.Project.ProjectName
			worktree = task.Project.WorktreeLabel
			branch = task.Project.Branch
			project = projectName
			if worktree != "" {
				project += " · " + worktree
			}
			if branch != "" {
				project += " / " + branch
			}
		}
		lifecycle := strings.ToUpper(string(task.Lifecycle))
		if task.Freshness == state.FreshnessStale {
			lifecycle += " · STALE"
		}
		title := task.Title
		if strings.TrimSpace(title) == "" {
			title = "Task title unavailable"
		}
		v := TaskView{
			Identity:        task.ID,
			ProviderProject: provider + " · " + project,
			Provider:        provider,
			Project:         projectName,
			Worktree:        worktree,
			Branch:          branch,
			HasProject:      projectName != "",
			Title:           title,
			Lifecycle:       lifecycle,
			StateClass:      taskStateClass(task),
			Freshness:       strings.ToUpper(string(task.Freshness)),
			Confidence:      strings.ToUpper(string(task.Confidence)),
			Elapsed:         formatTaskElapsed(task, now),
			NeedsAttention:  task.Lifecycle == state.TaskLifecycleAttention || task.Attention != nil,
			Unread:          task.Unread,
			Priority:        taskDisplayPriority(task),
		}
		if action, ok := taskNavigation(task.Navigation); ok {
			v.Navigable = true
			v.NavigationTargetID = task.Navigation.TargetID
			v.NavigationAction = string(action)
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
		if v.NeedsAttention && strings.TrimSpace(v.Attention) == "" {
			v.Attention = "Action details unavailable."
		}
		if task.Completion != nil {
			v.HasCompletion = true
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

func taskNavigation(target *state.PublicNavigationTarget) (state.NavigationAction, bool) {
	if target == nil || target.TargetID == "" {
		return "", false
	}
	for _, action := range target.AllowedActions {
		if action == state.ActionFocusAgent {
			return action, true
		}
	}
	return "", false
}

func taskStateClass(task state.PublicTask) string {
	if task.Lifecycle == state.TaskError {
		return "is-error"
	}
	if task.Lifecycle == state.TaskLifecycleAttention || task.Attention != nil {
		return "is-attention"
	}
	if task.Freshness == state.FreshnessStale {
		return "is-stale"
	}
	if task.Lifecycle == state.TaskWorking {
		return "is-working"
	}
	if task.Lifecycle == state.TaskComplete {
		return "is-complete"
	}
	return "is-unknown"
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
