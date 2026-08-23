package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/state"
)

// M5.3.1: the production HUB constructor must refuse to build a non-mock hub
// server without a push runtime. Accepting it would let the dashboard silently
// degrade into the legacy pull PeerSnapshotStore path.
func TestM531HubServerRequiresPushRuntimeWhenNotMock(t *testing.T) {
	if _, err := NewHubServer(state.ProjectionConfig{}, false, nil, nil, 2); err == nil {
		t.Fatal("non-mock hub server without push runtime unexpectedly allowed")
	}
}

// The historical mock hub (--mock on the hub role) owns no runtime authority;
// its dashboard stays the deterministic synthetic scenario and binds neither
// the node store nor any legacy peer store.
func TestM531MockHubServerWithoutRuntimeStaysSynthetic(t *testing.T) {
	s, err := NewHubServer(state.ProjectionConfig{}, true, nil, nil, 2)
	if err != nil {
		t.Fatalf("mock hub without runtime: %v", err)
	}
	if s.nodes != nil || s.peers != nil {
		t.Fatalf("mock hub must bind no runtime authority: nodes=%v peers=%v", s.nodes, s.peers)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d", rec.Code)
	}
	var dashboard struct {
		Hosts []struct {
			ConfiguredHostID string `json:"configuredHostId"`
			Source           struct {
				Kind string `json:"kind"`
			} `json:"source"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Hosts) != 2 {
		t.Fatalf("mock hub hosts=%d", len(dashboard.Hosts))
	}
	for _, host := range dashboard.Hosts {
		if host.Source.Kind != "peer" || host.ConfiguredHostID == "local" {
			t.Fatalf("mock hub must stay synthetic peer-only: %+v", host)
		}
	}
}

// M5.3.1 runtime authority pin: a production hub server starts the receiver,
// binds the push node store as its only dashboard authority, holds no legacy
// PeerSnapshotStore, and renders push-native node wrappers only — including
// for a registered node that never pushed.
func TestM531ProductionHubServerBindsPushAuthorityOnly(t *testing.T) {
	entries := []hub.NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: m53TokenA},
		{NodeID: "mac-b", DisplayName: "Mac B", Enabled: true, Token: m53TokenB},
	}
	s, rt, _ := m53HubServer(t, entries, m53WebBase)

	if s.peers != nil {
		t.Fatal("production hub server must not hold the legacy peer snapshot store")
	}
	if s.nodes == nil || s.nodes != rt.Store() {
		t.Fatal("production hub server must bind the push node store as its only dashboard authority")
	}

	// Receiver authority is live on the frozen machine write route.
	rec := m53PostSnapshot(t, s, m53SnapshotBody("mac-a", "aabbccddeeff00112233445566778899", 1, m53WebBase), m53TokenA)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot push status=%d body=%q", rec.Code, rec.Body.String())
	}

	dashRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(dashRec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashRec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d", dashRec.Code)
	}
	var dashboard struct {
		Hosts []struct {
			ConfiguredHostID string `json:"configuredHostId"`
			Source           struct {
				Kind   string `json:"kind"`
				Status string `json:"status"`
			} `json:"source"`
			State any `json:"state"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(dashRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Hosts) != 2 {
		t.Fatalf("production hub hosts=%d", len(dashboard.Hosts))
	}
	byID := make(map[string]struct {
		Kind   string
		Status string
		State  any
	}, len(dashboard.Hosts))
	for _, host := range dashboard.Hosts {
		if host.Source.Kind != "node" {
			t.Fatalf("non-node wrapper in production hub dashboard: %+v", host)
		}
		byID[host.ConfiguredHostID] = struct {
			Kind   string
			Status string
			State  any
		}{host.Source.Kind, host.Source.Status, host.State}
	}
	if got := byID["mac-a"]; got.Status != "online" || got.State == nil {
		t.Fatalf("pushed mac-a must be online with state: %+v", got)
	}
	if got := byID["mac-b"]; got.Status != "offline" || got.State != nil {
		t.Fatalf("never-pushing mac-b must be an offline wrapper without state: %+v", got)
	}
}

// M5.3.1 wire-compat pin: the degenerate legacy hub constructor (no runtime,
// no peer store) must still render the frozen empty dashboard envelope with
// an empty hosts array on the wire, never a null one.
func TestM531DegenerateLegacyHubRendersEmptyHostsArray(t *testing.T) {
	s, err := NewRoleServer(nil, state.ProjectionConfig{}, false, nil, nil, config.RuntimeRoleHub, 2)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"hosts":[]`) {
		t.Fatalf("empty hub dashboard must render an empty hosts array: %s", rec.Body.String())
	}
}
