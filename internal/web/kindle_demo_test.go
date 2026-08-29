package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestKindleDemoIsAdditiveAndMonochrome(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	root := state.MockInternalState(now, state.HostState{ID: "mac-a", DisplayName: "Mac Air"})
	s, err := NewRoleServer(state.NewStore(root), state.ProjectionConfig{KindleRefreshSeconds: 20}, true, nil, nil, config.RuntimeRoleNode, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	for _, path := range []string{"/kindle/R", "/kindle/L"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, rec.Code)
		}
		body := strings.ToLower(rec.Body.String())
		for _, forbidden := range []string{"<script", "display:grid", "display:flex", "websocket", "<canvas", "svg", "color:red", "color:blue", "color:green", "color:orange", "#ff0000", "#00ff00", "#0000ff"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains forbidden %q", path, forbidden)
			}
		}
		for _, forbidden := range []string{"kindle-meter", "mac-a", "mac-b"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s leaks compact-only field %q", path, forbidden)
			}
		}
		for _, required := range []string{"mac a", "mac b", "5h", "week", "codex a", "codex b"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing compact Kindle field %q", path, required)
			}
		}
		if !strings.Contains(body, "@media (orientation:landscape)") {
			t.Fatalf("%s missing landscape preview fallback", path)
		}
		if !strings.Contains(body, "@media (orientation:portrait)") {
			t.Fatalf("%s missing portrait compact layout", path)
		}
		if !strings.Contains(body, `http-equiv="refresh" content="2"`) {
			t.Fatalf("%s does not use the two-second Kindle refresh contract", path)
		}
		if !strings.Contains(body, "kindle-fixed-890") {
			t.Fatalf("%s missing fixed 890x750 canvas", path)
		}
		for _, want := range []string{"kindle-state-complete", "kindle-state-highlight", "background:#fff", "kindle-host-metric-track", "cpu", "memory", "swap", "disk"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing complete-state visual %q", path, want)
			}
		}
		if strings.Contains(body, ">web<") {
			t.Fatalf("%s still renders WEB in the resource metric row", path)
		}
		if strings.Contains(body, "reserved") {
			t.Fatalf("%s renders the hidden browser-reserved area", path)
		}
		required := []string{"kindle-main", "kindle-task-card", "kindle-task-request", "kindle-task-feedback", "kindle-quota-table", "http-equiv=\"refresh\"", "font-size:18px"}
		orientation := "kindle-rotate-none"
		if strings.HasSuffix(path, "/R") || strings.HasSuffix(path, "/r") {
			orientation = "kindle-rotate-right"
		}
		if strings.HasSuffix(path, "/L") || strings.HasSuffix(path, "/l") {
			orientation = "kindle-rotate-left"
		}
		required = append(required, orientation)
		for _, want := range required {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
	for _, path := range []string{"/kindle", "/k/R", "/k2/R", "/display/kindle", "/display/kindle/R", "/display/kindle-demo", "/kindle/R?rotate=none"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("retired Kindle route %s status=%d", path, rec.Code)
		}
	}
	invalid := httptest.NewRecorder()
	s.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/kindle/X", nil))
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid Kindle orientation status=%d", invalid.Code)
	}
}

func TestKindleDemoUsesResourceMetricLabels(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	cpu, memory, swap, disk := 22.0, 44.0, 4.0, 66.0
	pub := padTestState()
	pub.System.CPUPercent = &cpu
	pub.System.Memory.PercentUsed = &memory
	pub.System.Swap.PercentUsed = &swap
	pub.System.Disk.PercentUsed = &disk
	model := padTestDashboard(pub, dashboard.HostStatus("online"))
	vm := buildKindleDemoViewModel(model, now, false, "right")
	if len(vm.Hosts) != 1 || len(vm.Hosts[0].Metrics) != 4 {
		t.Fatalf("host metrics=%+v, want one host with four resource metrics", vm.Hosts)
	}
	want := []string{"CPU", "MEMORY", "SWAP", "DISK"}
	for index, name := range want {
		if vm.Hosts[0].Metrics[index].Name != name {
			t.Fatalf("metric %d=%q, want %q", index, vm.Hosts[0].Metrics[index].Name, name)
		}
	}
	body := renderKindleDemoTemplate(t, vm)
	if strings.Contains(body, ">WEB<") {
		t.Fatalf("Kindle resource row still contains WEB: %s", body)
	}
}

func TestKindleDemoUsesFiveHourAndWeeklyWindowsForEveryProvider(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	fiveHourReset := now.Add(4*time.Hour + 12*time.Minute)
	weekReset := now.Add(72*time.Hour + 7*time.Hour)
	codexFiveHour := 20.0
	codexWeek := 35.0
	glmFiveHour := 45.0
	glmWeek := 55.0
	mcp := 10.0
	model := dashboard.State{
		GeneratedAt: now,
		Quota: []state.PublicQuota{
			{Provider: "codex", DisplayLabel: "Codex A", AccountKey: "a", SourceStatus: state.SourceAvailable, Windows: &[]state.PublicQuotaWindow{{Name: "PRIMARY", UsedPercent: &codexFiveHour, ResetsAt: &fiveHourReset}, {Name: "SECONDARY", UsedPercent: &codexWeek, ResetsAt: &weekReset}}},
			{Provider: "codex", DisplayLabel: "Codex B", AccountKey: "b", SourceStatus: state.SourceAvailable, Windows: &[]state.PublicQuotaWindow{{Name: "PRIMARY", UsedPercent: &codexFiveHour}, {Name: "SECONDARY", UsedPercent: &codexWeek}}},
			{Provider: "z.ai", DisplayLabel: "GLM", AccountKey: "g", SourceStatus: state.SourceAvailable, Windows: &[]state.PublicQuotaWindow{{Name: "PRIMARY", UsedPercent: &glmFiveHour}, {Name: "SECONDARY", UsedPercent: &glmWeek}, {Name: "MCP", UsedPercent: &mcp}}},
		},
	}
	vm := buildKindleDemoViewModel(model, now, false, "right")
	if !vm.QuotaConnected || len(vm.Quota) != 3 {
		t.Fatalf("quota connected=%v rows=%d: %+v", vm.QuotaConnected, len(vm.Quota), vm.Quota)
	}
	for _, quota := range vm.Quota {
		if len(quota.Windows) != 2 || quota.Windows[0].Label != "5H" || quota.Windows[1].Label != "WEEK" {
			t.Fatalf("%s windows=%+v, want 5H/WEEK", quota.Label, quota.Windows)
		}
		for _, window := range quota.Windows {
			if window.Label == "TOKEN" {
				t.Fatalf("%s retained legacy TOKEN label: %+v", quota.Label, quota.Windows)
			}
		}
	}
	if got := vm.Quota[0].Windows[0].ResetInfo; !strings.Contains(got, "IN 4h12m") || !strings.Contains(got, fiveHourReset.Local().Format("01/02 15:04")) {
		t.Fatalf("five-hour reset info=%q", got)
	}
	if got := vm.Quota[0].Windows[1].ResetInfo; !strings.Contains(got, "IN 3d07h") || !strings.Contains(got, weekReset.Local().Format("01/02 15:04")) {
		t.Fatalf("weekly reset info=%q", got)
	}
}

func TestKindleDemoAddsBoundedProgressToReadyAndCompleteCards(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	attention := state.PublicTask{
		ID: "ready", Provider: "claude-code", Title: "Confirm the quota source", Lifecycle: state.TaskLifecycleAttention,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-4 * time.Minute), UpdatedAt: now,
		Attention:  &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Choose the account", At: now},
		Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointInspecting, Text: "Inspecting the current quota source", At: now.Add(-time.Minute)},
	}
	completed := state.PublicTask{
		ID: "complete", Provider: "codex", Title: "Deploy the Kindle display", Lifecycle: state.TaskComplete,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-10 * time.Minute), UpdatedAt: now, Unread: true,
		Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointValidating, Text: "Validating the Hub route", At: now.Add(-2 * time.Minute)},
		Completion: &state.PublicTaskCompletion{Summary: stringPtr("Hub route is live and quota rows are rendering."), At: now},
	}
	model := padTestDashboard(padTestState(attention, completed), dashboard.HostStatus("online"))
	vm := buildKindleDemoViewModel(model, now, false, "right")
	if len(vm.Tasks) != 2 {
		t.Fatalf("tasks=%d, want 2: %+v", len(vm.Tasks), vm.Tasks)
	}
	if vm.Tasks[0].State != "READY" || !strings.Contains(vm.Tasks[0].Detail, "Inspecting the current quota source") {
		t.Fatalf("ready supplement=%+v", vm.Tasks[0])
	}
	if vm.Tasks[1].State != "COMPLETE" || !strings.Contains(vm.Tasks[1].Detail, "Validating the Hub route") {
		t.Fatalf("complete supplement=%+v", vm.Tasks[1])
	}
	if !strings.Contains(vm.Tasks[0].StateClass, "kindle-state-highlight") || !strings.Contains(vm.Tasks[1].StateClass, "kindle-state-highlight") {
		t.Fatalf("ready/unread complete highlight=%+v", vm.Tasks)
	}
	body := renderKindleDemoTemplate(t, vm)
	for _, want := range []string{"ACTION REQUIRED", "FEEDBACK", "Validating the Hub route"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered Kindle card missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "LAST CHECKPOINT") || strings.Contains(body, "LAST PROGRESS") {
		t.Fatalf("checkpoint/progress was rendered as a separate region: %s", body)
	}
}

func TestKindleDemoTaskCardAcceptsNativeFormClick(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	task := state.PublicTask{
		ID: "clickable", Provider: "codex", Title: "Open the Codex conversation", Lifecycle: state.TaskWorking,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now, UpdatedAt: now,
		Navigation: &state.PublicNavigationTarget{TargetID: "opaque-agent", Kind: state.NavigationAgent, AllowedActions: []state.NavigationAction{state.ActionFocusAgent}},
	}
	model := padTestDashboard(padTestState(task), dashboard.HostStatus("online"))
	vm := buildKindleDemoViewModel(model, now, false, "right")
	if len(vm.Tasks) != 1 || !vm.Tasks[0].Navigable || vm.Tasks[0].HostID != "mac-a" {
		t.Fatalf("Kindle navigation was not projected: %+v", vm.Tasks)
	}
	body := renderKindleDemoTemplate(t, vm)
	for _, want := range []string{`method="post" action="/api/navigation"`, `class="kindle-task-action-overlay"`, `name="target_id" value="opaque-agent"`, `name="task_id" value="clickable"`, `name="action" value="focus_agent"`, `name="return_to" value="/kindle/R"`, "Open the Codex conversation"} {
		if !strings.Contains(body, want) {
			t.Fatalf("clickable Kindle card missing %q: %s", want, body)
		}
	}
}

func TestKindleDemoHidesCompleteAfterReadAcknowledgement(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	completed := state.PublicTask{
		ID: "complete-read", Provider: "codex", Title: "Already acknowledged", Lifecycle: state.TaskComplete,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-2 * time.Minute), UpdatedAt: now,
		Completion: &state.PublicTaskCompletion{Summary: stringPtr("The result was acknowledged."), At: now},
	}
	model := padTestDashboard(padTestState(completed), dashboard.HostStatus("online"))
	vm := buildKindleDemoViewModel(model, now, false, "right")
	if len(vm.Tasks) != 0 {
		t.Fatalf("read complete must leave the home surface: %+v", vm.Tasks)
	}
}

func renderKindleDemoTemplate(t *testing.T, vm KindleDemoViewModel) string {
	t.Helper()
	s := testServer(t)
	var body strings.Builder
	if err := s.templates.ExecuteTemplate(&body, "kindle_demo.html", vm); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestKindleDemoIsAvailableOnHubAndUsesAggregateState(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	rt, err := hub.NewRuntime([]hub.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac Air", Enabled: true, Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewHubServer(state.ProjectionConfig{KindleRefreshSeconds: 20}, false, slog.New(slog.NewTextHandler(io.Discard, nil)), rt, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kindle/R", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("hub kindle demo status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "kindle-safe-rail") || !strings.Contains(rec.Body.String(), "0/1") {
		t.Fatalf("hub kindle demo missing aggregate header: %s", rec.Body.String())
	}
}
