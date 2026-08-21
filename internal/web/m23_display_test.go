package web

import (
	"bytes"
	"io"
	"log/slog"
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
func m23IDs(in []AgentView) []string {
	out := make([]string, len(in))
	for i, a := range in {
		out[i] = a.ID
	}
	return out
}
func m23Contains(in []AgentView, id string) bool {
	for _, a := range in {
		if a.ID == id {
			return true
		}
	}
	return false
}
func m23Count(in []AgentView, s state.DisplayStatus) int {
	n := 0
	for _, a := range in {
		if a.Status == s {
			n++
		}
	}
	return n
}
func m23RenderVM(t *testing.T, vm ViewModel) string {
	t.Helper()
	s, err := NewServer(state.NewStore(state.LiveInitialState(m23Now(), state.HostState{ID: "h"})), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, "kindle.html", vm); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func ptrFloat(v float64) *float64    { return &v }
func ptrTime(v time.Time) *time.Time { return &v }

func TestAcceptedKindleCapacityAndSharedProviderQueue(t *testing.T) {
	cases := []struct {
		name, layout string
		agents       []state.PublicAgent
		want         int
	}{
		{"landscape one", "landscape", []state.PublicAgent{m23Working("a", "codex")}, 1},
		{"landscape two", "landscape", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code")}, 2},
		{"landscape three", "landscape", []state.PublicAgent{m23Working("a", "claude-code"), m23Working("b", "claude-code"), m23Working("c", "claude-code")}, 3},
		{"landscape four caps", "landscape", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex"), m23Working("d", "claude-code")}, 3},
		{"portrait one", "portrait", []state.PublicAgent{m23Working("a", "codex")}, 1},
		{"portrait two", "portrait", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code")}, 2},
		{"portrait three caps", "portrait", []state.PublicAgent{m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex")}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildKindleViewModel(m23Pub(tc.agents...), m23Now(), false, tc.layout, "none")
			if len(vm.KindleAgents) != tc.want {
				t.Fatalf("got=%v want=%d", m23IDs(vm.KindleAgents), tc.want)
			}
		})
	}
}
func TestAcceptedKindleCompleteAndActivePressureSemantics(t *testing.T) {
	t.Run("old complete remains without pressure", func(t *testing.T) {
		vm := BuildKindleViewModel(m23Pub(m23Complete("a", "codex", 2*time.Hour), m23Complete("b", "claude-code", 3*time.Hour), m23Complete("c", "codex", 4*time.Hour)), m23Now(), false, "landscape", "none")
		if m23Count(vm.KindleAgents, state.DisplayComplete) != 3 {
			t.Fatalf("%+v", vm.KindleAgents)
		}
	})
	t.Run("old complete yields to active", func(t *testing.T) {
		vm := BuildKindleViewModel(m23Pub(m23Complete("old", "codex", 2*time.Hour), m23Working("a", "codex"), m23Stale("b", "claude-code"), m23Working("c", "codex")), m23Now(), false, "landscape", "none")
		if m23Contains(vm.KindleAgents, "old") {
			t.Fatalf("old complete retained under pressure: %v", m23IDs(vm.KindleAgents))
		}
	})
	t.Run("recent complete reserves delivery slot", func(t *testing.T) {
		vm := BuildKindleViewModel(m23Pub(m23Complete("delivery", "codex", 5*time.Minute), m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex")), m23Now(), false, "landscape", "none")
		if m23Count(vm.KindleAgents, state.DisplayComplete) != 1 || m23Count(vm.KindleAgents, state.DisplayWorking) != 2 {
			t.Fatalf("%+v", vm.KindleAgents)
		}
	})
}
func TestAcceptedKindleCriticalAndRotationFairness(t *testing.T) {
	pub := m23Pub(m23Attention("critical-a", "claude-code"), m23Error("critical-b", "codex"), m23Complete("delivery-a", "codex", 5*time.Minute), m23Complete("delivery-b", "claude-code", 6*time.Minute), m23Working("active-a", "codex"), m23Working("active-b", "claude-code"))
	seenD := map[string]bool{}
	seenA := map[string]bool{}
	for i := 0; i < 8; i++ {
		vm := BuildKindleViewModel(pub, m23Now().Add(time.Duration(i*20)*time.Second), false, "landscape", "none")
		if !m23Contains(vm.KindleAgents, "critical-a") || !m23Contains(vm.KindleAgents, "critical-b") {
			t.Fatalf("critical missing: %v", m23IDs(vm.KindleAgents))
		}
		for _, a := range vm.KindleAgents {
			if a.DeliveryTier == "promoted" {
				seenD[a.ID] = true
			}
			if a.DeliveryTier == "active" {
				seenA[a.ID] = true
			}
		}
	}
	for _, id := range []string{"delivery-a", "delivery-b"} {
		if !seenD[id] {
			t.Fatalf("delivery starved %s", id)
		}
	}
	for _, id := range []string{"active-a", "active-b"} {
		if !seenA[id] {
			t.Fatalf("active starved %s", id)
		}
	}
}
func TestKindleQueryNormalization(t *testing.T) {
	for in, want := range map[string]string{"portrait": "portrait", "landscape": "landscape", "bad": "landscape", "": "landscape"} {
		if got := normalizeKindleLayout(in); got != want {
			t.Fatalf("layout %q=%q", in, got)
		}
	}
	for in, want := range map[string]string{"none": "none", "left": "left", "right": "right", "PRIVATE": "none", "": "none"} {
		if got := normalizeKindleRotate(in); got != want {
			t.Fatalf("rotate %q=%q", in, got)
		}
	}
}

func TestPhysicalRotationCanvasGeometry(t *testing.T) {
	body := strings.ToLower(m23RenderVM(t, BuildKindleViewModel(m23Pub(m23Working("a", "codex")), m23Now(), false, "landscape", "left")))
	for _, want := range []string{"class=\"viewport-shell\"", "class=\"rotation-canvas\"", ".rotate-left .rotation-canvas{width:100vh;height:100vw;left:0;top:100%;", ".rotate-right .rotation-canvas{width:100vh;height:100vw;left:100%;top:0;", "-webkit-transform-origin:0 0", "transform-origin:0 0", "overflow:hidden", "rotate(-90deg)", "rotate(90deg)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rotation geometry missing %q", want)
		}
	}
}
func TestKindleDeckOwnsPrimaryScreenAreaAndNoLargeHeader(t *testing.T) {
	body := m23RenderVM(t, BuildKindleViewModel(m23Pub(m23Working("a", "codex"), m23Working("b", "claude-code"), m23Working("c", "codex")), m23Now(), false, "landscape", "none"))
	for _, want := range []string{"class=\"deck agent-count-3\"", "height:62%", "class=\"agent-table agent-count-3\"", "height:100%", "class=\"system-bar\"", "class=\"quota-rail\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("deck geometry missing %q", want)
		}
	}
	if strings.Contains(body, "class=\"header\"") || strings.Contains(body, "class=\"title\">DEVBOARD") {
		t.Fatalf("large header remains: %s", body)
	}
}
func TestKindleDeckGeometryForOneTwoThreeAndPortrait(t *testing.T) {
	cases := []struct {
		layout string
		n      int
		want   string
	}{{"landscape", 1, "agent-table agent-count-1"}, {"landscape", 2, "agent-table agent-count-2"}, {"landscape", 3, "agent-table agent-count-3"}, {"portrait", 1, "portrait-stack agent-count-1"}, {"portrait", 2, "portrait-stack agent-count-2"}}
	for _, tc := range cases {
		agents := make([]state.PublicAgent, 0, tc.n)
		for i := 0; i < tc.n; i++ {
			agents = append(agents, m23Working(string(rune('a'+i)), "codex"))
		}
		body := m23RenderVM(t, BuildKindleViewModel(m23Pub(agents...), m23Now(), false, tc.layout, "none"))
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s/%d missing %q", tc.layout, tc.n, tc.want)
		}
		if strings.Count(body, "class=\"card WORKING") != tc.n {
			t.Fatalf("%s/%d artificial/missing cards", tc.layout, tc.n)
		}
	}
}
func TestKindleEmptyDeckDoesNotInventCards(t *testing.T) {
	body := m23RenderVM(t, BuildKindleViewModel(m23Pub(), m23Now(), false, "landscape", "none"))
	if strings.Contains(body, "class=\"card ") {
		t.Fatalf("artificial card rendered")
	}
	if !strings.Contains(body, "NO ACTIVE OR DELIVERED TASKS") {
		t.Fatal("empty state missing")
	}
}

func TestQuotaRemainingConversionAndFixedBar(t *testing.T) {
	cases := []struct {
		used           float64
		remaining, bar string
	}{{28, "72% LEFT", "[############----]"}, {0, "100% LEFT", "[################]"}, {100, "0% LEFT", "[----------------]"}, {-20, "100% LEFT", "[################]"}, {140, "0% LEFT", "[----------------]"}}
	for _, tc := range cases {
		windows := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: ptrFloat(tc.used)}}
		quota, connected := buildQuota([]state.PublicQuota{{Provider: "codex", Windows: &windows, SourceStatus: state.SourceAvailable}}, m23Now())
		if !connected || len(quota[0].Windows) != 1 {
			t.Fatalf("used=%v disconnected", tc.used)
		}
		got := quota[0].Windows[0]
		if got.Remaining != tc.remaining || got.Bar != tc.bar {
			t.Fatalf("used=%v got=%+v", tc.used, got)
		}
	}
}
func TestQuotaMultiWindowResetAndPartialAvailability(t *testing.T) {
	now := m23Now()
	r5 := now.Add(2*time.Hour + 18*time.Minute)
	rw := now.Add(3*24*time.Hour + 7*time.Hour)
	used5, usedW := 28.0, 57.0
	windows := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: &used5, ResetsAt: &r5}, {Name: "WEEK", UsedPercent: &usedW, ResetsAt: &rw}, {Name: "NO-DATA", UsedPercent: nil}}
	quota, connected := buildQuota([]state.PublicQuota{{Provider: "CODEX A", Windows: &windows, SourceStatus: state.SourceAvailable}, {Provider: "other", Windows: nil, SourceStatus: state.SourceUnavailable}}, now)
	if !connected {
		t.Fatal("usable provider masked by unavailable provider")
	}
	if len(quota[0].Windows) != 2 || len(quota[1].Windows) != 0 {
		t.Fatalf("quota=%+v", quota)
	}
	if quota[0].Windows[0].Reset != "reset 2h18m" || quota[0].Windows[1].Reset != "reset 3d07h" {
		t.Fatalf("reset=%+v", quota[0].Windows)
	}
	body := m23RenderVM(t, ViewModel{Layout: "landscape", Rotate: "none", RotationClass: "rotate-none", KindleRefresh: 20, SystemBar: "SYSTEM · NOT CONNECTED | 15:00", Quota: quota, QuotaConnected: true})
	if strings.Contains(body, "QUOTA · NOT CONNECTED") {
		t.Fatal("partial availability shown disconnected")
	}
	if !strings.Contains(body, "72% LEFT") || !strings.Contains(body, "43% LEFT") {
		t.Fatalf("remaining rows missing: %s", body)
	}
}
func TestQuotaResetNilAndPast(t *testing.T) {
	now := m23Now()
	past := now.Add(-time.Second)
	if got := quotaReset(nil, now); got != "" {
		t.Fatalf("nil reset=%q", got)
	}
	if got := quotaReset(&past, now); got != "reset due" {
		t.Fatalf("past reset=%q", got)
	}
}
func TestQuotaProviderIdentityIsNotRemapped(t *testing.T) {
	u := 13.0
	w := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: &u}}
	quota, _ := buildQuota([]state.PublicQuota{{Provider: "glm", Windows: &w, SourceStatus: state.SourceAvailable}}, m23Now())
	if quota[0].Provider != "glm" {
		t.Fatalf("provider remapped: %+v", quota[0])
	}
	if strings.Contains(strings.ToLower(quota[0].Provider), "claude") {
		t.Fatal("GLM mapped to Claude")
	}
}
func TestQuotaNoUsableWindowIsNotConnected(t *testing.T) {
	w := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: nil}}
	_, connected := buildQuota([]state.PublicQuota{{Provider: "codex", Windows: &w, SourceStatus: state.SourceAvailable}}, m23Now())
	if connected {
		t.Fatal("nil UsedPercent considered usable")
	}
}

func TestSystemBarConnectedAndUnavailableAlwaysHasClock(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 8, 21, 8, 43, 0, 0, loc)
	unavailable := BuildKindleViewModel(m23Pub(), now, false, "landscape", "none")
	if unavailable.SystemBar != "SYSTEM · NOT CONNECTED | 08:43" {
		t.Fatalf("unavailable=%q", unavailable.SystemBar)
	}
	cpu := 24.0
	memU, memT := uint64(14<<30), uint64(24<<30)
	swapU, swapT := uint64(1<<30), uint64(4<<30)
	disk := 61.0
	pub := m23Pub()
	pub.Sources["system"] = state.PublicSourceHealth{Status: state.SourceAvailable}
	pub.System.CPUPercent = &cpu
	pub.System.Memory = state.PublicMetricSet{UsedBytes: &memU, TotalBytes: &memT}
	pub.System.Swap = state.PublicMetricSet{UsedBytes: &swapU, TotalBytes: &swapT}
	pub.System.Disk = state.PublicMetricSet{PercentUsed: &disk}
	connected := BuildKindleViewModel(pub, now, false, "landscape", "none")
	if connected.SystemBar != "CPU 24% | MEM 14/24G | SWAP 1/4G | DISK 61% | 08:43" {
		t.Fatalf("connected=%q", connected.SystemBar)
	}
}
func TestKindlePrivacyAndDiagnosticsBoundary(t *testing.T) {
	pub := m23Pub(m23Working("PRIVATE_AGENT_ID", "codex"))
	vm := BuildKindleViewModel(pub, m23Now(), false, "landscape", "none")
	body := m23RenderVM(t, vm)
	for _, forbidden := range []string{"PRIVATE_AGENT_ID", "HOOK SOURCES", "PROJECTS", "focusLocator", "worktreeRoot", "session ID", "turn ID", "prompt", "transcript", "tool_input", "tool_output"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Kindle leaked %q", forbidden)
		}
	}
}
