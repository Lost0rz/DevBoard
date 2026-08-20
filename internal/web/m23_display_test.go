package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func m23Now() time.Time { return time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC) }
func m23Meta() state.DisplayMeta {
	return state.DisplayMeta{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}
}
func m23Working(id, provider string) state.PublicAgent {
	now := m23Now()
	return state.PublicAgent{ID: id, Provider: provider, SessionID: id, CurrentTurn: state.PublicCurrentTurn{TurnID: "t", Activity: state.ActivityWorking, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: now.Add(-5 * time.Minute), UpdatedAt: now}}
}
func m23Stale(id, provider string) state.PublicAgent {
	a := m23Working(id, provider)
	a.CurrentTurn.Freshness = state.FreshnessStale
	return a
}
func m23Attention(id, provider string) state.PublicAgent {
	a := m23Working(id, provider)
	a.CurrentTurn.Activity = state.ActivityAttention
	return a
}
func m23Error(id, provider string) state.PublicAgent {
	a := m23Working(id, provider)
	a.CurrentTurn.Activity = state.ActivityError
	a.CurrentTurn.Outcome = state.OutcomeFailed
	return a
}
func m23Complete(id, provider string, age time.Duration) state.PublicAgent {
	now := m23Now()
	done := now.Add(-age)
	return state.PublicAgent{ID: id, Provider: provider, SessionID: id, CurrentTurn: state.PublicCurrentTurn{TurnID: "t", Activity: state.ActivityIdle, Outcome: state.OutcomeCompleted, Freshness: state.FreshnessFresh, StartedAt: done.Add(-10 * time.Minute), CompletedAt: &done, UpdatedAt: done}}
}
func m23Pub(agents ...state.PublicAgent) state.PublicState {
	return state.PublicState{Meta: m23Meta(), Agents: agents, Sources: map[string]state.PublicSourceHealth{}}
}
func ids(in []AgentView) []string {
	out := make([]string, len(in))
	for i, a := range in {
		out[i] = a.ID
	}
	return out
}
func containsID(in []AgentView, id string) bool {
	for _, a := range in {
		if a.ID == id {
			return true
		}
	}
	return false
}
func countStatus(in []AgentView, s state.DisplayStatus) int {
	n := 0
	for _, a := range in {
		if a.Status == s {
			n++
		}
	}
	return n
}

func TestKindleLandscapeDynamicCapacityAndSharedProviderQueue(t *testing.T) {
	cases := []struct {
		name   string
		agents []state.PublicAgent
		want   int
	}{
		{"zero", nil, 0},
		{"one", []state.PublicAgent{m23Working("a", "codex")}, 1},
		{"two", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code")}, 2},
		{"three", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex")}, 3},
		{"four", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex"), m23Working("d", "claude-code")}, 3},
		{"three claude", []state.PublicAgent{m23Working("a", "claude-code"), m23Working("b", "claude-code"), m23Working("c", "claude-code")}, 3},
		{"three codex", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "codex"), m23Working("c", "codex")}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildKindleViewModel(m23Pub(tc.agents...), m23Now(), false, "landscape", "none")
			if len(vm.KindleAgents) != tc.want {
				t.Fatalf("agents=%v got=%d want=%d", ids(vm.KindleAgents), len(vm.KindleAgents), tc.want)
			}
		})
	}
}

func TestKindlePortraitDynamicCapacity(t *testing.T) {
	for n, want := range map[int]int{1: 1, 2: 2, 3: 2} {
		agents := make([]state.PublicAgent, 0, n)
		for i := 0; i < n; i++ {
			agents = append(agents, m23Working(string(rune('a'+i)), "codex"))
		}
		vm := BuildKindleViewModel(m23Pub(agents...), m23Now(), false, "portrait", "none")
		if len(vm.KindleAgents) != want {
			t.Fatalf("n=%d got=%d want=%d", n, len(vm.KindleAgents), want)
		}
	}
}

func TestKindlePriorityAndCompleteCompetition(t *testing.T) {
	now := m23Now()
	t.Run("critical before normal", func(t *testing.T) {
		pub := m23Pub(m23Working("w", "codex"), m23Complete("c", "codex", 5*time.Minute), m23Error("e", "claude-code"), m23Attention("a", "claude-code"))
		vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
		if !containsID(vm.KindleAgents, "a") || !containsID(vm.KindleAgents, "e") {
			t.Fatalf("critical missing: %v", ids(vm.KindleAgents))
		}
	})
	t.Run("completed remain without active pressure", func(t *testing.T) {
		pub := m23Pub(m23Complete("a", "codex", 2*time.Hour), m23Complete("b", "claude-code", 3*time.Hour), m23Complete("c", "codex", 4*time.Hour))
		vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
		if len(vm.KindleAgents) != 3 || countStatus(vm.KindleAgents, state.DisplayComplete) != 3 {
			t.Fatalf("old completed disappeared: %+v", vm.KindleAgents)
		}
	})
	t.Run("old complete behind working pressure", func(t *testing.T) {
		pub := m23Pub(m23Complete("old", "codex", 2*time.Hour), m23Working("b", "codex"), m23Working("c", "claude-code"), m23Working("d", "codex"))
		vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
		if containsID(vm.KindleAgents, "old") || countStatus(vm.KindleAgents, state.DisplayWorking) != 3 {
			t.Fatalf("unexpected foreground: %v", ids(vm.KindleAgents))
		}
	})
	t.Run("recent complete reserves one delivery slot", func(t *testing.T) {
		pub := m23Pub(m23Complete("delivery", "codex", 5*time.Minute), m23Working("b", "codex"), m23Working("c", "claude-code"), m23Working("d", "codex"))
		vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
		if countStatus(vm.KindleAgents, state.DisplayComplete) != 1 || countStatus(vm.KindleAgents, state.DisplayWorking) != 2 {
			t.Fatalf("delivery competition wrong: %+v", vm.KindleAgents)
		}
	})
	t.Run("stale competes as active", func(t *testing.T) {
		pub := m23Pub(m23Complete("old", "codex", 2*time.Hour), m23Stale("s", "codex"), m23Working("w1", "codex"), m23Working("w2", "claude-code"))
		vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
		if containsID(vm.KindleAgents, "old") || !containsID(vm.KindleAgents, "s") {
			t.Fatalf("stale active competition wrong: %v", ids(vm.KindleAgents))
		}
	})
}

func TestKindleRotationDeterministicFairAndNoStarvation(t *testing.T) {
	pub := m23Pub(m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex"), m23Working("d", "claude-code"))
	base := m23Now()
	pub.Meta = m23Meta()
	one := BuildKindleViewModel(pub, base, false, "landscape", "none")
	again := BuildKindleViewModel(pub, base.Add(10*time.Second), false, "landscape", "none")
	if strings.Join(ids(one.KindleAgents), ",") != strings.Join(ids(again.KindleAgents), ",") {
		t.Fatalf("same rotation slot changed: %v vs %v", ids(one.KindleAgents), ids(again.KindleAgents))
	}
	next := BuildKindleViewModel(pub, base.Add(20*time.Second), false, "landscape", "none")
	if strings.Join(ids(one.KindleAgents), ",") == strings.Join(ids(next.KindleAgents), ",") {
		t.Fatalf("next slot did not rotate: %v", ids(next.KindleAgents))
	}
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		vm := BuildKindleViewModel(pub, base.Add(time.Duration(i*20)*time.Second), false, "landscape", "none")
		for _, a := range vm.KindleAgents {
			seen[a.ID] = true
		}
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if !seen[id] {
			t.Fatalf("stable queue starved %s", id)
		}
	}
}

func TestKindleDeliveryAndActiveQueuesRotateFairly(t *testing.T) {
	pub := m23Pub(m23Complete("ca", "codex", 5*time.Minute), m23Complete("cb", "claude-code", 6*time.Minute), m23Complete("cc", "codex", 7*time.Minute), m23Working("wa", "codex"), m23Working("wb", "claude-code"), m23Working("wc", "codex"))
	base := m23Now()
	seenDelivery := map[string]bool{}
	seenActive := map[string]bool{}
	for i := 0; i < 6; i++ {
		vm := BuildKindleViewModel(pub, base.Add(time.Duration(i*20)*time.Second), false, "landscape", "none")
		if countStatus(vm.KindleAgents, state.DisplayComplete) != 1 {
			t.Fatalf("slot %d delivery count %+v", i, vm.KindleAgents)
		}
		for _, a := range vm.KindleAgents {
			if a.Status == state.DisplayComplete {
				seenDelivery[a.ID] = true
			} else if a.Status == state.DisplayWorking {
				seenActive[a.ID] = true
			}
		}
	}
	for _, id := range []string{"ca", "cb", "cc"} {
		if !seenDelivery[id] {
			t.Fatalf("delivery queue starved %s", id)
		}
	}
	for _, id := range []string{"wa", "wb", "wc"} {
		if !seenActive[id] {
			t.Fatalf("active queue starved %s", id)
		}
	}
}

func TestKindleQueryNormalization(t *testing.T) {
	for in, want := range map[string]string{"portrait": "portrait", "landscape": "landscape", "bad": "landscape", "": "landscape"} {
		if got := normalizeKindleLayout(in); got != want {
			t.Fatalf("layout %q -> %q want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"none": "none", "left": "left", "right": "right", "PRIVATE_ROTATE_SENTINEL": "none", "": "none"} {
		if got := normalizeKindleRotate(in); got != want {
			t.Fatalf("rotate %q -> %q want %q", in, got, want)
		}
	}
}

func m23Server(t *testing.T, internal state.InternalRootState, now time.Time) *Server {
	t.Helper()
	s, err := NewServer(state.NewStore(internal), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	return s
}
func m23Request(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestKindleOperationalSurfaceOmitsDiagnosticsAndPrivateData(t *testing.T) {
	now := m23Now()
	internal := state.LiveInitialState(now, state.HostState{ID: "h", DisplayName: "Host"})
	internal.Agents = []state.AgentState{{ID: "codex:opaque-session", Provider: "codex", SessionID: "PRIVATE_SESSION_SENTINEL", CurrentTurn: state.CurrentTurn{TurnID: "PRIVATE_TURN_SENTINEL", Activity: state.ActivityWorking, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: now.Add(-time.Minute), UpdatedAt: now}}}
	internal.Projects = []state.ProjectState{{ProjectID: "p", DisplayName: "PRIVATE_PROJECT_SENTINEL", WorktreeRoot: "/Users/private/PRIVATE_CWD_SENTINEL", WorktreeID: "w"}}
	internal.Sources["codex-hooks"] = state.SourceHealth{Status: state.SourceDegraded, Message: "PRIVATE_HOOK_MESSAGE_SENTINEL"}
	s := m23Server(t, internal, now)
	w := m23Request(t, s, "/display/kindle?layout=landscape&rotate=PRIVATE_ROTATE_SENTINEL")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	for _, forbidden := range []string{"HOOK SOURCES", "PRIVATE_HOOK_MESSAGE_SENTINEL", "PRIVATE_PROJECT_SENTINEL", "PRIVATE_CWD_SENTINEL", "PRIVATE_SESSION_SENTINEL", "PRIVATE_TURN_SENTINEL", "PRIVATE_ROTATE_SENTINEL", "/Users/private", "navigationTarget", "focusLocator", "prompt", "transcript", "tool_input"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("kindle leaked %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{"WORKING", "codex", "SYSTEM · NOT CONNECTED", "QUOTA · NOT CONNECTED", "rotate-none", "layout-landscape"} {
		if !strings.Contains(body, required) {
			t.Fatalf("kindle missing %q: %s", required, body)
		}
	}
}

func TestKindleRotationClassesAndCompatibility(t *testing.T) {
	now := m23Now()
	s := m23Server(t, state.LiveInitialState(now, state.HostState{ID: "h"}), now)
	for rotate := range map[string]bool{"none": true, "left": true, "right": true} {
		body := strings.ToLower(m23Request(t, s, "/display/kindle?layout=landscape&rotate="+rotate).Body.String())
		if !strings.Contains(body, "rotate-"+rotate) {
			t.Fatalf("missing rotate class %s", rotate)
		}
		for _, forbidden := range []string{"<script", "fetch(", "websocket", "eventsource", "display:grid", "<canvas", "<svg"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("forbidden %q", forbidden)
			}
		}
		for _, required := range []string{"http-equiv=\"refresh\"", "-webkit-transform", "transform"} {
			if !strings.Contains(body, required) {
				t.Fatalf("missing %q", required)
			}
		}
	}
}

func TestDisplayRetainsDiagnosticsWithAgentSystemQuotaHierarchy(t *testing.T) {
	now := m23Now()
	internal := state.LiveInitialState(now, state.HostState{ID: "h"})
	internal.Agents = []state.AgentState{{ID: "codex:s", Provider: "codex", SessionID: "s", CurrentTurn: state.CurrentTurn{TurnID: "t", Activity: state.ActivityWorking, Freshness: state.FreshnessFresh, StartedAt: now, UpdatedAt: now}}}
	internal.Projects = []state.ProjectState{{ProjectID: "p", DisplayName: "Project Visible On Display", WorktreeID: "w"}}
	body := m23Request(t, m23Server(t, internal, now), "/display").Body.String()
	for _, required := range []string{"AGENTS", "SYSTEM", "QUOTA", "HOOK SOURCES", "PROJECTS", "Project Visible On Display"} {
		if !strings.Contains(body, required) {
			t.Fatalf("display missing %q", required)
		}
	}
	if strings.Index(body, "AGENTS") > strings.Index(body, "SYSTEM") || strings.Index(body, "SYSTEM") > strings.Index(body, "QUOTA") {
		t.Fatalf("primary hierarchy not Agent/System/Quota")
	}
}

func TestKindleCriticalPressureDoesNotStarveDeliveryOrActive(t *testing.T) {
	pub := m23Pub(
		m23Attention("critical-a", "claude-code"),
		m23Error("critical-b", "codex"),
		m23Complete("delivery", "codex", 5*time.Minute),
		m23Working("active", "claude-code"),
	)
	base := m23Now()
	seenDelivery := false
	seenActive := false
	for i := 0; i < 4; i++ {
		vm := BuildKindleViewModel(pub, base.Add(time.Duration(i*20)*time.Second), false, "landscape", "none")
		if len(vm.KindleAgents) != 3 {
			t.Fatalf("slot %d len=%d", i, len(vm.KindleAgents))
		}
		seenDelivery = seenDelivery || containsID(vm.KindleAgents, "delivery")
		seenActive = seenActive || containsID(vm.KindleAgents, "active")
	}
	if !seenDelivery || !seenActive {
		t.Fatalf("last slot starvation delivery=%v active=%v", seenDelivery, seenActive)
	}
}

func TestKindleCompletedFillCapacityAfterActiveQueueExhausted(t *testing.T) {
	pub := m23Pub(
		m23Complete("delivery-a", "codex", 5*time.Minute),
		m23Complete("delivery-b", "claude-code", 6*time.Minute),
		m23Working("active", "codex"),
	)
	vm := BuildKindleViewModel(pub, m23Now(), false, "landscape", "none")
	if len(vm.KindleAgents) != 3 || countStatus(vm.KindleAgents, state.DisplayWorking) != 1 || countStatus(vm.KindleAgents, state.DisplayComplete) != 2 {
		t.Fatalf("available completed tasks did not fill deck: %+v", vm.KindleAgents)
	}
}

func TestKindleCriticalPressureRotatesWithinDeliveryAndActiveQueues(t *testing.T) {
	pub := m23Pub(
		m23Attention("critical-a", "claude-code"),
		m23Error("critical-b", "codex"),
		m23Complete("delivery-a", "codex", 5*time.Minute),
		m23Complete("delivery-b", "claude-code", 6*time.Minute),
		m23Working("active-a", "codex"),
		m23Working("active-b", "claude-code"),
	)
	base := m23Now()
	seenDelivery := map[string]bool{}
	seenActive := map[string]bool{}
	for i := 0; i < 8; i++ {
		vm := BuildKindleViewModel(pub, base.Add(time.Duration(i*20)*time.Second), false, "landscape", "none")
		for _, a := range vm.KindleAgents {
			switch a.DeliveryTier {
			case "promoted":
				seenDelivery[a.ID] = true
			case "active":
				seenActive[a.ID] = true
			}
		}
	}
	for _, id := range []string{"delivery-a", "delivery-b"} {
		if !seenDelivery[id] {
			t.Fatalf("critical pressure starved delivery %s", id)
		}
	}
	for _, id := range []string{"active-a", "active-b"} {
		if !seenActive[id] {
			t.Fatalf("critical pressure starved active %s", id)
		}
	}
}
