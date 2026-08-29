package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
)

func newUnconfiguredAdmin(t *testing.T) (*AdminHandler, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "hub.yaml")
	tokenPath := filepath.Join(dir, "admin.token")
	passwordPath := filepath.Join(dir, "admin.password")
	if err := os.WriteFile(tokenPath, []byte(adminTestSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Runtime.Role = config.RuntimeRoleHub
	cfg.Server.Host = "0.0.0.0"
	cfg.Admin = config.AdminConfig{Enabled: true, TokenFile: tokenPath, PasswordFile: passwordPath}
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	handler, err := NewAdminHandler(AdminOptions{ConfigPath: cfgPath, TokenFile: tokenPath, PasswordFile: passwordPath})
	if err != nil {
		t.Fatal(err)
	}
	return handler, cfgPath, tokenPath, passwordPath
}

func TestAdminFirstRunCreatesPasswordAndRejectsMachineTokenLogin(t *testing.T) {
	handler, _, _, passwordPath := newUnconfiguredAdmin(t)
	get := httptest.NewRequest(http.MethodGet, "/admin", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "Set Admin password") {
		t.Fatalf("first run did not show password setup: %d %s", getResponse.Code, getResponse.Body.String())
	}
	if strings.Contains(getResponse.Body.String(), `name="username"`) || strings.Contains(getResponse.Body.String(), "Admin Secret") {
		t.Fatal("first-run form exposed a username or legacy secret login")
	}

	form := url.Values{"password": {"first-run-password-123"}, "confirm_password": {"first-run-password-123"}}
	setup := httptest.NewRequest(http.MethodPost, "/admin/setup", bytes.NewBufferString(form.Encode()))
	setup.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	if setupResponse.Code != http.StatusSeeOther || setupResponse.Header().Get("Location") != "/admin/overview" {
		t.Fatalf("password setup: %d location=%q body=%s", setupResponse.Code, setupResponse.Header().Get("Location"), setupResponse.Body.String())
	}
	passwordCookie := setupResponse.Result().Cookies()[0]
	if _, configured, err := loadAdminPassword(passwordPath); err != nil || !configured {
		t.Fatalf("password record not created: configured=%v err=%v", configured, err)
	}
	info, err := os.Stat(passwordPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("password file permissions: info=%v err=%v", info, err)
	}
	dashboard := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	dashboard.AddCookie(passwordCookie)
	dashboardResponse := httptest.NewRecorder()
	handler.ServeHTTP(dashboardResponse, dashboard)
	if !strings.Contains(dashboardResponse.Body.String(), "Operational dashboard") || !strings.Contains(dashboardResponse.Body.String(), "Admin password") {
		t.Fatalf("setup session did not open Dashboard: %d %s", dashboardResponse.Code, dashboardResponse.Body.String())
	}

	legacy := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("secret="+adminTestSecret))
	legacy.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	legacyResponse := httptest.NewRecorder()
	handler.ServeHTTP(legacyResponse, legacy)
	if legacyResponse.Code != http.StatusBadRequest || len(legacyResponse.Result().Cookies()) != 0 {
		t.Fatalf("legacy token login was accepted: status=%d cookies=%v", legacyResponse.Code, legacyResponse.Result().Cookies())
	}
}

func TestAdminPasswordAllowsShortNonEmptyPassword(t *testing.T) {
	handler, _, _, _ := newUnconfiguredAdmin(t)
	form := url.Values{"password": {"1234"}, "confirm_password": {"1234"}}
	setup := httptest.NewRequest(http.MethodPost, "/admin/setup", bytes.NewBufferString(form.Encode()))
	setup.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, setup)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/overview" {
		t.Fatalf("short password setup rejected: status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
}

func TestAdminPasswordCanBeChangedFromSettings(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestPassword)
	if cookie == nil {
		t.Fatal("test password did not authenticate")
	}
	csrf := adminCSRF(t, a.get("/admin/settings", cookie).Body.String())
	wrong := a.post("/admin/settings/password", cookie, map[string]string{
		"csrf": csrf, "current_password": "wrong-current-password", "new_password": "new-admin-password-123", "confirm_password": "new-admin-password-123",
	})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	changed := a.post("/admin/settings/password", cookie, map[string]string{
		"csrf": csrf, "current_password": adminTestPassword, "new_password": "new-admin-password-123", "confirm_password": "new-admin-password-123",
	})
	if changed.Code != http.StatusOK || !strings.Contains(changed.Body.String(), "Admin password updated") {
		t.Fatalf("password change: %d %s", changed.Code, changed.Body.String())
	}
	if strings.Contains(changed.Body.String(), "new-admin-password-123") {
		t.Fatal("changed password was echoed by Settings")
	}
	if oldCookie := a.login(adminTestPassword); oldCookie != nil {
		t.Fatal("old password remained valid after change")
	}
	if newCookie := a.login("new-admin-password-123"); newCookie == nil {
		t.Fatal("new password did not authenticate")
	}
}
