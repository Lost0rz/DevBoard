package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
)

const adminTestSecret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func adminTestEnv(t *testing.T) (cfgPath, tokenFile string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "hub.yaml")
	tokenFile = filepath.Join(dir, "admin.token")
	if err := os.WriteFile(tokenFile, []byte(adminTestSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Runtime.Role = config.RuntimeRoleHub
	cfg.Server.Host = "0.0.0.0"
	cfg.Admin = config.AdminConfig{Enabled: true, TokenFile: tokenFile}
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		t.Fatalf("seed hub config: %v", err)
	}
	return cfgPath, tokenFile
}

type adminHarness struct {
	t        *testing.T
	handler  *AdminHandler
	cfgPath  string
	logs     *bytes.Buffer
	restarts *int32
}

func newAdminHarness(t *testing.T) *adminHarness {
	t.Helper()
	cfgPath, tokenFile := adminTestEnv(t)
	rt, err := hub.NewRuntime(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	var restarts int32
	handler, err := NewAdminHandler(AdminOptions{
		ConfigPath:     cfgPath,
		TokenFile:      tokenFile,
		Nodes:          rt.Store(),
		RequestRestart: func() { atomic.AddInt32(&restarts, 1) },
		RuntimeReady:   true,
		ProductVersion: "test-version",
		GitCommit:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Logger:         slog.New(slog.NewTextHandler(logs, nil)),
		Now:            func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("admin handler: %v", err)
	}
	return &adminHarness{t: t, handler: handler, cfgPath: cfgPath, logs: logs, restarts: &restarts}
}

func adminCSRF(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]{64})"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf token in authenticated page:\n%s", body)
	}
	return m[1]
}

// login performs the admin login and returns the session cookie.
func (a *adminHarness) login(secret string) *http.Cookie {
	a.t.Helper()
	form := "secret=" + secret
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "devboard_admin" {
			cookie = c
		}
	}
	return cookie
}

func (a *adminHarness) get(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	a.t.Helper()
	// /admin is now the authenticated redirect entrypoint; historical node
	// page assertions use the compatible canonical child route.
	if path == "/admin" && cookie != nil {
		path = "/admin/nodes"
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a *adminHarness) post(path string, cookie *http.Cookie, fields map[string]string) *httptest.ResponseRecorder {
	a.t.Helper()
	form := bytes.Buffer{}
	first := true
	for k, v := range fields {
		if !first {
			form.WriteString("&")
		}
		first = false
		form.WriteString(k + "=" + v)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.String()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminUnauthenticatedShowsLoginAndProtectsMutations(t *testing.T) {
	a := newAdminHarness(t)
	rec := a.get("/admin", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Admin Secret") {
		t.Fatalf("unauthenticated GET must show the login form, got %d", rec.Code)
	}
	// Mutations without a session are denied.
	rec = a.post("/admin/nodes/add", nil, map[string]string{"node_id": "mac-a"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mutation status=%d, want 401", rec.Code)
	}
}

func TestAdminMachineProvisionIsAuthenticatedAndIdempotent(t *testing.T) {
	a := newAdminHarness(t)
	post := func(nodeID, display, secret string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"nodeId": nodeID, "displayName": display})
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/nodes", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+secret)
		rec := httptest.NewRecorder()
		a.handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("mac-b", "Laptop", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad machine auth status=%d", rec.Code)
	}
	first := post("mac-b", "Laptop", adminTestSecret)
	if first.Code != http.StatusOK {
		t.Fatalf("first provision status=%d body=%s", first.Code, first.Body.String())
	}
	var one struct {
		Created bool   `json:"created"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &one); err != nil || !one.Created || len(one.Token) < 32 {
		t.Fatalf("first provision=%s", first.Body.String())
	}
	second := post("mac-b", "Laptop", adminTestSecret)
	var two struct {
		Created bool   `json:"created"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &two); err != nil || two.Created || two.Token != one.Token {
		t.Fatalf("idempotent provision=%s first=%s", second.Body.String(), first.Body.String())
	}
	cfg, err := config.Load(a.cfgPath)
	if err != nil || len(cfg.Nodes.Registered) != 1 || cfg.Nodes.Registered[0].NodeID != "mac-b" {
		t.Fatalf("registry=%+v err=%v", cfg.Nodes, err)
	}
	if atomic.LoadInt32(a.restarts) != 2 {
		t.Fatalf("restart requests=%d", atomic.LoadInt32(a.restarts))
	}
}

func TestAdminBadSecretRejected(t *testing.T) {
	a := newAdminHarness(t)
	const wrongSecret = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if cookie := a.login(wrongSecret); cookie != nil {
		t.Fatal("bad secret must not mint a session cookie")
	}
	form := "secret=" + wrongSecret
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Invalid admin secret") {
		t.Fatalf("bad secret login: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminAuthenticatedSessionAndCSRF(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	if cookie == nil {
		t.Fatal("valid secret must mint a session cookie")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie hardening missing: %+v", cookie)
	}
	if cookie.Path != "/admin" {
		t.Fatalf("cookie path=%q, want /admin", cookie.Path)
	}
	if strings.Contains(cookie.Value, adminTestSecret) {
		t.Fatal("session cookie must not contain the raw admin secret")
	}
	rec := a.get("/admin", cookie)
	if !strings.Contains(rec.Body.String(), "Add Node") {
		t.Fatalf("authenticated GET must show management page, got %d", rec.Code)
	}

	// Mutation with a valid session but no CSRF is forbidden.
	rec = a.post("/admin/nodes/add", cookie, map[string]string{"node_id": "mac-a", "display_name": "Mac A"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mutation without csrf status=%d, want 403", rec.Code)
	}
}

func TestAdminHTTPSCookieIsSecure(t *testing.T) {
	a := newAdminHarness(t)
	form := "secret=" + adminTestSecret
	req := httptest.NewRequest(http.MethodPost, "https://hub.example.test/admin/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("HTTPS login cookie must set Secure: %+v", cookies)
	}
}

func TestAdminLogoutRequiresSessionCSRFAndClearsCookie(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())

	if rec := a.post("/admin/logout", nil, map[string]string{"csrf": csrf}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logout without session status=%d, want 401", rec.Code)
	}
	if rec := a.post("/admin/logout", cookie, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf status=%d, want 403", rec.Code)
	}
	rec := a.post("/admin/logout", cookie, map[string]string{"csrf": csrf})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid logout status=%d, want 303", rec.Code)
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != "devboard_admin" || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear session cookie: %+v", cleared)
	}
	if cleared[0].Path != "/admin" {
		t.Fatalf("logout cookie path=%q, want /admin", cleared[0].Path)
	}
}

func TestAdminLoginRejectsOversizedForm(t *testing.T) {
	a := newAdminHarness(t)
	const marker = "oversized-admin-login-secret-marker"
	form := "secret=" + marker + strings.Repeat("x", managedFormMaxBytes)
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login status=%d, want 413", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("oversized login form minted a session cookie")
	}
	if strings.Contains(rec.Body.String(), marker) || strings.Contains(a.logs.String(), marker) {
		t.Fatal("oversized login secret was exposed")
	}
}

func TestAdminAddNodeGeneratesValidOneTimeToken(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())

	rec := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a", "display_name": "Mac A", "accent": "amber"})
	if rec.Code != http.StatusOK {
		t.Fatalf("add node: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Copy this token now") {
		t.Fatal("one-time token result must carry the copy warning")
	}
	m := regexp.MustCompile(`class="token">([0-9a-f]{64})<`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("generated token not shown exactly once:\n%s", body)
	}
	token := m[1]

	// The registry persists the new node with that token.
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Nodes.Registered) != 1 || cfg.Nodes.Registered[0].NodeID != "mac-a" || cfg.Nodes.Registered[0].Token != token || cfg.Nodes.Registered[0].Accent != "amber" {
		t.Fatalf("registry not persisted: %+v", cfg.Nodes.Registered)
	}

	// Normal admin pages never show the token again.
	refresh := a.get("/admin", cookie)
	if strings.Contains(refresh.Body.String(), token) {
		t.Fatal("normal admin page exposed the raw node token")
	}
	if atomic.LoadInt32(a.restarts) != 1 {
		t.Fatalf("successful mutation must request one restart, got %d", atomic.LoadInt32(a.restarts))
	}
}

func TestAdminDisableSuccessShowsRestartPending(t *testing.T) {
	a := newAdminHarness(t)
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Nodes.Registered = []config.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Token: m53TokenA}}
	if err := config.SaveAtomic(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())

	rec := a.post("/admin/nodes/disable", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a"})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, required := range []string{
		"Registry change saved",
		"Node mac-a disabled.",
		"Hub is restarting to rebuild the node registry.",
		"Node status will repopulate after the Hub returns and Nodes resume outbound snapshots.",
		"Reload node management",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("disable response missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{"Last Success", "<table"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("restart-pending response rendered operational content %q: %s", forbidden, body)
		}
	}
	updated, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Nodes.Disabled) != 1 || updated.Nodes.Disabled[0] != "mac-a" {
		t.Fatalf("disable was not persisted: %+v", updated.Nodes.Disabled)
	}
	if atomic.LoadInt32(a.restarts) != 1 {
		t.Fatalf("successful disable must request exactly one restart, got %d", *a.restarts)
	}
}

func TestAdminEnableSuccessShowsRestartPending(t *testing.T) {
	a := newAdminHarness(t)
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Nodes.Registered = []config.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Token: m53TokenA}}
	cfg.Nodes.Disabled = []string{"mac-a"}
	if err := config.SaveAtomic(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())

	rec := a.post("/admin/nodes/enable", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a"})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status=%d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, required := range []string{
		"Registry change saved",
		"Node mac-a enabled.",
		"Hub is restarting to rebuild the node registry.",
		"Node status will repopulate after the Hub returns and Nodes resume outbound snapshots.",
		"Reload node management",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("enable response missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{"Last Success", "<table"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("restart-pending response rendered operational content %q: %s", forbidden, body)
		}
	}
	updated, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Nodes.Disabled) != 0 {
		t.Fatalf("enable was not persisted: %+v", updated.Nodes.Disabled)
	}
	if atomic.LoadInt32(a.restarts) != 1 {
		t.Fatalf("successful enable must request exactly one restart, got %d", *a.restarts)
	}
}

// snapshotBody builds one minimal valid NodeSnapshotV1 envelope for the
// receiver-level credential checks.
func snapshotBody(t *testing.T, nodeID string, at time.Time) []byte {
	t.Helper()
	env := map[string]any{
		"schemaVersion": 1,
		"stateKind":     "nodeSnapshot",
		"nodeId":        nodeID,
		"sessionId":     "aabbccddeeff00112233445566778899",
		"sequence":      1,
		"sentAt":        at.UTC().Format(time.RFC3339Nano),
		"state": map[string]any{
			"schemaVersion": 1,
			"stateKind":     "public",
			"generatedAt":   at.UTC().Format(time.RFC3339Nano),
			"host":          map[string]any{"id": nodeID, "displayName": nodeID},
			"sources":       map[string]any{},
		},
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// postSnapshot drives a real reconstructed receiver with the given bearer
// credential and reports the HTTP status.
func postSnapshot(t *testing.T, cfg config.Config, token string) int {
	t.Helper()
	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	entries := make([]hub.NodeConfig, 0, len(cfg.Nodes.Registered))
	disabled := make(map[string]bool, len(cfg.Nodes.Disabled))
	for _, id := range cfg.Nodes.Disabled {
		disabled[id] = true
	}
	for _, node := range cfg.Nodes.Registered {
		entries = append(entries, hub.NodeConfig{NodeID: node.NodeID, DisplayName: node.DisplayName, Enabled: !disabled[node.NodeID], Token: node.Token})
	}
	rt, err := hub.NewRuntime(entries, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(snapshotBody(t, cfg.Nodes.Registered[0].NodeID, at)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	return rec.Code
}

func TestAdminResetTokenRotatesCredentialAtReceiver(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())

	added := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a", "display_name": "Mac A"})
	oldToken := regexp.MustCompile(`class="token">([0-9a-f]{64})<`).FindStringSubmatch(added.Body.String())[1]

	// Old token is live on a reconstructed runtime.
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := postSnapshot(t, cfg, oldToken); code != http.StatusOK {
		t.Fatalf("old credential status=%d, want 200", code)
	}

	reset := a.post("/admin/nodes/reset", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a"})
	newToken := regexp.MustCompile(`class="token">([0-9a-f]{64})<`).FindStringSubmatch(reset.Body.String())[1]
	if newToken == oldToken {
		t.Fatal("reset must generate a different credential")
	}

	cfg, err = config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := postSnapshot(t, cfg, oldToken); code != http.StatusUnauthorized {
		t.Fatalf("old credential after reset status=%d, want 401", code)
	}
	if code := postSnapshot(t, cfg, newToken); code != http.StatusOK {
		t.Fatalf("new credential after reset status=%d, want 200", code)
	}
}

func TestAdminDisableSurvivesReconstructedRuntime(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	added := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a", "display_name": "Mac A"})
	token := regexp.MustCompile(`class="token">([0-9a-f]{64})<`).FindStringSubmatch(added.Body.String())[1]

	rec := a.post("/admin/nodes/disable", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("disable mutation: %d %s", rec.Code, rec.Body.String())
	}
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := postSnapshot(t, cfg, token); code != http.StatusForbidden {
		t.Fatalf("disabled node credential status=%d, want 403", code)
	}

	// Re-enable restores ingestion.
	rec = a.post("/admin/nodes/enable", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a"})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable mutation: %d", rec.Code)
	}
	cfg, err = config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := postSnapshot(t, cfg, token); code != http.StatusOK {
		t.Fatalf("re-enabled credential status=%d, want 200", code)
	}
}

func TestAdminInvalidMutationChangesNothing(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	before, _ := os.ReadFile(a.cfgPath)

	// Duplicate id and invalid grammar are both rejected by validation.
	rec := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "bad id!", "display_name": "X"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "validation failed") {
		t.Fatalf("expected validation error, got %d %s", rec.Code, rec.Body.String())
	}
	after, _ := os.ReadFile(a.cfgPath)
	if string(before) != string(after) {
		t.Fatal("rejected mutation must not touch the config file")
	}
	if atomic.LoadInt32(a.restarts) != 0 {
		t.Fatal("rejected mutation must not request a restart")
	}
}

func TestAdminMutationRejectsOversizedForm(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	before, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "oversized-admin-mutation-secret-marker"
	rec := a.post("/admin/nodes/add", cookie, map[string]string{
		"csrf":         csrf,
		"node_id":      "mac-a",
		"display_name": marker + strings.Repeat("x", managedFormMaxBytes),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized mutation status=%d, want 413", rec.Code)
	}
	after, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("oversized admin mutation changed the config")
	}
	if atomic.LoadInt32(a.restarts) != 0 {
		t.Fatal("oversized admin mutation requested a restart")
	}
	if strings.Contains(rec.Body.String(), marker) || strings.Contains(a.logs.String(), marker) {
		t.Fatal("oversized admin mutation exposed submitted body material")
	}
}

func TestAdminAtomicWriteFailureSkipsRestart(t *testing.T) {
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	before, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	a.handler.opts.SaveConfig = func(string, config.Config) error { return errors.New("synthetic write failure") }
	rec := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a", "display_name": "Mac A"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Registry change rejected") {
		t.Fatalf("write failure response: %d %s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed atomic save mutated the config")
	}
	if atomic.LoadInt32(a.restarts) != 0 {
		t.Fatal("failed atomic save requested a restart")
	}
}

func TestAdminSecretsNeverLogged(t *testing.T) {
	a := newAdminHarness(t)
	// Failed login, successful login, add-node with one-time token.
	_ = a.login("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	cookie := a.login(adminTestSecret)
	csrf := adminCSRF(t, a.get("/admin", cookie).Body.String())
	added := a.post("/admin/nodes/add", cookie, map[string]string{"csrf": csrf, "node_id": "mac-a", "display_name": "Mac A"})
	token := regexp.MustCompile(`class="token">([0-9a-f]{64})<`).FindStringSubmatch(added.Body.String())[1]

	logs := a.logs.String()
	for _, forbidden := range []string{adminTestSecret, token} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("admin log leaked a secret: %q", forbidden)
		}
	}
}

func TestAdminSessionSurvivesHandlerRestart(t *testing.T) {
	// The session cookie is signed by the secret file, so a hub restart
	// (handler reconstruction with the same options) keeps the session.
	a := newAdminHarness(t)
	cookie := a.login(adminTestSecret)

	cfgPath, tokenFile := adminTestEnv(t)
	_ = cfgPath
	rt, _ := hub.NewRuntime(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	restarted, err := NewAdminHandler(AdminOptions{
		ConfigPath: a.cfgPath,
		TokenFile:  tokenFile,
		Nodes:      rt.Store(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:        func() time.Time { return time.Date(2026, 8, 23, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/nodes", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	restarted.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Add Node") {
		t.Fatalf("session did not survive restart: %d", rec.Code)
	}
}

func TestAdminConstructorRejectsBadSecretFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewAdminHandler(AdminOptions{ConfigPath: filepath.Join(dir, "c.yaml"), TokenFile: filepath.Join(dir, "missing.token")}); err == nil {
		t.Fatal("missing secret file must be rejected")
	}
	short := filepath.Join(dir, "short.token")
	if err := os.WriteFile(short, []byte("tooshort"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdminHandler(AdminOptions{ConfigPath: filepath.Join(dir, "c.yaml"), TokenFile: short}); err == nil {
		t.Fatal("short secret must be rejected")
	}
	insecure := filepath.Join(dir, "insecure.token")
	if err := os.WriteFile(insecure, []byte(adminTestSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(insecure, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdminHandler(AdminOptions{ConfigPath: filepath.Join(dir, "c.yaml"), TokenFile: insecure}); err == nil {
		t.Fatal("group/world-readable secret file must be rejected")
	}
	malformed := filepath.Join(dir, "malformed.token")
	if err := os.WriteFile(malformed, []byte("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdminHandler(AdminOptions{ConfigPath: filepath.Join(dir, "c.yaml"), TokenFile: malformed}); err == nil {
		t.Fatal("non-hex secret file must be rejected")
	}
}
