package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestProductDashboardUsesLocalAssetsAndFragment(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/assets/app.css", "/assets/dashboard.js", "/display/fragment"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s status=%d cache=%q", path, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/display", nil))
	body := rec.Body.String()
	if strings.Contains(body, `meta http-equiv="refresh"`) || !strings.Contains(body, "/assets/app.css") {
		t.Fatalf("modern display shell invalid: %s", body)
	}
	if !strings.Contains(body, ">TASKS<") || !strings.Contains(body, "WEB WATCH · NOT CONNECTED") || !strings.Contains(body, "data-refresh-seconds") {
		t.Fatal("display omitted the current dashboard fragment shell")
	}
}

func renderProductFragment(t *testing.T, model dashboard.State, now time.Time) string {
	t.Helper()
	s := testServer(t)
	vm := buildDashboardViewModel(model, now, false)
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "dashboard_fragment.html", vm); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestProductDashboardRendersCompleteOperationalStateMatrix(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	summary := "Delivered the frontend product; validation passes."
	working := state.PublicTask{
		ID: "PRIVATE_TASK_ID", Provider: "codex", Title: "Build status board", Lifecycle: state.TaskWorking,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-8 * time.Minute), UpdatedAt: now,
		Project:    &state.PublicTaskProject{ProjectName: "DevBoard", Branch: "codex/pc1-frontend"},
		Checkpoint: &state.PublicTaskCheckpoint{Kind: state.CheckpointEditing, Text: "Editing the responsive dashboard", At: now},
	}
	attention := state.PublicTask{
		ID: "PRIVATE_ATTENTION_ID", Provider: "claude-code", Title: "Review deployment choice", Lifecycle: state.TaskLifecycleAttention,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-3 * time.Minute), UpdatedAt: now,
		Attention: &state.PublicTaskAttention{Kind: state.AttentionQuestionWaiting, Text: "Question waiting · choose deployment target", At: now},
	}
	complete := state.PublicTask{
		ID: "PRIVATE_COMPLETE_ID", Provider: "codex", Title: "Validate UI", Lifecycle: state.TaskComplete,
		Freshness: state.FreshnessFresh, Confidence: state.TaskConfidenceHigh, StartedAt: now.Add(-12 * time.Minute), UpdatedAt: now,
		Completion: &state.PublicTaskCompletion{Summary: &summary, At: now},
	}
	onlineState := state.PublicState{
		Host: state.PublicHost{ID: "mac-a", DisplayName: "Studio Mac"}, Tasks: []state.PublicTask{working, attention, complete},
		Network: state.PublicNetwork{Quality: state.NetworkDegraded},
		Sources: map[string]state.PublicSourceHealth{
			"codex-hooks":  {Status: state.SourceAvailable, LastSuccessAt: timePtr(now)},
			"claude-hooks": {Status: state.SourceDegraded, Message: "Limited provider capability."},
			"system":       {Status: state.SourceUnavailable, Message: "System sample unavailable."},
			"network":      {Status: state.SourceDegraded, Message: "Network quality degraded."},
			"git":          {Status: state.SourceUnavailable},
			"quota":        {Status: state.SourceUnavailable},
		},
	}
	retainedState := state.PublicState{Host: state.PublicHost{ID: "mac-b", DisplayName: "Laptop"}, Sources: map[string]state.PublicSourceHealth{}}
	staleSnapshot := dashboard.SnapshotStale
	currentSnapshot := dashboard.SnapshotFresh
	lastSeen := now.Add(-31 * time.Minute)
	model := dashboard.State{Hosts: []dashboard.HostSnapshot{
		{ConfiguredHostID: "mac-a", DisplayName: "Studio Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: dashboard.HostStatus("online"), LastSuccessAt: timePtr(now), Message: "Receiving node snapshots."}, SnapshotFreshness: &currentSnapshot, State: &onlineState},
		{ConfiguredHostID: "mac-b", DisplayName: "Laptop", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: dashboard.HostStatus("offline"), LastSuccessAt: timePtr(now.Add(-40 * time.Second)), Message: "Node is not sending snapshots; retained state shown."}, SnapshotFreshness: &staleSnapshot, State: &retainedState},
		{ConfiguredHostID: "mac-c", DisplayName: "Build Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: dashboard.HostStatus("offline"), Message: "Registered node awaiting first snapshot."}},
		{ConfiguredHostID: "mac-d", DisplayName: "Travel Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: dashboard.HostStatus("offline"), LastSuccessAt: &lastSeen, Message: "Node offline."}},
	}}
	body := renderProductFragment(t, model, now)
	for _, required := range []string{">TASKS<", "Studio Mac · mac-a", "Build status board", "WORKING", "READY", "COMPLETE", "Question waiting", summary, "HOST HEALTH", "CPU", "MEMORY", "SWAP", "DISK", "AI SIGNALS"} {
		if !strings.Contains(body, required) {
			t.Fatalf("state-matrix render missing %q", required)
		}
	}
	for _, forbidden := range []string{"PROJECT IDENTITY", "PRIVATE_PROJECT", "Source health", "Provider lifecycle", "NETWORK HEALTH", "SNAPSHOT ·"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Pad rendered forbidden desktop field %q", forbidden)
		}
	}
	for _, private := range []string{"PRIVATE_TASK_ID", "PRIVATE_ATTENTION_ID", "PRIVATE_COMPLETE_ID"} {
		if strings.Contains(body, private) {
			t.Fatalf("opaque task identity leaked: %q", private)
		}
	}
}

func TestProductDashboardExplicitlyRendersNoTasksAndNoNodes(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	emptyState := state.PublicState{Host: state.PublicHost{ID: "mac-a", DisplayName: "Studio Mac"}, Sources: map[string]state.PublicSourceHealth{}}
	current := dashboard.SnapshotFresh
	noTasks := renderProductFragment(t, dashboard.State{Hosts: []dashboard.HostSnapshot{{
		ConfiguredHostID: "mac-a", DisplayName: "Studio Mac", Source: dashboard.HostSource{Kind: dashboard.HostSourceNode, Status: dashboard.HostStatus("online")}, SnapshotFreshness: &current, State: &emptyState,
	}}}, now)
	if !strings.Contains(noTasks, "ALL CLEAR · NO CURRENT TASKS") {
		t.Fatal("no-task state is not explicit")
	}
	noNodes := renderProductFragment(t, dashboard.State{Hosts: []dashboard.HostSnapshot{}}, now)
	for _, required := range []string{"MAC NOT CONNECTED", "CPU", "MEMORY", "SWAP", "DISK", "WEB WATCH · NOT CONNECTED"} {
		if !strings.Contains(noNodes, required) {
			t.Fatalf("zero-node state missing %q", required)
		}
	}
}

func TestProductRefreshScriptPreservesLastDOMAndRecovers(t *testing.T) {
	b, err := templateFS.ReadFile("static/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, required := range []string{
		`container.innerHTML = html`, `setRefreshPaused(true)`, `setRefreshPaused(false)`,
		`window.setTimeout(refresh, delay)`, `last successful server-rendered DOM`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("refresh recovery implementation missing %q", required)
		}
	}
	if strings.Contains(text, `container.innerHTML = ""`) {
		t.Fatal("refresh failure may not erase the last successful DOM")
	}
}

func TestManagedSurfacesUseSharedResponsiveProductSystem(t *testing.T) {
	admin, err := templateFS.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := templateFS.ReadFile("templates/settings.html")
	if err != nil {
		t.Fatal(err)
	}
	css, err := templateFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"admin": string(admin), "settings": string(settings)} {
		for _, required := range []string{`/assets/app.css`, `class="managed-header"`, `class="brand"`, `class="product-nav`} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s surface missing shared product element %q", name, required)
			}
		}
	}
	if strings.Contains(string(settings), ".BinaryPath") || strings.Contains(string(settings), "DevBoard binary") {
		t.Fatal("settings presentation exposes an absolute binary-path surface")
	}
	for _, required := range []string{"@media (max-width: 760px)", "@media (max-width: 520px)", ":focus-visible", "prefers-reduced-motion"} {
		if !strings.Contains(string(css), required) {
			t.Fatalf("responsive/accessibility product CSS missing %q", required)
		}
	}
}

func timePtr(v time.Time) *time.Time { return &v }

func TestNodeStatusAPIIsLoopbackOnlyAndRedactsToken(t *testing.T) {
	path := writeNodeConfig(t, func(cfg *config.Config) {
		cfg.Host.ID = "mac-a"
		cfg.Host.DisplayName = "Mac A"
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-a", Token: settingsTestToken}
	})
	h, err := NewSettingsHandler(SettingsOptions{ConfigPath: path, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := settingsRequest(http.MethodGet, "/api/node/status", nil)
	h.ServeNodeStatus(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	for _, forbidden := range []string{settingsTestToken, `"token"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("node status leaked %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{`"schemaVersion":1`, `"serviceRunning":true`, `"nodeId":"mac-a"`, `"tokenConfigured":true`, `"uplinkRunning":false`, `"lastAttemptAt":null`, `"lastSuccessAt":null`} {
		if !strings.Contains(body, required) {
			t.Fatalf("node status missing %q: %s", required, body)
		}
	}
	req = settingsRequest(http.MethodGet, "/api/node/status", nil)
	req.Host = "attacker.example"
	rec = httptest.NewRecorder()
	h.ServeNodeStatus(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad Host status=%d", rec.Code)
	}
}

type productUIHealth struct {
	value UplinkHealth
}

func (h productUIHealth) UplinkHealth() UplinkHealth { return h.value }

func TestNodeStatusAPIFrozenContract(t *testing.T) {
	attempt := time.Date(2026, 8, 23, 10, 11, 12, 0, time.UTC)
	success := attempt.Add(3 * time.Minute)
	path := writeNodeConfig(t, func(cfg *config.Config) {
		cfg.Host.ID = "mac-a"
		cfg.Host.DisplayName = "Mac A"
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.test", NodeID: "mac-a", Token: settingsTestToken}
	})
	h, err := NewSettingsHandler(SettingsOptions{
		ConfigPath: path,
		HealthSource: productUIHealth{value: UplinkHealth{
			Connected:      true,
			LastAttemptAt:  &attempt,
			LastSuccessAt:  &success,
			LastErrorClass: "",
		}},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	post := settingsRequest(http.MethodPost, "/api/node/status", nil)
	rec := httptest.NewRecorder()
	h.ServeNodeStatus(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeNodeStatus(rec, settingsRequest(http.MethodGet, "/api/node/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d", rec.Code)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{
		"schemaVersion": true, "serviceRunning": true, "nodeId": true, "displayName": true,
		"hubEndpoint": true, "uplinkEnabled": true, "tokenConfigured": true, "uplinkRunning": true,
		"connected": true, "lastAttemptAt": true, "lastSuccessAt": true, "lastErrorClass": true,
	}
	if len(fields) != len(expected) {
		t.Fatalf("field count=%d fields=%v", len(fields), fields)
	}
	for key := range fields {
		if !expected[key] {
			t.Fatalf("unexpected bounded field %q", key)
		}
	}
	var decoded struct {
		Connected       bool      `json:"connected"`
		TokenConfigured bool      `json:"tokenConfigured"`
		LastAttemptAt   time.Time `json:"lastAttemptAt"`
		LastSuccessAt   time.Time `json:"lastSuccessAt"`
		LastErrorClass  string    `json:"lastErrorClass"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Connected || !decoded.TokenConfigured || !decoded.LastAttemptAt.Equal(attempt) || !decoded.LastSuccessAt.Equal(success) || decoded.LastErrorClass != "" {
		t.Fatalf("health propagation=%+v", decoded)
	}
	if strings.Contains(rec.Body.String(), settingsTestToken) || strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("status response leaked token material: %s", rec.Body.String())
	}
}
