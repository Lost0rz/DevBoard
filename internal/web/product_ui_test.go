package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
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
	if !strings.Contains(body, "AI TASKS") || !strings.Contains(body, "data-refresh-seconds") {
		t.Fatal("display omitted the current dashboard fragment shell")
	}
}

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
