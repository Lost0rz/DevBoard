package web

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/state"
)

// These tests map one-to-one to Docs/contracts/pad-display-v1.md §18. The
// browser-only viewport, scroll, and console assertions live with the real
// browser verification because Go's template tests cannot measure a layout.

func padTestNow() time.Time {
	return time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
}

func padTestState(tasks ...state.PublicTask) state.PublicState {
	return state.PublicState{
		SchemaVersion: 1,
		StateKind:     "public",
		GeneratedAt:   padTestNow(),
		Host:          state.PublicHost{ID: "mac-a", DisplayName: "Studio Mac"},
		Tasks:         tasks,
		Sources: map[string]state.PublicSourceHealth{
			"system": {Status: state.SourceAvailable},
		},
		Meta: state.DisplayMeta{CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800},
	}
}

func padTestDashboard(pub state.PublicState, sourceStatus dashboard.HostStatus) dashboard.State {
	fresh := dashboard.SnapshotFresh
	return dashboard.State{GeneratedAt: padTestNow(), Hosts: []dashboard.HostSnapshot{{
		ConfiguredHostID:  "mac-a",
		DisplayName:       "Studio Mac",
		Source:            dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: sourceStatus, LastSuccessAt: timePtr(padTestNow())},
		SnapshotFreshness: &fresh,
		State:             &pub,
	}}}
}

func padTask(id, provider, title string, lifecycle state.TaskLifecycle, updated time.Time) state.PublicTask {
	return state.PublicTask{
		ID: id, Provider: provider, Title: title, Lifecycle: lifecycle, Freshness: state.FreshnessFresh,
		Confidence: state.TaskConfidenceHigh, StartedAt: updated.Add(-5 * time.Minute), UpdatedAt: updated,
	}
}

func renderPadDashboard(t *testing.T, model dashboard.State, now time.Time) (string, DashboardDesktopViewModel) {
	t.Helper()
	s := testServer(t)
	vm := buildDashboardViewModel(model, now, false)
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "dashboard_fragment.html", vm); err != nil {
		t.Fatal(err)
	}
	return body.String(), vm
}

func renderPadViewModel(t *testing.T, vm DashboardDesktopViewModel) string {
	t.Helper()
	s := testServer(t)
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "dashboard_fragment.html", vm); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestPadScenario01EmptyHealthyNoSources(t *testing.T) {
	now := padTestNow()
	body, vm := renderPadDashboard(t, padTestDashboard(padTestState(), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 0 || strings.Count(body, `class="pad-task-card`) != 0 {
		t.Fatalf("empty board rendered task cards: tasks=%d", len(vm.Pad.Tasks))
	}
	for _, required := range []string{"Connection Strip", "TASKS", "HOST HEALTH", "MONITORED MACS", "AI SIGNALS", "QUOTA · NOT CONNECTED", "WEB WATCH · NOT CONNECTED", "CPU", "MEMORY", "SWAP", "DISK"} {
		if !strings.Contains(body, required) {
			t.Fatalf("scenario 1 missing %q", required)
		}
	}
	if strings.Contains(body, ">NORMAL<") {
		t.Fatalf("scenario 1 rendered a low-value NORMAL status label: %s", body)
	}
	if strings.Count(body, `class="pad-metric-block`) != 4 || strings.Count(body, `class="pad-usage-rail"`) != 4 {
		t.Fatalf("scenario 1 host metrics=%d rails=%d, want four each", strings.Count(body, `class="pad-metric-block`), strings.Count(body, `class="pad-usage-rail"`))
	}
	if strings.Count(body, "UNAVAILABLE") < 4 {
		t.Fatalf("scenario 1 unavailable metric state is not explicit: %s", body)
	}
}

func TestPadScenario02OneCodexWorking(t *testing.T) {
	now := padTestNow()
	working := padTask("working", "codex", "Build the status board", state.TaskWorking, now.Add(-2*time.Minute))
	working.Checkpoint = &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Editing the responsive layout", At: now.Add(-10 * time.Second)}
	body, vm := renderPadDashboard(t, padTestDashboard(padTestState(working), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 1 || vm.Pad.Tasks[0].State != "WORKING" || vm.Pad.Tasks[0].Provider != "CODEX" {
		t.Fatalf("working projection=%+v", vm.Pad.Tasks)
	}
	for _, required := range []string{"WORKING", "CODEX", "Build the status board", "Editing the responsive layout"} {
		if !strings.Contains(body, required) {
			t.Fatalf("scenario 2 missing %q", required)
		}
	}
}

func TestPadScenario03ClaudeReadyAction(t *testing.T) {
	now := padTestNow()
	ready := padTask("ready", "claude-code", "Choose the deployment target", state.TaskLifecycleAttention, now.Add(-4*time.Minute))
	ready.Attention = &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Choose staging or production", At: now.Add(-4 * time.Minute)}
	body, vm := renderPadDashboard(t, padTestDashboard(padTestState(ready), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 1 || vm.Pad.Tasks[0].State != "READY" || vm.Pad.Tasks[0].Provider != "CLAUDE CODE" {
		t.Fatalf("ready projection=%+v", vm.Pad.Tasks)
	}
	for _, required := range []string{"READY", "CLAUDE CODE", "ACTION REQUIRED", "Choose staging or production"} {
		if !strings.Contains(body, required) {
			t.Fatalf("scenario 3 missing %q", required)
		}
	}
}

func TestPadScenario04MixedProviderOrdering(t *testing.T) {
	now := padTestNow()
	complete := padTask("complete", "codex", "Recent delivery", state.TaskComplete, now.Add(-3*time.Minute))
	complete.Completion = &state.PublicTaskCompletion{Summary: stringPtr("Delivered the change."), At: now.Add(-3 * time.Minute)}
	working := padTask("working", "codex", "Active implementation", state.TaskWorking, now.Add(-time.Minute))
	ready := padTask("ready", "claude-code", "Needs an answer", state.TaskLifecycleAttention, now.Add(-2*time.Minute))
	ready.Attention = &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Answer required", At: now.Add(-2 * time.Minute)}
	_, vm := renderPadDashboard(t, padTestDashboard(padTestState(complete, working, ready), dashboard.HostStatus("online")), now)
	if got := []string{vm.Pad.Tasks[0].State, vm.Pad.Tasks[1].State, vm.Pad.Tasks[2].State}; !equalStrings(got, []string{"READY", "WORKING", "COMPLETE"}) {
		t.Fatalf("mixed ordering=%v", got)
	}
	if vm.Pad.Tasks[0].Provider != "CLAUDE CODE" || vm.Pad.Tasks[2].Provider != "CODEX" {
		t.Fatalf("provider-independent ordering=%+v", vm.Pad.Tasks)
	}
}

func TestPadScenario05CapacityAndHiddenCount(t *testing.T) {
	now := padTestNow()
	tasks := []state.PublicTask{
		padTask("w1", "codex", "Working one", state.TaskWorking, now.Add(-time.Minute)),
		padTask("w2", "claude-code", "Working two", state.TaskWorking, now.Add(-2*time.Minute)),
		padTask("w3", "codex", "Working three", state.TaskWorking, now.Add(-3*time.Minute)),
		padTask("c1", "codex", "Complete one", state.TaskComplete, now.Add(-3*time.Minute)),
		padTask("c2", "claude-code", "Complete two", state.TaskComplete, now.Add(-4*time.Minute)),
	}
	for i := 3; i < len(tasks); i++ {
		at := now.Add(-time.Duration(i+9) * time.Minute)
		tasks[i].Completion = &state.PublicTaskCompletion{At: at}
		tasks[i].UpdatedAt = at
	}
	_, vm := renderPadDashboard(t, padTestDashboard(padTestState(tasks...), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 3 || vm.Pad.HiddenTaskCount != 2 || vm.Pad.DeckClass != "agent-count-3" {
		t.Fatalf("capacity projection tasks=%d hidden=%d class=%q", len(vm.Pad.Tasks), vm.Pad.HiddenTaskCount, vm.Pad.DeckClass)
	}
	for _, task := range vm.Pad.Tasks {
		if task.State == "COMPLETE" {
			t.Fatalf("working tasks must displace complete tasks: %+v", vm.Pad.Tasks)
		}
	}
}

func TestPadScenario06CompleteDecayAndExpiry(t *testing.T) {
	now := padTestNow()
	makeComplete := func(id string, age time.Duration) state.PublicTask {
		at := now.Add(-age)
		task := padTask(id, "codex", id, state.TaskComplete, at)
		task.Completion = &state.PublicTaskCompletion{At: at}
		task.UpdatedAt = at
		return task
	}
	_, vm := renderPadDashboard(t, padTestDashboard(padTestState(
		makeComplete("high", 5*time.Minute), makeComplete("muted", 10*time.Minute), makeComplete("retained", 29*time.Minute), makeComplete("expired", 30*time.Minute),
	), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 3 {
		t.Fatalf("complete retention count=%d tasks=%+v", len(vm.Pad.Tasks), vm.Pad.Tasks)
	}
	if vm.Pad.Tasks[0].CompletionPhase != "high" || vm.Pad.Tasks[1].CompletionPhase != "muted" || vm.Pad.Tasks[2].CompletionPhase != "muted" {
		t.Fatalf("completion phases=%+v", vm.Pad.Tasks)
	}
	for _, task := range vm.Pad.Tasks {
		if task.Title == "expired" {
			t.Fatal("30-minute COMPLETE task was not removed")
		}
	}
}

func TestPadScenario07OfflineRetainsLastGoodDataHonestly(t *testing.T) {
	now := padTestNow()
	working := padTask("stale-working", "codex", "Last known work", state.TaskWorking, now.Add(-3*time.Minute))
	working.Freshness = state.FreshnessStale
	pub := padTestState(working)
	stale := dashboard.SnapshotStale
	model := padTestDashboard(pub, dashboard.HostStatus("offline"))
	model.Hosts[0].SnapshotFreshness = &stale
	body, vm := renderPadDashboard(t, model, now)
	if vm.Pad.Connection.MacStatus != "OFFLINE" || len(vm.Pad.Tasks) != 1 || !vm.Pad.Tasks[0].Stale {
		t.Fatalf("offline retained projection=%+v connection=%+v", vm.Pad.Tasks, vm.Pad.Connection)
	}
	for _, required := range []string{"OFFLINE", "DATA STALE", "Last known work", "--"} {
		if !strings.Contains(body, required) {
			t.Fatalf("scenario 7 missing %q", required)
		}
	}
}

func TestPadScenario08AbnormalHostMetricsStayBounded(t *testing.T) {
	now := padTestNow()
	cpu, memoryUsed, memoryTotal, swapUsed, swapTotal, diskUsed, diskTotal := 99.0, uint64(15<<30), uint64(16<<30), uint64(7<<30), uint64(8<<30), uint64(95<<30), uint64(100<<30)
	pub := padTestState()
	pub.System.CPUPercent = &cpu
	pub.System.Memory = state.PublicMetricSet{UsedBytes: &memoryUsed, TotalBytes: &memoryTotal, PercentUsed: floatPtr(93.75)}
	pub.System.Swap = state.PublicMetricSet{UsedBytes: &swapUsed, TotalBytes: &swapTotal, PercentUsed: floatPtr(87.5)}
	pub.System.Disk = state.PublicMetricSet{UsedBytes: &diskUsed, TotalBytes: &diskTotal, PercentUsed: floatPtr(95)}
	body, _ := renderPadDashboard(t, padTestDashboard(pub, dashboard.HostStatus("online")), now)
	for _, required := range []string{"99%", "15.0 / 16.0 GiB", "7.0 / 8.0 GiB", "95.0 / 100.0 GiB", "CRITICAL", "WARNING", "pad-usage-rail"} {
		if !strings.Contains(body, required) {
			t.Fatalf("scenario 8 missing %q", required)
		}
	}
	if strings.Contains(body, "NETWORK") || strings.Contains(body, "Process") {
		t.Fatal("host health expanded beyond the four frozen metrics")
	}
}

func TestPadMetricProjectionUsesRealPercentagesAndSeverity(t *testing.T) {
	used, total := uint64(8<<30), uint64(16<<30)
	metric := padMetric(state.PublicMetricSet{UsedBytes: &used, TotalBytes: &total})
	if metric.Percent != "50%" || metric.Detail != "8.0 / 16.0 GiB" || metric.Status != "NORMAL" || metric.RailPercent != 50 {
		t.Fatalf("derived metric=%+v", metric)
	}
	// A NORMAL metric keeps its percentage and rail, but the low-value
	// NORMAL label itself is only rendered for abnormal states.
	now := padTestNow()
	cpu := 50.0
	pub := padTestState()
	pub.System.CPUPercent = &cpu
	body, _ := renderPadDashboard(t, padTestDashboard(pub, dashboard.HostStatus("online")), now)
	if !strings.Contains(body, "50%") {
		t.Fatalf("normal metric percentage missing: %s", body)
	}
	if strings.Contains(body, ">NORMAL<") {
		t.Fatal("NORMAL status label must be suppressed for healthy metrics")
	}
	if status, class := padMetricStatus(70); status != "WARNING" || class != "pad-metric-warning" {
		t.Fatalf("warning threshold=%s %s", status, class)
	}
	if status, class := padMetricStatus(90); status != "CRITICAL" || class != "pad-metric-critical" {
		t.Fatalf("critical threshold=%s %s", status, class)
	}
	if unavailable := padMetric(state.PublicMetricSet{}); unavailable.Status != "UNAVAILABLE" || unavailable.Percent != "--" {
		t.Fatalf("unavailable metric=%+v", unavailable)
	}
}

func TestPadReadabilityCSSHasCardCountLayoutsAndNoMaxContent(t *testing.T) {
	css, err := templateFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	for _, required := range []string{"agent-count-1 .pad-task-state", "agent-count-2 .pad-task-state", "agent-count-3 .pad-task-state", "pad-task-state-mark", "pad-task-state-label", "pad-metric-percent", "pad-usage-rail", "pad-metric-normal", "pad-metric-warning", "pad-metric-critical", "pad-metric-unavailable"} {
		if !strings.Contains(text, required) {
			t.Fatalf("readability CSS missing %q", required)
		}
	}
	if strings.Contains(text, "max-content") {
		t.Fatal("Pad lower band must not use max-content sizing")
	}
}

// TestPadCSSConsolidationAndGlyphSafety pins the consolidated Pad style
// region: CJK fallback fonts, complete-line truncation only, and clamp
// rules paired with an exact line-height/max-height multiple so no half
// glyph or clipped baseline can render.
func TestPadCSSConsolidationAndGlyphSafety(t *testing.T) {
	css, err := templateFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	for _, required := range []string{`"PingFang SC"`, `"Hiragino Sans GB"`, `"Noto Sans CJK SC"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("CJK fallback font missing from shared stack: %q", required)
		}
	}
	padStart := strings.Index(text, ".pad-body {")
	padEnd := strings.Index(text[padStart:], "@media (max-width: 1060px)")
	if padStart < 0 || padEnd < 0 {
		t.Fatal("consolidated Pad style region not found")
	}
	pad := text[padStart : padStart+padEnd]
	// One consolidated region: no second .pad-body block may follow.
	if n := strings.Count(pad, ".pad-body {"); n != 1 {
		t.Fatalf("pad style region defines .pad-body %d times, want 1", n)
	}
	for _, clamp := range []struct {
		selector   string
		lineClamp  string
		maxHeight  string
		lineHeight string
	}{
		{selector: ".pad-task-title", lineClamp: "2", maxHeight: "2.7em", lineHeight: "1.35"},
		{selector: ".pad-task-detail p", lineClamp: "2", maxHeight: "2.8em", lineHeight: "1.4"},
	} {
		ruleStart := strings.Index(pad, clamp.selector+" {")
		if ruleStart < 0 {
			t.Fatalf("pad clamp rule missing for %s", clamp.selector)
		}
		ruleEnd := strings.Index(pad[ruleStart:], "}")
		rule := pad[ruleStart : ruleStart+ruleEnd]
		for _, required := range []string{"-webkit-line-clamp: " + clamp.lineClamp, "max-height: " + clamp.maxHeight, "line-height: " + clamp.lineHeight} {
			if !strings.Contains(rule, required) {
				t.Fatalf("%s rule missing paired clamp guarantee %q: %s", clamp.selector, required, rule)
			}
		}
	}
}

func TestPadScenario09QuotaAvailableWebUnavailable(t *testing.T) {
	used := 25.0
	windows := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: &used}}
	pub := padTestState()
	pub.Quota = []state.PublicQuota{{Provider: "codex", Windows: &windows, SourceStatus: state.SourceAvailable}}
	body, _ := renderPadDashboard(t, padTestDashboard(pub, dashboard.HostStatus("online")), padTestNow())
	if !strings.Contains(body, "75% LEFT") || !strings.Contains(body, "WEB WATCH · NOT CONNECTED") || strings.Contains(body, "QUOTA · NOT CONNECTED") {
		t.Fatalf("quota/browser split incorrect: %s", body)
	}
}

func TestPadScenario10WebNotificationQuotaUnavailable(t *testing.T) {
	model := padTestDashboard(padTestState(), dashboard.HostStatus("online"))
	vm := buildDashboardViewModel(model, padTestNow(), false)
	vm.Pad.WebConnected = true
	vm.Pad.WebNotifications = []PadWebNotificationView{{Service: "CHATGPT", Status: "NEW REPLY", Conversation: "Release planning", Age: "2M AGO"}}
	body := renderPadViewModel(t, vm)
	if !strings.Contains(body, "NEW REPLY") || !strings.Contains(body, "Release planning") || !strings.Contains(body, "CHATGPT") {
		t.Fatalf("web notification missing: %s", body)
	}
	if !strings.Contains(body, "QUOTA · NOT CONNECTED") {
		t.Fatal("quota unavailable rail missing")
	}
}

func TestPadScenario11RefreshFailureKeepsDOMAndRecovers(t *testing.T) {
	b, err := templateFS.ReadFile("static/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, required := range []string{"container.innerHTML = html", "setRefreshPaused(true)", "setRefreshPaused(false)", "window.setTimeout(refresh, delay)", "last successful server-rendered DOM", "REFRESH STALE"} {
		if !strings.Contains(text, required) {
			t.Fatalf("refresh recovery missing %q", required)
		}
	}
	if strings.Contains(text, `container.innerHTML = ""`) {
		t.Fatal("refresh failure may not erase the last successful DOM")
	}
}

func TestPadScenario12PrivacySentinelsAndFourRegions(t *testing.T) {
	now := padTestNow()
	task := padTask("PRIVATE_TASK_ID", "codex", "Safe title", state.TaskWorking, now)
	task.Project = &state.PublicTaskProject{ProjectName: "PRIVATE_PROJECT", WorktreeLabel: "PRIVATE_WORKTREE", Branch: "PRIVATE_BRANCH"}
	task.Checkpoint = &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Safe checkpoint", At: now}
	pub := padTestState(task)
	body, _ := renderPadDashboard(t, padTestDashboard(pub, dashboard.HostStatus("online")), now)
	for _, forbidden := range []string{"PRIVATE_TASK_ID", "PRIVATE_PROJECT", "PRIVATE_WORKTREE", "PRIVATE_BRANCH", "resultIdentifier", "Source health", "Provider lifecycle", "PROJECT IDENTITY", "GLOBAL ATTENTION", "product-nav", "Overview"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("privacy/IA sentinel leaked: %q", forbidden)
		}
	}
	if got := strings.Count(body, `class="pad-region `); got != 4 {
		t.Fatalf("top-level Pad regions=%d, want 4", got)
	}
}

func TestPadMultiHostTasksAreGloballySortedAndScoped(t *testing.T) {
	now := padTestNow()
	ready := padTask("same-local-id", "claude-code", "Answer on laptop", state.TaskLifecycleAttention, now.Add(-time.Minute))
	ready.Attention = &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Laptop needs an answer", At: now.Add(-time.Minute)}
	working := padTask("same-local-id", "codex", "Work on studio", state.TaskWorking, now.Add(-2*time.Minute))
	studio := padTestState(working)
	laptop := padTestState(ready)
	laptop.Host.ID = "mac-b"
	model := dashboard.State{GeneratedAt: now, Hosts: []dashboard.HostSnapshot{
		{ConfiguredHostID: "mac-a", DisplayName: "Studio Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "online"}, State: ptrPublicState(studio)},
		{ConfiguredHostID: "mac-b", DisplayName: "Laptop", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "online"}, State: ptrPublicState(laptop)},
	}}
	vm := buildDashboardViewModel(model, now, false)
	if len(vm.Pad.Tasks) != 2 || vm.Pad.Tasks[0].State != "READY" || vm.Pad.Tasks[0].HostLabel != "Laptop · mac-b" {
		t.Fatalf("global task ordering=%+v", vm.Pad.Tasks)
	}
	if vm.Pad.Tasks[0].ScopedKey == vm.Pad.Tasks[1].ScopedKey || vm.Pad.Tasks[1].HostLabel != "Studio Mac · mac-a" {
		t.Fatalf("same local task IDs collapsed=%+v", vm.Pad.Tasks)
	}
	body := renderPadViewModel(t, vm)
	if !strings.Contains(body, "Laptop · mac-b · CLAUDE CODE") || !strings.Contains(body, "Studio Mac · mac-a · CODEX") {
		t.Fatalf("host/provider ownership not prominent: %s", body)
	}
}

func TestPadConnectionStripCountsHostsAndDedupesQuota(t *testing.T) {
	now := padTestNow()
	used := 25.0
	windows := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: &used}}
	makeState := func(hostID string) *state.PublicState {
		pub := padTestState()
		pub.Host.ID = hostID
		pub.Quota = []state.PublicQuota{{Provider: "codex", AccountKey: "acct_same", DisplayLabel: "Codex A", Windows: &windows, SourceStatus: state.SourceAvailable}}
		return &pub
	}
	model := dashboard.State{GeneratedAt: now, Hosts: []dashboard.HostSnapshot{
		{ConfiguredHostID: "mac-a", DisplayName: "Studio Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "online"}, State: makeState("mac-a")},
		{ConfiguredHostID: "mac-b", DisplayName: "Laptop", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "stale"}, State: makeState("mac-b")},
		{ConfiguredHostID: "mac-c", DisplayName: "Build Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "offline"}},
	}}
	vm := buildDashboardViewModel(model, now, false)
	if vm.Pad.Connection.HubStatus != "DEGRADED" || vm.Pad.Connection.HostCount != 3 || vm.Pad.Connection.OnlineCount != 1 || vm.Pad.Connection.StaleCount != 1 || vm.Pad.Connection.OfflineCount != 1 {
		t.Fatalf("connection aggregate=%+v", vm.Pad.Connection)
	}
	if len(vm.Pad.Quota) != 1 {
		t.Fatalf("cross-host quota duplicate=%+v", vm.Pad.Quota)
	}
	body := renderPadViewModel(t, vm)
	for _, required := range []string{"HUB", "DEGRADED", "1/3 MACS ONLINE", "1 STALE", "1 OFFLINE", "Codex A", "WEB WATCH · NOT CONNECTED"} {
		if !strings.Contains(body, required) {
			t.Fatalf("connection/quota missing %q: %s", required, body)
		}
	}
}

func TestPadMoreThanThreeHostsPrioritizesFailureAndShowsSummary(t *testing.T) {
	now := padTestNow()
	hosts := make([]dashboard.HostSnapshot, 0, 5)
	statuses := []dashboard.HostStatus{"online", "online", "offline", "stale", "online"}
	for i, status := range statuses {
		id := fmt.Sprintf("mac-%c", 'a'+rune(i))
		hosts = append(hosts, dashboard.HostSnapshot{ConfiguredHostID: id, DisplayName: id, Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: status}})
	}
	vm := buildDashboardViewModel(dashboard.State{GeneratedAt: now, Hosts: hosts}, now, false)
	if len(vm.Pad.Hosts) != 3 || vm.Pad.HiddenHostCount != 2 {
		t.Fatalf("host capacity=%d hidden=%d", len(vm.Pad.Hosts), vm.Pad.HiddenHostCount)
	}
	if vm.Pad.Hosts[0].Connection != "OFFLINE" || vm.Pad.Hosts[1].Connection != "STALE" {
		t.Fatalf("host failure priority=%+v", vm.Pad.Hosts)
	}
	body := renderPadViewModel(t, vm)
	if !strings.Contains(body, "+2 HOSTS") {
		t.Fatalf("host summary missing: %s", body)
	}
}

func TestPadReadyAlwaysPrecedesStaleWorking(t *testing.T) {
	now := padTestNow()
	ready := padTask("ready", "claude-code", "Needs an answer", state.TaskLifecycleAttention, now.Add(-10*time.Minute))
	ready.Attention = &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Answer required", At: ready.UpdatedAt}
	staleWorking := padTask("stale-work", "codex", "Retained work", state.TaskWorking, now.Add(-time.Minute))
	readyState := padTestState(ready)
	staleState := padTestState(staleWorking)
	model := dashboard.State{GeneratedAt: now, Hosts: []dashboard.HostSnapshot{
		{ConfiguredHostID: "mac-a", DisplayName: "Studio Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "online"}, State: ptrPublicState(readyState)},
		{ConfiguredHostID: "mac-b", DisplayName: "Laptop", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: "stale"}, State: ptrPublicState(staleState)},
	}}
	vm := buildDashboardViewModel(model, now, false)
	if len(vm.Pad.Tasks) != 2 || vm.Pad.Tasks[0].State != "READY" || !vm.Pad.Tasks[1].Stale {
		t.Fatalf("strict stale ordering=%+v", vm.Pad.Tasks)
	}
}

func TestPadRejectsOutOfRangeMetricPercentage(t *testing.T) {
	metric := padMetricFromPercent(101, "USED/TOTAL UNAVAILABLE")
	if metric.Status != "UNAVAILABLE" || metric.Percent != "--" || metric.RailPercent != 0 {
		t.Fatalf("invalid metric was rendered as usable: %+v", metric)
	}
}

func ptrPublicState(value state.PublicState) *state.PublicState { return &value }

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func stringPtr(value string) *string  { return &value }
func floatPtr(value float64) *float64 { return &value }

// The 2026-08-25 recovered-error amendment: an error card superseded by a
// later successful terminal Stop of the same session must not occupy a Pad
// READY slot, while an unrecovered error keeps its READY emphasis.
func TestPadSupersededErrorCardLeavesDeck(t *testing.T) {
	now := padTestNow()
	superseded := now.Add(-time.Hour)
	oldError := padTask("old-error", "claude-code", "Blocked turn", state.TaskError, superseded)
	oldError.Attention = &state.PublicTaskAttention{Kind: state.AttentionRateLimited, Text: "Rate limited", At: superseded}
	oldError.SupersededAt = &superseded
	unrecovered := padTask("stuck-error", "claude-code", "Still blocked", state.TaskError, now.Add(-10*time.Minute))
	unrecovered.Attention = &state.PublicTaskAttention{Kind: state.AttentionAuthenticationRequired, Text: "Authentication required", At: now.Add(-10 * time.Minute)}
	complete := padTask("new-complete", "claude-code", "Recovered turn", state.TaskComplete, now.Add(-time.Minute))
	complete.Completion = &state.PublicTaskCompletion{At: now.Add(-time.Minute)}

	body, vm := renderPadDashboard(t, padTestDashboard(padTestState(oldError, unrecovered, complete), dashboard.HostStatus("online")), now)
	if len(vm.Pad.Tasks) != 2 {
		t.Fatalf("superseded error must leave the Pad deck: tasks=%d", len(vm.Pad.Tasks))
	}
	if strings.Contains(body, "old-error") || strings.Contains(body, "Blocked turn") {
		t.Fatal("superseded error card still rendered")
	}
	if vm.Pad.Tasks[0].State != "READY" || vm.Pad.Tasks[0].Title != "Still blocked" {
		t.Fatalf("unrecovered error must keep top READY slot: %+v", vm.Pad.Tasks[0])
	}
	if vm.Pad.Tasks[1].State != "COMPLETE" || vm.Pad.Tasks[1].Title != "Recovered turn" {
		t.Fatalf("recovered turn must render its COMPLETE card: %+v", vm.Pad.Tasks[1])
	}
}
