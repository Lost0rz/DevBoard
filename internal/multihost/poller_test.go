package multihost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func validPeerPayload(t *testing.T, now time.Time, hostID string) string {
	t.Helper()
	pub := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: hostID, DisplayName: hostID}}
	body, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}
}

func TestPollPeerUsesFixedGETStatePath(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	var gotMethod, gotPath, gotHost string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotMethod, gotPath, gotHost = r.Method, r.URL.Path, r.URL.Host
		return response(http.StatusOK, validPeerPayload(t, now, "peer")), nil
	})}
	pollPeer(context.Background(), client, store, config.PeerConfig{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}, "local", slog.Default(), func() time.Time { return now })
	if gotMethod != http.MethodGet || gotPath != "/api/state" || gotHost != "192.168.1.2:8787" {
		t.Fatalf("unexpected request %s http://%s%s", gotMethod, gotHost, gotPath)
	}
	d := store.Dashboard(state.PublicState{Host: state.PublicHost{ID: "local"}}, now)
	if d.Hosts[1].State == nil || d.Hosts[1].Source.Status != PeerAvailable {
		t.Fatal("valid peer snapshot was not accepted")
	}
}

func TestPeerHTTPClientContract(t *testing.T) {
	client := newPeerHTTPClient()
	if client.Timeout != 1500*time.Millisecond {
		t.Fatalf("timeout = %s", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("redirect policy missing")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://192.168.1.2/api/state", nil)
	if err := client.CheckRedirect(req, []*http.Request{req}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}

func TestPollPeerRejectsMalformedOversizedAndDashboardPayload(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"oversized", strings.Repeat("x", MaxBodyBytes+1)},
		{"dashboard", `{"schemaVersion":1,"stateKind":"dashboard","generatedAt":"2026-08-22T06:00:00Z","hosts":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, tc.body), nil })}
			pollPeer(context.Background(), client, store, config.PeerConfig{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}, "local", slog.Default(), func() time.Time { return now })
			d := store.Dashboard(state.PublicState{Host: state.PublicHost{ID: "local"}}, now)
			if d.Hosts[1].State != nil || d.Hosts[1].Source.Status != PeerDegraded {
				t.Fatalf("invalid payload escaped isolation: %#v", d.Hosts[1])
			}
		})
	}
}

func TestValidatePeerStateIdentityDuplicatesAndClock(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	base := state.PublicState{SchemaVersion: 1, StateKind: "public", GeneratedAt: now, Host: state.PublicHost{ID: "peer"}}
	cases := []struct {
		name   string
		mutate func(*state.PublicState)
		accept bool
	}{
		{"wrong kind", func(p *state.PublicState) { p.StateKind = "dashboard" }, false},
		{"wrong schema", func(p *state.PublicState) { p.SchemaVersion = 2 }, false},
		{"empty host", func(p *state.PublicState) { p.Host.ID = "" }, false},
		{"mismatch", func(p *state.PublicState) { p.Host.ID = "other" }, false},
		{"local collision", func(p *state.PublicState) { p.Host.ID = "local" }, false},
		{"duplicate task", func(p *state.PublicState) { p.Tasks = []state.PublicTask{{ID: "x"}, {ID: "x"}} }, false},
		{"duplicate agent", func(p *state.PublicState) { p.Agents = []state.PublicAgent{{ID: "x"}, {ID: "x"}} }, false},
		{"future tolerated", func(p *state.PublicState) { p.GeneratedAt = now.Add(2 * time.Minute) }, true},
		{"future rejected", func(p *state.PublicState) { p.GeneratedAt = now.Add(2*time.Minute + time.Nanosecond) }, false},
		{"stale accepted", func(p *state.PublicState) { p.GeneratedAt = now.Add(-31 * time.Second) }, true},
		{"too old rejected", func(p *state.PublicState) { p.GeneratedAt = now.Add(-RetentionWindow - time.Nanosecond) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := base
			tc.mutate(&pub)
			_, _, accept := validatePeerState(pub, "peer", "local", store, now)
			if accept != tc.accept {
				t.Fatalf("accept = %v want %v", accept, tc.accept)
			}
		})
	}
}

func TestPollLoopDoesNotOverlapAndCancellationStops(t *testing.T) {
	now := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	store := NewPeerSnapshotStore([]config.PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}})
	var inFlight, maxInFlight, calls int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if current <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, current) {
				break
			}
		}
		defer atomic.AddInt32(&inFlight, -1)
		atomic.AddInt32(&calls, 1)
		select {
		case <-time.After(4 * time.Millisecond):
			return response(http.StatusOK, validPeerPayload(t, now, "peer")), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pollLoop(ctx, client, store, config.PeerConfig{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}, "local", slog.Default(), time.Millisecond, func() time.Time { return now })
	}()
	time.Sleep(15 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll loop did not terminate after cancellation")
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatal("test did not exercise repeated polling")
	}
	if atomic.LoadInt32(&maxInFlight) != 1 {
		t.Fatalf("max concurrent polls = %d", maxInFlight)
	}
}
