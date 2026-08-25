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

// TestProjectPublicSanitizesSourceMessage freezes the source-message privacy
// boundary: internal SourceHealth.Message is arbitrary provider/operator text
// (socket paths, credential fragments, credential-bearing URLs) and must never
// cross the public projection. The public message is a bounded, status-derived
// allow-listed value; status and timestamps are unchanged.
func TestProjectPublicSanitizesSourceMessage(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	attempt := now.Add(-5 * time.Second)
	sentinels := []string{
		"/Users/private/project/secret.sock",
		"SUPER_SECRET_SOURCE_TOKEN",
		"https://user:password@example.invalid/private",
	}
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.Sources["codex-hooks"] = SourceHealth{Status: SourceAvailable, LastAttemptAt: &attempt, LastSuccessAt: &attempt, Message: "dial " + sentinels[0] + " failed with " + sentinels[1]}
	in.Sources["system"] = SourceHealth{Status: SourceDegraded, LastAttemptAt: &attempt, Message: "partial read failure at " + sentinels[0]}
	in.Sources["quota"] = SourceHealth{Status: SourceUnavailable, LastAttemptAt: &attempt, Message: sentinels[2] + " quota fetch failed"}

	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	b, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, sentinel := range sentinels {
		if strings.Contains(body, sentinel) {
			t.Fatalf("public state leaked source message sentinel %q", sentinel)
		}
	}

	wantMessages := map[string]string{
		"codex-hooks": "Source available.",
		"system":      "Source degraded.",
		"quota":       "Source unavailable.",
	}
	wantStatuses := map[string]SourceStatus{
		"codex-hooks": SourceAvailable,
		"system":      SourceDegraded,
		"quota":       SourceUnavailable,
	}
	for id, want := range wantMessages {
		source, ok := pub.Sources[id]
		if !ok {
			t.Fatalf("source %q missing from public projection", id)
		}
		if source.Message != want {
			t.Fatalf("public source %q message=%q, want bounded allow-listed %q", id, source.Message, want)
		}
		if source.Status != wantStatuses[id] {
			t.Fatalf("public source %q status=%q, want unchanged %q", id, source.Status, wantStatuses[id])
		}
	}
	if got := pub.Sources["codex-hooks"].LastAttemptAt; got == nil || !got.Equal(attempt) {
		t.Fatalf("public source lastAttemptAt must be preserved, got %v", got)
	}
	if got := pub.Sources["codex-hooks"].LastSuccessAt; got == nil || !got.Equal(attempt) {
		t.Fatalf("public source lastSuccessAt must be preserved, got %v", got)
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

func TestAgentNavigationRequiresTargetOwnership(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	cfg := ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}

	t.Run("agent cannot reference another agent target", func(t *testing.T) {
		in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
		in.Agents[0].NavigationTargetID = in.NavigationTargets[1].TargetID
		pub := ProjectPublic(in, RuntimeCapabilities{}, cfg, now)
		if pub.Agents[0].Navigation != nil {
			t.Fatal("agent A must not inherit agent B navigation target")
		}
	})

	t.Run("provider mismatch", func(t *testing.T) {
		in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
		target := &in.NavigationTargets[0]
		target.Detail.Provider = "claude-code"
		target.Detail.AgentID = target.Detail.Provider + ":" + target.Detail.SessionID
		pub := ProjectPublic(in, RuntimeCapabilities{}, cfg, now)
		if pub.Agents[0].Navigation != nil {
			t.Fatal("provider-mismatched target must not attach to agent")
		}
	})

	t.Run("session mismatch", func(t *testing.T) {
		in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
		target := &in.NavigationTargets[0]
		target.Detail.SessionID = "session-other"
		target.Detail.AgentID = target.Detail.Provider + ":" + target.Detail.SessionID
		pub := ProjectPublic(in, RuntimeCapabilities{}, cfg, now)
		if pub.Agents[0].Navigation != nil {
			t.Fatal("session-mismatched target must not attach to agent")
		}
	})

	t.Run("turn contradiction", func(t *testing.T) {
		in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
		in.NavigationTargets[0].Detail.TurnID = "turn-other"
		pub := ProjectPublic(in, RuntimeCapabilities{}, cfg, now)
		if pub.Agents[0].Navigation != nil {
			t.Fatal("contradictory turn target must not attach to agent")
		}
	})
}

func TestProjectNavigationRequiresTargetOwnership(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.NavigationTargets[3].Detail.ProjectID = "project-other"
	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	if pub.Projects[0].Navigation != nil {
		t.Fatal("mismatched project target must not attach to project")
	}
}

func TestInvalidTargetShapeExcludedFromPublicRegistry(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	cfg := ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}

	cases := []struct {
		name     string
		mutate   func(*InternalRootState)
		targetID string
	}{
		{
			name: "agent missing session",
			mutate: func(in *InternalRootState) {
				in.NavigationTargets[0].Detail.SessionID = ""
			},
			targetID: "target-agent-codex-mock-001",
		},
		{
			name: "agent incoherent canonical id",
			mutate: func(in *InternalRootState) {
				in.NavigationTargets[0].Detail.AgentID = "codex:other-session"
			},
			targetID: "target-agent-codex-mock-001",
		},
		{
			name: "project missing worktree",
			mutate: func(in *InternalRootState) {
				in.NavigationTargets[3].Detail.WorktreeID = ""
			},
			targetID: "target-project-mock-main",
		},
		{
			name: "app missing app ref",
			mutate: func(in *InternalRootState) {
				in.NavigationTargets[4].Detail.AppRef = ""
			},
			targetID: "target-app-terminal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
			tc.mutate(&in)
			pub := ProjectPublic(in, RuntimeCapabilities{}, cfg, now)
			for _, target := range pub.NavigationTargets {
				if target.TargetID == tc.targetID {
					t.Fatalf("invalid target %s was exposed", tc.targetID)
				}
			}
			for _, agent := range pub.Agents {
				if agent.Navigation != nil && agent.Navigation.TargetID == tc.targetID {
					t.Fatalf("invalid target %s remained attached to agent", tc.targetID)
				}
			}
			for _, project := range pub.Projects {
				if project.Navigation != nil && project.Navigation.TargetID == tc.targetID {
					t.Fatalf("invalid target %s remained attached to project", tc.targetID)
				}
			}
		})
	}
}

func TestPublicQuotaWindowIsIndependentProjection(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	reset := now.Add(5 * time.Hour)
	used := 42.5
	windows := []QuotaWindow{{Name: "five-hour", UsedPercent: &used, ResetsAt: &reset}}
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.Quota[0].Windows = &windows

	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, now)
	if pub.Quota[0].Windows == nil || len(*pub.Quota[0].Windows) != 1 {
		t.Fatalf("public quota windows=%v", pub.Quota[0].Windows)
	}
	got := (*pub.Quota[0].Windows)[0]
	if got.Name != "five-hour" || got.UsedPercent == nil || *got.UsedPercent != 42.5 || got.ResetsAt == nil || !got.ResetsAt.Equal(reset) {
		t.Fatalf("unexpected public quota window: %+v", got)
	}

	(*in.Quota[0].Windows)[0].Name = "PRIVATE_INTERNAL_CHANGED"
	*(*in.Quota[0].Windows)[0].UsedPercent = 99
	changedReset := reset.Add(time.Hour)
	(*in.Quota[0].Windows)[0].ResetsAt = &changedReset
	got = (*pub.Quota[0].Windows)[0]
	if got.Name != "five-hour" || *got.UsedPercent != 42.5 || !got.ResetsAt.Equal(reset) {
		t.Fatalf("public quota window aliased internal state: %+v", got)
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["name"] == nil || fields["usedPercent"] == nil || fields["resetsAt"] == nil {
		t.Fatalf("public quota window contract unexpectedly expanded: %s", b)
	}
}

// The recovered-error amendment (2026-08-25) crosses the public projection as
// a derived timestamp only: the supersede fact must reach downstream Pad
// consumers while no error detail beyond the existing bounded fields appears.
func TestProjectPublicCarriesSupersededErrorTimestamp(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	in := MockInternalState(now, HostState{ID: "host", DisplayName: "Host"})
	in.Tasks = append(in.Tasks, TaskState{
		ID: "task-superseded", Provider: "claude-code", SessionID: "s", TurnID: "old",
		Title: "Blocked turn", Lifecycle: TaskError, Freshness: FreshnessFresh,
		Confidence: TaskConfidenceHigh, StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		Attention:    &TaskAttention{Kind: AttentionRateLimited, Text: "Rate limited", At: now.Add(-time.Hour)},
		SupersededAt: ptrTimeForProjector(now.Add(-30 * time.Minute)),
	})
	in.Tasks = append(in.Tasks, TaskState{
		ID: "task-unrecovered", Provider: "claude-code", SessionID: "s2", TurnID: "stuck",
		Title: "Still blocked", Lifecycle: TaskError, Freshness: FreshnessFresh,
		Confidence: TaskConfidenceHigh, StartedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
		Attention: &TaskAttention{Kind: AttentionRateLimited, Text: "Rate limited", At: now.Add(-2 * time.Hour)},
	})

	pub := ProjectPublic(in, RuntimeCapabilities{}, ProjectionConfig{KindleRefreshSeconds: 20}, now)
	byID := map[string]PublicTask{}
	for _, task := range pub.Tasks {
		byID[task.ID] = task
	}
	superseded := byID["task-superseded"]
	if superseded.SupersededAt == nil || !superseded.SupersededAt.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("superseded timestamp did not cross the projection: %+v", superseded)
	}
	if superseded.Lifecycle != TaskError {
		t.Fatalf("superseded error lifecycle must stay error publicly: %q", superseded.Lifecycle)
	}
	unrecovered := byID["task-unrecovered"]
	if unrecovered.SupersededAt != nil {
		t.Fatalf("unrecovered error must not carry a supersede timestamp: %+v", unrecovered)
	}
}

func ptrTimeForProjector(v time.Time) *time.Time { return &v }
