package state

import "time"

// AcknowledgeTask marks one terminal task as observed after a display action.
// The task and target IDs are both required so a stale display form cannot
// acknowledge a different task in the same provider session.
func AcknowledgeTask(root *InternalRootState, taskID, targetID string, at time.Time) bool {
	if root == nil || taskID == "" || targetID == "" || at.IsZero() {
		return false
	}
	if !navigationTargetHasTask(root, targetID, taskID) {
		return false
	}
	for i := range root.Tasks {
		task := &root.Tasks[i]
		if task.ID != taskID || (task.Lifecycle != TaskComplete && task.Lifecycle != TaskError) {
			continue
		}
		if task.ReadAt == nil {
			readAt := at.UTC()
			task.ReadAt = &readAt
		}
		return true
	}
	return false
}

func navigationTargetHasTask(root *InternalRootState, targetID, taskID string) bool {
	var target *NavigationTarget
	for i := range root.NavigationTargets {
		if root.NavigationTargets[i].TargetID == targetID {
			target = &root.NavigationTargets[i]
			break
		}
	}
	if target == nil || target.Kind != NavigationAgent || !containsAction(target.AllowedActions, ActionFocusAgent) {
		return false
	}
	for _, task := range root.Tasks {
		if task.ID == taskID && taskTargetMatches(task, *target) {
			return true
		}
	}
	return false
}
