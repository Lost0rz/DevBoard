package web

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

const settingsTestToken = "node-token-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func writeNodeConfig(t *testing.T, mutate func(*config.Config)) string {
	t.Helper()
	cfg := config.Defaults()
	cfg.Host.ID = "mac-a"
	cfg.Host.DisplayName = "Mac A"
	cfg.Uplink = config.UplinkConfig{Enabled: false, Token: "", Endpoint: ""}
	if mutate != nil {
		mutate(&cfg)
	}
	path := filepath.Join(t.TempDir(), "node.yaml")
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return path
}

func newSettingsForTest(t *testing.T, path string, health UplinkHealthSource, restart *int32) *SettingsHandler {
	t.Helper()
	h, err := NewSettingsHandler(SettingsOptions{
		ConfigPath:   path,
		HealthSource: health,
		RequestRestart: func() {
			if restart != nil {
				atomic.AddInt32(restart, 1)
			}
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("settings handler: %v", err)
	}
	return h
}

func settingsCSRF(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]{64})"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf token in page:\n%s", body)
	}
	return m[1]
}

func settingsRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1:8787"
	return req
}

func TestSettingsGetRedactsToken(t *testing.T) {
	path := writeNodeConfig(t, func(c *config.Config) {
		c.Uplink.Token = settingsTestToken
	})
	h := newSettingsForTest(t, path, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, settingsTestToken) {
		t.Fatal("settings page leaked the configured token")
	}
	if !strings.Contains(body, "configured") {
		t.Fatal("settings page must indicate the token is configured")
	}
	if strings.Contains(body, `name="token" value="`+settingsTestToken) {
		t.Fatal("token input must never carry the configured value")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", rec.Header().Get("Cache-Control"))
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("settings must not enable CORS, got %q", got)
	}
}

func TestSettingsTokenConfiguredIndicator(t *testing.T) {
	without := newSettingsForTest(t, writeNodeConfig(t, nil), nil, nil)
	rec := httptest.NewRecorder()
	without.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatal("expected 'not configured' indicator without a token")
	}

	with := newSettingsForTest(t, writeNodeConfig(t, func(c *config.Config) {
		c.Uplink.Token = settingsTestToken
	}), nil, nil)
	rec = httptest.NewRecorder()
	with.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	if !strings.Contains(rec.Body.String(), "configured — leave blank to keep") {
		t.Fatal("expected 'configured' indicator with a token")
	}
}

func settingsPostForm(t *testing.T, h *SettingsHandler, csrf string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	if csrf != "" {
		form.Set("csrf", csrf)
	}
	for k, v := range fields {
		form.Set(k, v)
	}
	req := settingsRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSettingsBlankTokenPreservesConfiguredToken(t *testing.T) {
	path := writeNodeConfig(t, func(c *config.Config) {
		c.Uplink.Token = settingsTestToken
	})
	var restarts int32
	h := newSettingsForTest(t, path, nil, &restarts)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	csrf := settingsCSRF(t, rec.Body.String())

	// Enable the uplink with a valid endpoint but leave the token blank.
	rec = settingsPostForm(t, h, csrf, map[string]string{
		"node_id": "mac-a", "display_name": "Mac A", "endpoint": "http://192.0.2.10:8787", "uplink": "on",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Settings saved") {
		t.Fatalf("save failed: %d %s", rec.Code, rec.Body.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Uplink.Token != settingsTestToken {
		t.Fatalf("blank token must preserve the configured credential, got %q", cfg.Uplink.Token)
	}
	if !cfg.Uplink.Enabled || cfg.Uplink.Endpoint != "http://192.0.2.10:8787" {
		t.Fatalf("unexpected saved uplink config: %+v", cfg.Uplink)
	}
	if atomic.LoadInt32(&restarts) != 1 {
		t.Fatalf("successful save must request exactly one restart, got %d", restarts)
	}
}

func TestSettingsReplacementTokenChangesCredential(t *testing.T) {
	path := writeNodeConfig(t, func(c *config.Config) {
		c.Uplink.Token = settingsTestToken
	})
	h := newSettingsForTest(t, path, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	csrf := settingsCSRF(t, rec.Body.String())

	const replacement = "node-token-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rec = settingsPostForm(t, h, csrf, map[string]string{
		"node_id": "mac-a", "display_name": "Mac A", "endpoint": "http://192.0.2.10:8787", "uplink": "on", "token": replacement,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), replacement) {
		t.Fatal("replaced token must not be echoed in the response")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Uplink.Token != replacement {
		t.Fatalf("token replacement not persisted, got %q", cfg.Uplink.Token)
	}
}

func TestSettingsInvalidPostMutatesNothingAndSkipsRestart(t *testing.T) {
	path := writeNodeConfig(t, func(c *config.Config) {
		c.Uplink.Token = settingsTestToken
	})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restarts int32
	h := newSettingsForTest(t, path, nil, &restarts)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	csrf := settingsCSRF(t, rec.Body.String())

	// Invalid: token far too short for the frozen credential grammar.
	rec = settingsPostForm(t, h, csrf, map[string]string{
		"node_id": "mac-a", "display_name": "Mac A", "endpoint": "http://192.0.2.10:8787", "uplink": "on", "token": "short",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "uplink.token") {
		t.Fatalf("expected bounded validation error, got %d %s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid POST must not mutate the config file")
	}
	if atomic.LoadInt32(&restarts) != 0 {
		t.Fatal("failed save must not request a restart")
	}
}

func TestSettingsAtomicWriteFailureSkipsRestart(t *testing.T) {
	path := writeNodeConfig(t, nil)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restarts int32
	h := newSettingsForTest(t, path, nil, &restarts)
	h.opts.SaveConfig = func(string, config.Config) error { return errors.New("synthetic write failure") }
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	csrf := settingsCSRF(t, rec.Body.String())
	rec = settingsPostForm(t, h, csrf, map[string]string{"node_id": "mac-a", "display_name": "Mac A"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "synthetic write failure") {
		t.Fatalf("write failure response: %d %s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed atomic save mutated the config")
	}
	if atomic.LoadInt32(&restarts) != 0 {
		t.Fatal("failed atomic save requested a restart")
	}
}

func TestSettingsRequiresCSRF(t *testing.T) {
	path := writeNodeConfig(t, nil)
	h := newSettingsForTest(t, path, nil, nil)
	for _, csrf := range []string{"", "wrong"} {
		rec := settingsPostForm(t, h, csrf, map[string]string{"node_id": "mac-a"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("csrf=%q: status=%d, want 403", csrf, rec.Code)
		}
	}
}

func TestSettingsUnavailableOnNonLoopbackBind(t *testing.T) {
	path := writeNodeConfig(t, func(c *config.Config) {
		c.Server.Host = "0.0.0.0"
	})
	h := newSettingsForTest(t, path, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-loopback bind must disable settings, got %d", rec.Code)
	}
}

func TestSettingsRejectsNonLoopbackRequestHost(t *testing.T) {
	path := writeNodeConfig(t, nil)
	h := newSettingsForTest(t, path, nil, nil)
	for _, authority := range []string{"attacker.example", "attacker.example:8787", "192.0.2.20:8787"} {
		req := settingsRequest(http.MethodGet, "/settings", nil)
		req.Host = authority
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("host=%q status=%d, want 403", authority, rec.Code)
		}
	}
	for _, authority := range []string{"localhost:8787", "LOCALHOST", "127.0.0.1:8787", "[::1]:8787"} {
		if !LoopbackRequestHost(authority) {
			t.Fatalf("loopback authority %q rejected", authority)
		}
	}
}

type fakeHealth struct{ h UplinkHealth }

func (f fakeHealth) UplinkHealth() UplinkHealth { return f.h }

func TestSettingsShowsUplinkHealthFields(t *testing.T) {
	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	path := writeNodeConfig(t, nil)
	h := newSettingsForTest(t, path, fakeHealth{UplinkHealth{Connected: false, LastAttemptAt: &at, LastErrorClass: "transient"}}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, settingsRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	for _, want := range []string{"Disconnected", "transient", at.Format(time.RFC3339)} {
		if !strings.Contains(body, want) {
			t.Fatalf("health panel missing %q", want)
		}
	}
}

// Handlers must never terminate the process themselves: restarts belong to
// the serve loop.
func TestManagedHandlersNeverCallOsExit(t *testing.T) {
	for _, name := range []string{"settings.go", "admin.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "os.Exit") {
			t.Fatalf("%s must never call os.Exit", name)
		}
	}
}
