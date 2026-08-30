package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lost0rz/DevBoard/internal/agentquota"
	"github.com/Lost0rz/DevBoard/internal/config"
)

func TestAdminConsoleRoutesAndCanonicalRedirect(t *testing.T) {
	a := newAdminHarness(t)
	for _, path := range []string{"/admin/overview", "/admin/nodes", "/admin/settings", "/admin/logs"} {
		rec := a.get(path, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sign in to Hub Admin") {
			t.Fatalf("unauthenticated %s: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	cookie := a.login(adminTestPassword)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/overview" {
		t.Fatalf("canonical admin redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	overview := a.get("/admin/overview", cookie)
	body := overview.Body.String()
	for _, required := range []string{"Dashboard", "Nodes", "Settings", "CONNECTION STATUS", "Persistent directory", "/display", "test-version", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if !strings.Contains(body, required) {
			t.Fatalf("overview missing %q: %s", required, body)
		}
	}
	if strings.Contains(body, a.cfgPath) || strings.Contains(body, adminTestSecret) {
		t.Fatal("overview exposed a private path or secret")
	}
}

func TestAdminOperatorSettingsAllowListAndAtomicRestart(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	csrf := adminCSRF(t, a.get("/admin/settings", cookie).Body.String())
	rec := a.post("/admin/settings", cookie, map[string]string{
		"csrf": csrf, "console_refresh_seconds": "30", "diagnostics_min_level": "warn", "diagnostics_capacity": "350",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Operator settings saved") {
		t.Fatalf("settings save: %d %s", rec.Code, rec.Body.String())
	}
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Operator.ConsoleRefreshSeconds != 30 || cfg.Operator.DiagnosticsMinLevel != "warn" || cfg.Operator.DiagnosticsCapacity != 350 {
		t.Fatalf("settings not persisted: %+v", cfg.Operator)
	}
	if *a.restarts != 1 {
		t.Fatalf("restart count=%d want 1", *a.restarts)
	}
	for _, name := range []string{"unknown", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			var request *http.Request
			if name == "duplicate" {
				form := "csrf=" + csrf + "&console_refresh_seconds=30&diagnostics_min_level=warn&diagnostics_capacity=350&diagnostics_capacity=351"
				request = httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				form := "csrf=" + csrf + "&console_refresh_seconds=30&diagnostics_min_level=warn&diagnostics_capacity=350&host_port=9999"
				request = httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			request.AddCookie(cookie)
			out := httptest.NewRecorder()
			a.handler.ServeHTTP(out, request)
			if out.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if *a.restarts != 1 {
				t.Fatalf("invalid settings requested restart: %d", *a.restarts)
			}
		})
	}
	invalidRange := a.post("/admin/settings", cookie, map[string]string{
		"csrf": csrf, "console_refresh_seconds": "4", "diagnostics_min_level": "warn", "diagnostics_capacity": "350",
	})
	if invalidRange.Code != http.StatusBadRequest || *a.restarts != 1 {
		t.Fatalf("invalid range changed state: status=%d restarts=%d", invalidRange.Code, *a.restarts)
	}
	a.handler.opts.SaveConfig = func(string, config.Config) error { return errSyntheticSettingsSave }
	failed := a.post("/admin/settings", cookie, map[string]string{
		"csrf": csrf, "console_refresh_seconds": "30", "diagnostics_min_level": "warn", "diagnostics_capacity": "350",
	})
	if failed.Code != http.StatusBadRequest || *a.restarts != 1 {
		t.Fatalf("save failure changed state: status=%d restarts=%d", failed.Code, *a.restarts)
	}
}

func TestAdminDisplayPathsPersistAndExposeFixedServerLinks(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	body := a.get("/admin", cookie).Body.String()
	csrf := adminCSRF(t, body)
	rec := a.post("/admin/settings/display", cookie, map[string]string{
		"csrf": csrf, "pad_path": "/wall", "kindle_right_path": "/paper/right", "kindle_left_path": "/paper/left",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Display paths saved") {
		t.Fatalf("display settings save: %d %s", rec.Code, rec.Body.String())
	}
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Display.PadPath != "/wall" || cfg.Display.KindleRightPath != "/paper/right" || cfg.Display.KindleLeftPath != "/paper/left" {
		t.Fatalf("display paths not persisted: %+v", cfg.Display)
	}
	if *a.restarts != 1 {
		t.Fatalf("display path save requested %d restarts, want 1", *a.restarts)
	}
	for _, required := range []string{"http://example.com/wall", "http://example.com/paper/right", "http://example.com/paper/left"} {
		if !strings.Contains(rec.Body.String(), required) {
			t.Fatalf("display settings response missing %q", required)
		}
	}
}

func TestAdminAgentQuotaSettingsPersistPrivateKeyAndSchedule(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	rec := a.post("/admin/settings/agent-quota", cookie, map[string]string{
		"csrf": "" + csrf, "agent_quota_enabled": "true", "agent_quota_provider": "glm",
		"agent_quota_endpoint": "https://example.invalid/v4/chat/completions", "agent_quota_model": "glm-test",
		"agent_quota_schedules": "05:00, 10:00, 15:00", "agent_quota_api_key": "glm-test-secret",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Agent quota schedule saved") {
		t.Fatalf("agent quota save: %d %s", rec.Code, rec.Body.String())
	}
	for _, required := range []string{"data-schedule-editor", "Add time", "Queue activation test", "/assets/app.css"} {
		if !strings.Contains(rec.Body.String(), required) {
			t.Fatalf("agent quota response missing %q", required)
		}
	}
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AgentQuota.Enabled || strings.Join(cfg.AgentQuota.Schedules, ",") != "05:00,10:00,15:00" || cfg.AgentQuota.Model != "glm-test" {
		t.Fatalf("agent quota config not persisted: %+v", cfg.AgentQuota)
	}
	keyPath := filepath.Join(filepath.Dir(a.cfgPath), "agent-quota.key")
	key, err := os.ReadFile(keyPath)
	if err != nil || strings.TrimSpace(string(key)) != "glm-test-secret" {
		t.Fatalf("agent quota key not persisted safely: %q err=%v", key, err)
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("agent quota key mode: info=%v err=%v", info, err)
	}
	if strings.Contains(rec.Body.String(), "glm-test-secret") {
		t.Fatal("agent quota response leaked API key")
	}
	if *a.restarts != 0 {
		t.Fatalf("agent quota save requested %d restarts, want 0", *a.restarts)
	}
}

func TestAdminAgentQuotaSettingsDoNotRestartHub(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	rec := a.post("/admin/settings/agent-quota", cookie, map[string]string{
		"csrf": csrf, "agent_quota_enabled": "true", "agent_quota_provider": "glm",
		"agent_quota_endpoint": "https://example.invalid/v4/chat/completions", "agent_quota_model": "glm-test",
		"agent_quota_schedule": "05:00", "agent_quota_api_key": "glm-test-secret",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "independent activator") {
		t.Fatalf("agent quota save: %d %s", rec.Code, rec.Body.String())
	}
	if *a.restarts != 0 {
		t.Fatalf("agent quota save restarted hub: %d", *a.restarts)
	}
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil || !cfg.AgentQuota.Enabled || cfg.AgentQuota.Model != "glm-test" || strings.Join(cfg.AgentQuota.Schedules, ",") != "05:00" {
		t.Fatalf("persisted config=%+v err=%v", cfg.AgentQuota, err)
	}
}

func TestAdminAgentQuotaRepeatedScheduleFieldsPreserveOrder(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	form := "csrf=" + csrf + "&agent_quota_enabled=true&agent_quota_provider=glm" +
		"&agent_quota_endpoint=https%3A%2F%2Fexample.invalid%2Fv4%2Fchat%2Fcompletions" +
		"&agent_quota_model=glm-test&agent_quota_schedule=15%3A00&agent_quota_schedule=05%3A00&agent_quota_api_key="
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/agent-quota", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeated schedule save: status=%d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.AgentQuota.Schedules, ",") != "15:00,05:00" {
		t.Fatalf("schedule order=%v", cfg.AgentQuota.Schedules)
	}
}

func TestAdminAgentQuotaTestQueuesIndependentActivator(t *testing.T) {
	a := newAdminHarness(t)
	cfg, err := configLoadForTest(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AgentQuota = config.AgentQuotaConfig{
		Enabled: true, Provider: "glm", Endpoint: "https://example.invalid/v4/chat/completions", Model: "glm-test", Schedules: []string{"05:00"},
	}
	if err := config.SaveAtomic(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(filepath.Dir(a.cfgPath), "agent-quota.key")
	if err := agentquota.SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}

	cookie := a.login(adminTestPassword)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	queued := a.post("/admin/settings/agent-quota/test", cookie, map[string]string{"csrf": csrf})
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), "QUEUED · WAITING FOR ACTIVATOR") {
		t.Fatalf("test was not queued: status=%d body=%s", queued.Code, queued.Body.String())
	}
	if _, claimed, err := agentquota.ClaimManualRequest(agentquota.ControlFile(a.cfgPath)); err != nil || !claimed {
		t.Fatalf("manual request missing or invalid: claimed=%v err=%v", claimed, err)
	}
	duplicate := a.post("/admin/settings/agent-quota/test", cookie, map[string]string{"csrf": csrf})
	if duplicate.Code != http.StatusOK {
		t.Fatalf("second test should queue after first was claimed: status=%d", duplicate.Code)
	}
	if *a.restarts != 0 {
		t.Fatalf("manual test unexpectedly requested restart: %d", *a.restarts)
	}
}

func TestAdminAgentQuotaActivationLogIsVisible(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	for _, event := range []agentquota.Event{
		{Code: "activation_due", Reason: "scheduled_time", Trigger: "scheduled"},
		{Code: "activation_attempt", Reason: "http_request", Trigger: "scheduled"},
		{Code: "activation_failed", Reason: "http_status", Trigger: "scheduled", HTTPStatus: 401},
	} {
		if err := a.audit.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	body := a.get("/admin/overview", cookie).Body.String()
	for _, required := range []string{"Activation checks", "activation_failed", "HTTP 401", "PERSISTENT AUDIT"} {
		if !strings.Contains(body, required) {
			t.Fatalf("activation log missing %q: %s", required, body)
		}
	}
}

func TestDiagnosticsRingIsBoundedFilteredAndRedacted(t *testing.T) {
	ring := NewDiagnosticsRing(2, "info")
	ring.Record("info", "admin", "login_accepted")
	ring.Record("warn", "admin", "login_rejected")
	ring.Record("info", "admin", "registry_saved")
	ring.Record("error", "not-allow-listed", "raw_secret")
	entries := ring.Query("", "", 10)
	if len(entries) != 2 || entries[0].EventCode != "registry_saved" || entries[1].EventCode != "login_rejected" {
		t.Fatalf("ring=%+v", entries)
	}
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Message), "secret") || strings.Contains(entry.Message, "/") {
			t.Fatalf("diagnostic leaked sensitive material: %+v", entry)
		}
	}
	if got := ring.Query("warn", "admin", 10); len(got) != 1 || got[0].EventCode != "login_rejected" {
		t.Fatalf("filtered ring=%+v", got)
	}
}

func TestAgentQuotaDiagnosticsExplainSafeFailureReasons(t *testing.T) {
	ring := NewDiagnosticsRing(20, "info")
	ring.RecordAgentQuota(agentquota.Event{Code: "activation_due", Reason: "scheduled_time"})
	ring.RecordAgentQuota(agentquota.Event{Code: "activation_attempt", Reason: "http_request"})
	ring.RecordAgentQuota(agentquota.Event{Code: "activation_failed", Reason: "http_status", HTTPStatus: 429})
	entries := ring.Query("", "agent-quota", 20)
	if len(entries) != 3 {
		t.Fatalf("agent quota diagnostics=%+v", entries)
	}
	if entries[0].Detail != "HTTP 429" || entries[0].EventCode != "activation_failed" {
		t.Fatalf("failure detail=%+v", entries[0])
	}
	for _, entry := range entries {
		if strings.Contains(entry.Detail, "Bearer") || strings.Contains(entry.Detail, "secret") {
			t.Fatalf("diagnostic detail leaked credential material: %+v", entry)
		}
	}
}

func TestAdminConstructorRejectsTokenSymlink(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.SaveAtomic(cfgPath, func() config.Config {
		cfg := config.Defaults()
		cfg.Runtime.Role = config.RuntimeRoleHub
		cfg.Server.Host = "0.0.0.0"
		return cfg
	}()); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(dir, "real.token")
	if err := os.WriteFile(regular, []byte(adminTestSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "admin.token")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdminHandler(AdminOptions{ConfigPath: cfgPath, TokenFile: link}); err == nil {
		t.Fatal("admin constructor followed token symlink")
	}
}

func configLoadForTest(path string) (config.Config, error) { return config.Load(path) }

var errSyntheticSettingsSave = errors.New("synthetic settings save failure")
