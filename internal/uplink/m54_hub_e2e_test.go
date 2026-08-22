package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/state"
)

// This file verifies the real M5.4 node uplink runtime against the real M5.3
// hub receiver: the envelopes the uplink produces must be accepted by the
// frozen wire contract, not merely by a test double. It is the only place the
// uplink package may reference internal/hub, and only from tests.

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// newRealHub starts the frozen M5.3 receiver runtime for mac-a.
func newRealHub(t *testing.T) (*hub.Runtime, string) {
	t.Helper()
	rt, err := hub.NewRuntime([]hub.NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: m54Token},
	}, discardLogger(), nil)
	if err != nil {
		t.Fatalf("hub runtime: %v", err)
	}
	return rt, httptestServer(t, rt)
}

func TestM54RealReceiverAcceptsUplinkStream(t *testing.T) {
	rt, url := newRealHub(t)
	store := m54Store(t)
	cfg := SchedulerConfig{
		HeartbeatInterval: 25 * time.Millisecond,
		RetryBackoff:      []time.Duration{60 * time.Millisecond},
		SlowRetryInterval: 150 * time.Millisecond,
		AdmissionWindow:   30 * time.Second,
		RequestTimeout:    time.Second,
	}
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
	sched := NewScheduler(store, builder, NewClient(url, m54Token, cfg.RequestTimeout), cfg, discardLogger(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)
	defer func() { cancel(); sched.Wait() }()

	// The heartbeat stream must be accepted end to end.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sched.Health().Connected {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !sched.Health().Connected {
		t.Fatalf("uplink never reached connected state against the real receiver")
	}

	// A public state change must reach the hub store through the receiver.
	changed := store.Snapshot()
	changed.Tasks = append(changed.Tasks, state.TaskState{ID: "e2e-task", Provider: "codex", Title: "E2E", StartedAt: m54Base, UpdatedAt: m54Base})
	store.Replace(changed)

	sawTask := false
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawTask {
		dash := rt.Store().Dashboard(time.Now().UTC())
		if len(dash.Hosts) != 1 || dash.Hosts[0].ConfiguredHostID != "mac-a" {
			t.Fatalf("unexpected dashboard assembly: %+v", dash.Hosts)
		}
		if dash.Hosts[0].Source.Status == "online" && dash.Hosts[0].State != nil {
			for _, task := range dash.Hosts[0].State.Tasks {
				if task.ID == "e2e-task" {
					sawTask = true
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !sawTask {
		t.Fatalf("changed public state never reached the hub store")
	}
}

// TestM54RealReceiverConflictTriggersResync drives a genuine 409 through the
// real receiver: a future-dated snapshot is pre-accepted for mac-a, so the
// uplink's fresh session regresses generatedAt and the receiver rejects it
// with 409 (M5.2 §14.6). The uplink must resynchronize (new session, sequence
// 1) and recover once its snapshots no longer regress the accepted time.
//
// Determinism: the first genuine 409 is held by a test-only gate wrapping the
// real receiver until the test has advanced both fake clocks. The scheduler
// therefore cannot observe the conflict — or build the resynchronization
// snapshot — until after the clocks moved, so the resync snapshot always
// carries the advanced instant and the real receiver always accepts it. No
// step depends on goroutine scheduling speed.
func TestM54RealReceiverConflictTriggersResync(t *testing.T) {
	uplinkClock := newM54Clock(m54Base)
	hubClock := newM54Clock(m54Base)

	rt, err := hub.NewRuntime([]hub.NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Enabled: true, Token: m54Token},
	}, discardLogger(), hubClock.Now)
	if err != nil {
		t.Fatalf("hub runtime: %v", err)
	}
	gate := newGatedConflictHandler(rt)
	url := httptestServer(t, gate)
	store := m54Store(t)
	cfg := SchedulerConfig{
		HeartbeatInterval: 25 * time.Millisecond,
		RetryBackoff:      []time.Duration{60 * time.Millisecond},
		SlowRetryInterval: 150 * time.Millisecond,
		AdmissionWindow:   30 * time.Second,
		RequestTimeout:    time.Second,
	}
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, uplinkClock.Now)

	// Pre-accept a snapshot generated 100s in the (hub-tolerated) future.
	// This post goes straight to the receiver: it is the accepted baseline,
	// not the conflict under test.
	pre := mustBuild(t, builder, "11223344556677889900aabbccddeeff", 1)
	pre.State.GeneratedAt = m54Base.Add(100 * time.Second)
	pre.SentAt = pre.State.GeneratedAt
	if code := postRaw(t, rt, mustMarshal(t, pre), m54Token); code != 200 {
		t.Fatalf("pre-accept setup failed: %d", code)
	}

	sched := NewScheduler(store, builder, NewClient(url, m54Token, cfg.RequestTimeout), cfg, discardLogger(), uplinkClock.Now)
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)
	defer func() { cancel(); sched.Wait() }()
	// Registered after the stop defer so cleanup releases any held response
	// before waiting for the scheduler; Release is idempotent.
	defer gate.Release()

	// Wait until the real receiver has produced the genuine 409 for the
	// startup snapshot (clocks still frozen at m54Base).
	select {
	case <-gate.conflictSeen:
	case <-time.After(2 * time.Second):
		t.Fatalf("real receiver never produced the conflict, health %+v", sched.Health())
	}

	// Advance node time past the pre-accepted generation instant while the
	// 409 is still held, then hand the conflict to the scheduler. The
	// resynchronization snapshot is necessarily built with the advanced
	// instant and no longer regresses the accepted state.
	uplinkClock.Advance(110 * time.Second)
	hubClock.Advance(110 * time.Second)
	gate.Release()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sched.Health().Connected {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("uplink never recovered from receiver conflict, health %+v", sched.Health())
}

// gatedConflictHandler wraps the real M5.3 receiver for exactly one
// synchronization purpose: it records the FIRST genuine 409 the receiver
// produces, signals the test, and holds that response until the test releases
// it. Every request — including the held one — is served end to end by the
// real receiver; the gate only controls when the node client gets to see the
// first conflict. Later 409s (none are expected once the clocks advance) pass
// through unheld, so the gate can never deadlock the stream.
type gatedConflictHandler struct {
	inner       http.Handler
	mu          sync.Mutex
	held        bool
	armed       bool
	releaseOnce sync.Once

	conflictSeen chan struct{}
	release      chan struct{}
}

func newGatedConflictHandler(inner http.Handler) *gatedConflictHandler {
	return &gatedConflictHandler{
		inner:        inner,
		conflictSeen: make(chan struct{}, 1),
		release:      make(chan struct{}),
	}
}

func (h *gatedConflictHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := httptest.NewRecorder()
	h.inner.ServeHTTP(rec, r)
	hold := false
	h.mu.Lock()
	if !h.armed && rec.Code == http.StatusConflict {
		h.armed = true
		h.held = true
		hold = true
	}
	h.mu.Unlock()
	if hold {
		// Buffered size 1 and guarded by armed: exactly one non-blocking send.
		h.conflictSeen <- struct{}{}
		<-h.release
		h.mu.Lock()
		h.held = false
		h.mu.Unlock()
	}
	writeRecordedResponse(w, rec)
}

// Release lets a held response continue. Idempotent and safe from test
// cleanup paths.
func (h *gatedConflictHandler) Release() {
	h.releaseOnce.Do(func() { close(h.release) })
}

// writeRecordedResponse copies a recorded real-receiver response — status,
// headers and body — onto the live response writer.
func writeRecordedResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for name, values := range rec.Header() {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(rec.Code)
	if rec.Body.Len() > 0 {
		_, _ = w.Write(rec.Body.Bytes())
	}
}

func mustBuild(t *testing.T, b *SnapshotBuilder, session string, seq uint64) NodeSnapshot {
	t.Helper()
	snap, err := b.Build(session, seq)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return snap
}

func mustMarshal(t *testing.T, snap NodeSnapshot) []byte {
	t.Helper()
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

func postRaw(t *testing.T, rt *hub.Runtime, body []byte, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, hub.SnapshotRoute, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)
	return w.Code
}
