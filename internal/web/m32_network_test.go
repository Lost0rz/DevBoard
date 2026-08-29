package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestDesktopRendersNetworkHealthWithoutPacketLossLabel(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)
	root := state.LiveInitialState(now, state.HostState{ID: "h"})
	reachable := true
	latency := 43.0
	failure := 0.0
	recv := 1.2 * 1024 * 1024
	send := 0.3 * 1024 * 1024
	root.Network = state.NetworkState{Quality: state.NetworkGood, Reachable: &reachable, ConnectLatencyMs: &latency, ProbeFailurePercent: &failure, ReceiveBytesPerSecond: &recv, SendBytesPerSecond: &send}
	root.Sources["network"] = state.SourceHealth{Status: state.SourceAvailable, Message: "Network health collector is available."}
	server, err := NewServer(state.NewStore(root), state.ProjectionConfig{}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return now }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/display", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"NETWORK · GOOD", "43 ms", "FAIL 0%", "REACHABLE YES", "↓ 1.2 MiB/s", "↑ 307.2 KiB/s", "SOURCE available"} {
		if !strings.Contains(body, want) {
			t.Fatalf("desktop network render missing %q: %s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "packet loss") {
		t.Fatal("desktop misleadingly labels TCP probe failure as packet loss")
	}
}

func TestKindleHostRailRendersResourceMetricState(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)
	base := state.MockInternalState(now, state.HostState{ID: "h"})
	swapPercent := 4.0
	base.System.Swap.PercentUsed = &swapPercent

	withServer, err := NewServer(state.NewStore(base), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	withServer.now = func() time.Time { return now }

	recorder := httptest.NewRecorder()
	withServer.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/kindle/R", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"CPU", "MEMORY", "SWAP", "DISK", "4%"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Kindle host resource metric missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, ">WEB<") {
		t.Fatalf("Kindle host resource rail still renders WEB: %s", body)
	}
}
