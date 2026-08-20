package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProjectPublicSecurityAndReferentialIntegrity(t *testing.T) {
	now := time.Date(2026, 8, 20, 6, 30, 0, 0, time.UTC)
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.Projects[0].WorktreeRoot = "/Users/private/example/project"
	in.NavigationTargets[0].Detail.FocusLocator = "PRIVATE_FOCUS_LOCATOR_SENTINEL"
	in.NavigationTargets[3].Detail.WorktreeRoot = "/Users/private/example/project"
	in.InternalMeta.PrivateNote = "PRIVATE_SECRET_SENTINEL"

	pub := ProjectPublic(in, RuntimeCapabilities{SafeNavigation: false}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	if pub.StateKind != "public" {
		t.Fatalf("stateKind=%q", pub.StateKind)
	}
	if pub.Meta.SafeNavigationEnabled {
		t.Fatal("M1 must report safeNavigationEnabled=false")
	}

	b, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, forbidden := range []string{"worktreeRoot", "focusLocator", "PRIVATE_FOCUS_LOCATOR_SENTINEL", "/Users/private/example/project", "PRIVATE_SECRET_SENTINEL", "privateNote"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public state leaked %q: %s", forbidden, body)
		}
	}

	targets := map[string]PublicNavigationTarget{}
	for _, target := range pub.NavigationTargets {
		targets[target.TargetID] = target
	}
	for _, agent := range pub.Agents {
		if agent.Navigation == nil {
			t.Fatalf("agent %s lost safe target reference", agent.ID)
		}
		if _, ok := targets[agent.Navigation.TargetID]; !ok {
			t.Fatalf("agent target %s absent from navigationTargets", agent.Navigation.TargetID)
		}
	}
	for _, project := range pub.Projects {
		if project.Navigation == nil {
			t.Fatalf("project %s lost safe target reference", project.ProjectID)
		}
		if _, ok := targets[project.Navigation.TargetID]; !ok {
			t.Fatalf("project target %s absent from navigationTargets", project.Navigation.TargetID)
		}
	}
}

func TestProjectPublicOmitsBrokenEntityReference(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.Agents[0].NavigationTargetID = "missing-target"
	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	if pub.Agents[0].Navigation != nil {
		t.Fatal("unsafe/missing target reference should be removed")
	}
}

func TestNullMetricsRemainNull(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	pub := ProjectPublic(MockInternalState(now, HostState{ID: "host", DisplayName: "Host"}), RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	if pub.System.Swap.UsedBytes != nil || pub.System.Swap.PercentUsed != nil {
		t.Fatal("unavailable metrics were converted to values")
	}
	if pub.Quota[0].Windows != nil {
		t.Fatal("unavailable quota windows should remain null")
	}
}

func TestProjectPublicFiltersForeignAndDuplicateTargets(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	duplicate := in.NavigationTargets[0]
	duplicate.AllowedActions = []NavigationAction{ActionFocusAgent}
	foreign := in.NavigationTargets[1]
	foreign.TargetID = "foreign-target"
	foreign.HostID = "other-host"
	in.NavigationTargets = append(in.NavigationTargets, duplicate, foreign)

	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	seen := map[string]bool{}
	for _, target := range pub.NavigationTargets {
		if seen[target.TargetID] {
			t.Fatalf("duplicate public target %s", target.TargetID)
		}
		seen[target.TargetID] = true
		if target.TargetID == "foreign-target" {
			t.Fatal("foreign host target leaked")
		}
	}
}
