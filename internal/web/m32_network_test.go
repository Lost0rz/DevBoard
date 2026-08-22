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

func TestKindlePresentationIsUnchangedByNetworkState(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)
	base := state.MockInternalState(now, state.HostState{ID: "h"})
	withoutNetwork := state.CloneInternalRootState(base)
	withoutNetwork.Network = state.NetworkState{Quality: state.NetworkUnknown}
	delete(withoutNetwork.Sources, "network")

	withServer, err := NewServer(state.NewStore(base), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	withoutServer, err := NewServer(state.NewStore(withoutNetwork), state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	withServer.now = func() time.Time { return now }
	withoutServer.now = func() time.Time { return now }

	request := func(server *Server) string {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/display/kindle?layout=landscape&rotate=none", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		return recorder.Body.String()
	}
	withBody := request(withServer)
	withoutBody := request(withoutServer)
	if withBody != withoutBody {
		t.Fatal("M3.2 network state changed frozen Kindle presentation")
	}
	if strings.Contains(strings.ToUpper(withBody), "NETWORK") {
		t.Fatal("Kindle gained a Network section in M3.2")
	}
}
