package web

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
)

func TestReceiverAndAdminShareDiagnosticsRing(t *testing.T) {
	cfgPath, tokenFile := adminTestEnv(t)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const nodeToken = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cfg.Nodes.Registered = []config.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Token: nodeToken}}
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	ring := NewDiagnosticsRing(50, "info")
	at := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	rt, err := hub.NewRuntimeWithDiagnostics([]hub.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: nodeToken}}, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return at }, ring)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAdminHandler(AdminOptions{
		ConfigPath: cfgPath, TokenFile: tokenFile, Nodes: rt.Store(), Diagnostics: ring,
		RuntimeReady: true, ProductVersion: "test", GitCommit: "abc123",
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(snapshotBody(t, "mac-a", at)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+nodeToken)
	receiverResponse := httptest.NewRecorder()
	rt.ServeHTTP(receiverResponse, request)
	if receiverResponse.Code != http.StatusOK {
		t.Fatalf("receiver status=%d body=%s", receiverResponse.Code, receiverResponse.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("secret="+adminTestSecret))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d", loginResponse.Code)
	}
	cookie := loginResponse.Result().Cookies()[0]
	logsRequest := httptest.NewRequest(http.MethodGet, "/admin/logs", nil)
	logsRequest.AddCookie(cookie)
	logsResponse := httptest.NewRecorder()
	handler.ServeHTTP(logsResponse, logsRequest)
	body := logsResponse.Body.String()
	for _, event := range []string{"runtime_started", "snapshot_accepted"} {
		if !strings.Contains(body, event) {
			t.Fatalf("shared diagnostics missing %q: %s", event, body)
		}
	}
	for _, forbidden := range []string{"mac-a", nodeToken, "aabbccddeeff00112233445566778899"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, body)
		}
	}
}

func TestReceiverRejectionsReachSharedAdminLogsWithoutRequestData(t *testing.T) {
	cfgPath, tokenFile := adminTestEnv(t)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const nodeToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	cfg.Nodes.Registered = []config.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Token: nodeToken}}
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	ring := NewDiagnosticsRing(50, "info")
	at := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	rt, err := hub.NewRuntimeWithDiagnostics([]hub.NodeConfig{{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: nodeToken}}, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return at }, ring)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAdminHandler(AdminOptions{ConfigPath: cfgPath, TokenFile: tokenFile, Nodes: rt.Store(), Diagnostics: ring, RuntimeReady: true, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}

	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(snapshotBody(t, "mac-a", at))),
		httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(snapshotBody(t, "mac-a", at))),
		httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader([]byte(`{"nodeId":"mac-a","body":"body-marker"}`))),
	}
	// This request is a valid bounded snapshot with only Authorization omitted,
	// so the rejection is specifically the credentials gate.
	requests[0].Header.Set("Content-Type", "application/json")
	// Keep the other rejection causes distinct and authenticated: wrong content
	// type, then invalid envelope with a body marker that must not be logged.
	requests[1].Header.Set("Authorization", "Bearer "+nodeToken)
	requests[1].Header.Set("Content-Type", "text/plain")
	requests[2].Header.Set("Authorization", "Bearer "+nodeToken)
	requests[2].Header.Set("Content-Type", "application/json")
	for _, request := range requests {
		response := httptest.NewRecorder()
		rt.ServeHTTP(response, request)
		if response.Code < http.StatusBadRequest {
			t.Fatalf("rejection status=%d body=%s", response.Code, response.Body.String())
		}
	}

	login := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("secret="+adminTestSecret))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d", loginResponse.Code)
	}
	logsRequest := httptest.NewRequest(http.MethodGet, "/admin/logs", nil)
	logsRequest.AddCookie(loginResponse.Result().Cookies()[0])
	logsResponse := httptest.NewRecorder()
	handler.ServeHTTP(logsResponse, logsRequest)
	body := logsResponse.Body.String()
	if count := strings.Count(body, "snapshot_rejected"); count < 3 {
		t.Fatalf("logs contain %d rejection events, want at least 3: %s", count, body)
	}
	for _, forbidden := range []string{nodeToken, "Authorization", "body-marker", "mac-a", "aabbccddeeff00112233445566778899", "content_type", "envelope_json", "credentials"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("rejection logs leaked %q: %s", forbidden, body)
		}
	}
}
