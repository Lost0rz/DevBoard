package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	cookie := a.login(adminTestSecret)
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/overview" {
		t.Fatalf("canonical admin redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	overview := a.get("/admin/overview", cookie)
	body := overview.Body.String()
	for _, required := range []string{"Overview", "Nodes", "Settings", "Logs", "HUB HEALTH", "Persistent directory", "/display", "test-version", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
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
	cookie := a.login(adminTestSecret)
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
