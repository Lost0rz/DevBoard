package state

import "testing"

func TestFrozenEnumValues(t *testing.T) {
	checks := map[string]string{
		"activity idle": string(ActivityIdle), "activity working": string(ActivityWorking), "activity attention": string(ActivityAttention), "activity error": string(ActivityError),
		"outcome none": string(OutcomeNone), "outcome completed": string(OutcomeCompleted), "outcome failed": string(OutcomeFailed),
		"fresh": string(FreshnessFresh), "stale": string(FreshnessStale),
		"available": string(SourceAvailable), "degraded": string(SourceDegraded), "unavailable": string(SourceUnavailable),
		"agent": string(NavigationAgent), "project": string(NavigationProject), "app": string(NavigationApp),
		"focus_app": string(ActionFocusApp), "focus_agent": string(ActionFocusAgent), "focus_project": string(ActionFocusProject), "open_project": string(ActionOpenProject),
	}
	for name, got := range checks {
		if got == "" {
			t.Fatalf("%s empty", name)
		}
	}
}
