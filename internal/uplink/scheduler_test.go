package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// m54Clock is a fake clock for gating decisions: the scheduler's evaluation
// points (ticker, store wake) stay real, while all eligibility comparisons
// and envelope timestamps read this clock.
type m54Clock struct {
	mu sync.Mutex
	t  time.Time
}

func newM54Clock(at time.Time) *m54Clock { return &m54Clock{t: at} }

func (c *m54Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *m54Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testHarness wires a scheduler against a recording HTTP hub.
type testHarness struct {
	t       *testing.T
	store   *state.Store
	handler *recordingHandler
	server  serverURL
	cfg     SchedulerConfig
	sched   *Scheduler
	cancel  context.CancelFunc
	nowFn   func() time.Time
}

type serverURL struct{ url string }

func newTestConfig() SchedulerConfig {
	return SchedulerConfig{
		HeartbeatInterval: 20 * time.Millisecond,
		RetryBackoff:      []time.Duration{60 * time.Millisecond, 120 * time.Millisecond},
		SlowRetryInterval: 150 * time.Millisecond,
		AdmissionWindow:   200 * time.Millisecond,
		RequestTimeout:    250 * time.Millisecond,
	}
}

// startHarness starts a scheduler with the default test config. now == nil
// means real time.
func startHarness(t *testing.T, handler *recordingHandler, now func() time.Time) *testHarness {
	t.Helper()
	cfg := newTestConfig()
	return startHarnessCfg(t, handler, cfg, now)
}

func startHarnessCfg(t *testing.T, handler *recordingHandler, cfg SchedulerConfig, now func() time.Time) *testHarness {
	t.Helper()
	server := httptestServer(t, handler)
	store := m54Store(t)
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, now)
	client := NewClient(server, m54Token, cfg.RequestTimeout)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	sched := NewScheduler(store, builder, client, cfg, logger, now)
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)
	h := &testHarness{t: t, store: store, handler: handler, server: serverURL{url: server}, cfg: cfg, sched: sched, cancel: cancel, nowFn: now}
	t.Cleanup(func() {
		cancel()
		sched.Wait()
	})
	return h
}

// mutatePublic applies a visible public change to the store.
func (h *testHarness) mutatePublic(id string) {
	h.t.Helper()
	changed := h.store.Snapshot()
	changed.Tasks = append(changed.Tasks, state.TaskState{ID: id, Provider: "codex", Title: "Task " + id, StartedAt: m54Base, UpdatedAt: m54Base})
	h.store.Replace(changed)
}

// mutateInternalOnly applies a change that must not alter the PublicState
// projection: the internal generatedAt bookkeeping field, which ProjectPublic
// ignores.
func (h *testHarness) mutateInternalOnly() {
	h.t.Helper()
	changed := h.store.Snapshot()
	changed.GeneratedAt = changed.GeneratedAt.Add(33 * time.Second)
	h.store.Replace(changed)
}

func (h *testHarness) requests() []recordedRequest {
	h.t.Helper()
	return h.handler.recorded()
}

func (h *testHarness) waitRequests(n int) []recordedRequest {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reqs := h.requests()
		if len(reqs) >= n {
			return reqs
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("expected %d requests, saw %d", n, len(reqs))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitHealthClass blocks until the scheduler has recorded the given error
// class, i.e. until it has fully processed the last response. Tests that
// advance a fake clock must synchronize on this, not on the server having
// merely recorded a request.
func (h *testHarness) waitHealthClass(class string) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if h.sched.Health().LastErrorClass == class {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("expected health error class %q, got %+v", class, h.sched.Health())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitConnected blocks until the scheduler records an accepted send.
func (h *testHarness) waitConnected() {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if h.sched.Health().Connected {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("expected connected health, got %+v", h.sched.Health())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *testHarness) assertQuiet(ms int, want int) {
	h.t.Helper()
	deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := len(h.requests()); got > want {
			h.t.Fatalf("expected no more than %d requests, saw %d", want, got)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(h.requests()); got != want {
		h.t.Fatalf("expected exactly %d requests after quiet window, saw %d", want, got)
	}
}

func decodeEnvelope(t *testing.T, req recordedRequest) NodeSnapshot {
	t.Helper()
	var snap NodeSnapshot
	if err := json.Unmarshal(req.body, &snap); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return snap
}

func TestM54SchedulerStartupSendsImmediatelyWithSequenceOne(t *testing.T) {
	h := startHarness(t, &recordingHandler{}, nil)
	reqs := h.waitRequests(1)
	snap := decodeEnvelope(t, reqs[0])
	if snap.Sequence != 1 {
		t.Fatalf("first snapshot of a session must use sequence 1, got %d", snap.Sequence)
	}
	if snap.NodeID != "mac-a" || snap.StateKind != "nodeSnapshot" || snap.State.Host.ID != "mac-a" {
		t.Fatalf("unexpected startup envelope: %+v", snap)
	}
	if !snap.SentAt.Equal(snap.State.GeneratedAt) {
		t.Fatalf("sentAt must equal nested generatedAt")
	}
	if snap.SessionID == "" || len(snap.SessionID) != 32 {
		t.Fatalf("startup must carry a fresh session id, got %q", snap.SessionID)
	}
}

func TestM54SchedulerHeartbeatSendsFreshSnapshotsWithNextSequence(t *testing.T) {
	cfg := newTestConfig()
	cfg.HeartbeatInterval = 25 * time.Millisecond
	h := startHarnessCfg(t, &recordingHandler{}, cfg, nil)
	reqs := h.waitRequests(5)
	session := decodeEnvelope(t, reqs[0]).SessionID
	last := time.Time{}
	for i, req := range reqs {
		snap := decodeEnvelope(t, req)
		if snap.SessionID != session {
			t.Fatalf("request %d switched session mid-run", i)
		}
		if snap.Sequence != uint64(i+1) {
			t.Fatalf("request %d has sequence %d, want %d", i, snap.Sequence, i+1)
		}
		if !snap.SentAt.After(last) {
			t.Fatalf("heartbeats must be freshly constructed snapshots with advancing sentAt")
		}
		last = snap.SentAt
	}
}

func TestM54SchedulerPublicChangeSendsBeforeHeartbeat(t *testing.T) {
	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second
	h := startHarnessCfg(t, &recordingHandler{}, cfg, nil)
	h.waitRequests(1)
	h.assertQuiet(150, 1)

	h.mutatePublic("task-immediate")
	reqs := h.waitRequests(2)
	snap := decodeEnvelope(t, reqs[1])
	if snap.Sequence != 2 {
		t.Fatalf("change-driven send must use next sequence, got %d", snap.Sequence)
	}
	found := false
	for _, task := range snap.State.Tasks {
		if task.ID == "task-immediate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("change-driven snapshot must carry the new public state")
	}
}

func TestM54SchedulerInternalOnlyChangeWaitsForHeartbeat(t *testing.T) {
	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second
	h := startHarnessCfg(t, &recordingHandler{}, cfg, nil)
	h.waitRequests(1)

	h.mutateInternalOnly()
	h.assertQuiet(300, 1) // digest unchanged: heartbeat only, and heartbeat is far away
}

func TestM54SchedulerTransientFailureRetriesSameEnvelopeAndSequence(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(500, 500, 200)
	h := startHarness(t, handler, nil)
	reqs := h.waitRequests(4)

	failed := reqs[:2]
	if bytes.Equal(failed[0].body, []byte{}) || !bytes.Equal(failed[0].body, failed[1].body) {
		t.Fatalf("transient retries must reuse the exact same payload bytes")
	}
	for i, req := range reqs[:3] {
		snap := decodeEnvelope(t, req)
		if snap.Sequence != 1 {
			t.Fatalf("failed send %d must not advance sequence (got %d)", i, snap.Sequence)
		}
	}
	accepted := decodeEnvelope(t, reqs[2])
	if accepted.Sequence != 1 {
		t.Fatalf("accepted retry must reuse sequence 1, got %d", accepted.Sequence)
	}
	next := decodeEnvelope(t, reqs[3])
	if next.Sequence != 2 {
		t.Fatalf("snapshot after acceptance must use sequence 2, got %d", next.Sequence)
	}
}

func TestM54SchedulerBackoffDelaysRetries(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(500, 500, 200)
	h := startHarness(t, handler, nil)
	reqs := h.waitRequests(3)

	gap1 := reqs[1].at.Sub(reqs[0].at)
	gap2 := reqs[2].at.Sub(reqs[1].at)
	// Ladder is [60ms, 120ms]; heartbeat ticks at 20ms must not bypass it.
	if gap1 < 55*time.Millisecond {
		t.Fatalf("first retry after backoff came too early: %v", gap1)
	}
	if gap2 < 115*time.Millisecond {
		t.Fatalf("second retry after backoff came too early: %v", gap2)
	}
}

func TestM54SchedulerConflictResyncsNewSessionAtSequenceOne(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(409, 200)
	h := startHarness(t, handler, nil)
	reqs := h.waitRequests(4)

	first := decodeEnvelope(t, reqs[0])
	resync := decodeEnvelope(t, reqs[1])
	if resync.SessionID == first.SessionID {
		t.Fatalf("409 must trigger a new session id")
	}
	if resync.Sequence != 1 {
		t.Fatalf("resync snapshot must reset sequence to 1, got %d", resync.Sequence)
	}
	after := decodeEnvelope(t, reqs[2])
	if after.SessionID != resync.SessionID || after.Sequence != 2 {
		t.Fatalf("post-resync snapshots must continue the new session, got %s/%d", after.SessionID, after.Sequence)
	}
}

func TestM54SchedulerPersistentConflictRetriesSlowly(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(409, 409, 409, 409, 409, 409, 409, 409, 409, 409, 409, 409)
	h := startHarness(t, handler, nil)
	h.waitRequests(3)

	time.Sleep(500 * time.Millisecond)
	reqs := h.requests()
	// 500ms with slow interval 150ms allows roughly t0 immediate pair plus
	// slow retries; a rapid loop at heartbeat rate would produce dozens.
	if len(reqs) > 8 {
		t.Fatalf("persistent conflict retried too fast: %d attempts in 500ms", len(reqs))
	}
	sessions := map[string]int{}
	for _, req := range reqs {
		snap := decodeEnvelope(t, req)
		if snap.Sequence != 1 {
			t.Fatalf("conflict-mode attempts must stay at sequence 1, got %d", snap.Sequence)
		}
		sessions[snap.SessionID]++
	}
	if len(sessions) < 3 {
		t.Fatalf("slow conflict retries must repeat the resync procedure (new sessions), saw %d", len(sessions))
	}
}

func TestM54SchedulerAuthFailureHoldsSlowInterval(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(401, 401, 401, 401)
	h := startHarness(t, handler, nil)
	h.waitRequests(1)
	h.waitHealthClass("auth")

	// Heartbeat is 20ms; the auth hold is 150ms. No re-attempt may run at
	// heartbeat rate.
	h.assertQuiet(80, 1)
	reqs := h.waitRequests(2)
	if gap := reqs[1].at.Sub(reqs[0].at); gap < 140*time.Millisecond {
		t.Fatalf("auth re-attempt came too early: %v", gap)
	}
	hl := h.sched.Health()
	if hl.Connected || hl.LastErrorClass != "auth" {
		t.Fatalf("unexpected health after 401: %+v", hl)
	}
}

func TestM54SchedulerPayloadFailureDropsEnvelope(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(400, 400, 400, 400, 400, 400)
	h := startHarness(t, handler, nil)
	reqs := h.waitRequests(3)

	for i, req := range reqs {
		snap := decodeEnvelope(t, req)
		if snap.Sequence != 1 {
			t.Fatalf("payload-rejected envelope %d must not advance sequence, got %d", i, snap.Sequence)
		}
	}
	// Each new attempt must be a freshly built envelope, never a byte-equal
	// retry of the rejected one.
	if bytes.Equal(reqs[0].body, reqs[1].body) {
		t.Fatalf("rejected envelope must not be retried as the same payload")
	}
	hl := h.sched.Health()
	if hl.Connected || hl.LastErrorClass != "payload" {
		t.Fatalf("unexpected health after 400: %+v", hl)
	}
}

func TestM54SchedulerStalePendingIsRebuiltFresh(t *testing.T) {
	clock := newM54Clock(m54Base)
	handler := &recordingHandler{}
	handler.setStatus(500, 500, 200)
	h := startHarnessCfg(t, handler, newTestConfig(), clock.Now)
	h.waitRequests(1)
	// Synchronize on the scheduler having processed the first failure before
	// moving its clock, so retryAt is already computed in old coordinates.
	h.waitHealthClass("transient")

	// First failure schedules a retry at +60ms (fake). Advance past the
	// 200ms admission window instead: the pending envelope is now older than
	// the receiver window and must be rebuilt, not retried stale.
	clock.Advance(250 * time.Millisecond)
	reqs := h.waitRequests(2)
	second := decodeEnvelope(t, reqs[1])
	if second.Sequence != 1 {
		t.Fatalf("rebuilt snapshot must reuse the unaccepted sequence 1, got %d", second.Sequence)
	}
	if !second.SentAt.Equal(m54Base.Add(250 * time.Millisecond)) {
		t.Fatalf("rebuilt snapshot must carry a fresh timestamp, got %v want %v", second.SentAt, m54Base.Add(250*time.Millisecond))
	}
	if bytes.Equal(reqs[0].body, reqs[1].body) {
		t.Fatalf("stale pending envelope must not be retried byte-equal")
	}
}

func TestM54SchedulerRestartUsesNewSession(t *testing.T) {
	handler := &recordingHandler{}
	server := httptestServer(t, handler)
	store := m54Store(t)
	cfg := newTestConfig()

	runOnce := func() string {
		builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
		client := NewClient(server, m54Token, cfg.RequestTimeout)
		logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
		sched := NewScheduler(store, builder, client, cfg, logger, nil)
		ctx, cancel := context.WithCancel(context.Background())
		go sched.Run(ctx)
		defer func() { cancel(); sched.Wait() }()
		before := len(handler.recorded())
		reqs := func() []recordedRequest {
			deadline := time.Now().Add(2 * time.Second)
			for {
				all := handler.recorded()
				if len(all) > before {
					return all[before:]
				}
				if time.Now().After(deadline) {
					t.Fatalf("no request observed")
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
		return decodeEnvelope(t, reqs[0]).SessionID
	}
	first := runOnce()
	second := runOnce()
	if first == second {
		t.Fatalf("each uplink process session must create a new session id")
	}
}

func TestM54SchedulerShutdownWaitsForInFlightRequest(t *testing.T) {
	handler := &recordingHandler{}
	handler.mu.Lock()
	handler.block = make(chan struct{})
	handler.mu.Unlock()

	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second // only the startup send exists
	server := httptestServer(t, handler)
	store := m54Store(t)
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
	client := NewClient(server, m54Token, cfg.RequestTimeout)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	sched := NewScheduler(store, builder, client, cfg, logger, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for len(handler.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if len(handler.recorded()) == 0 {
		t.Fatalf("startup request never arrived")
	}

	cancel()
	// The in-flight request is held by the hub: shutdown must wait for it
	// rather than kill it.
	waited := make(chan struct{})
	go func() { sched.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatalf("Wait returned while a request was still in flight")
	case <-time.After(120 * time.Millisecond):
	}

	close(handler.block) // release the hub response
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatalf("Wait did not return after the in-flight request completed")
	}
	if hl := sched.Health(); !hl.Connected {
		t.Fatalf("in-flight request must complete and be accepted, health %+v", hl)
	}
	if got := len(handler.recorded()); got != 1 {
		t.Fatalf("no further sends after shutdown, saw %d", got)
	}
}

func TestM54SchedulerHealthTransitions(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(500)
	h := startHarness(t, handler, nil)
	h.waitRequests(1)
	h.waitHealthClass("transient")
	hl := h.sched.Health()
	if hl.Connected || hl.LastAttemptAt == nil {
		t.Fatalf("unexpected failing health: %+v", hl)
	}

	handler.setStatus() // default 200 from here on
	h.waitRequests(2)
	h.waitConnected()
	hl = h.sched.Health()
	if hl.LastErrorClass != "" || hl.LastSuccessAt == nil {
		t.Fatalf("unexpected recovered health: %+v", hl)
	}
}

func TestM54BackoffLadderMatchesFrozenSchedule(t *testing.T) {
	sched := NewScheduler(nil, nil, nil, DefaultSchedulerConfig(), nil, nil)
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	for attempt := 1; attempt <= 10; attempt++ {
		got := sched.backoffDelay(attempt)
		wantDelay := want[len(want)-1]
		if attempt <= len(want) {
			wantDelay = want[attempt-1]
		}
		if got != wantDelay {
			t.Fatalf("backoffDelay(%d) = %v, want %v", attempt, got, wantDelay)
		}
	}
}

// TestM54SchedulerLogsNeverLeakSecrets freezes the M5.4 security audit: the
// scheduler may log node id, session/sequence and status classes, never the
// bearer token, the Authorization header or snapshot body content.
func TestM54SchedulerLogsNeverLeakSecrets(t *testing.T) {
	var logs bytes.Buffer
	handler := &recordingHandler{}
	handler.setStatus(401, 409, 500, 200)
	server := httptestServer(t, handler)
	store := m54Store(t)
	cfg := newTestConfig()
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
	client := NewClient(server, m54Token, cfg.RequestTimeout)
	sched := NewScheduler(store, builder, client, cfg, logger, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)
	defer func() { cancel(); sched.Wait() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(handler.recorded()) >= 4 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	out := logs.String()
	for _, forbidden := range []string{m54Token, "Bearer", "Authorization", "schemaVersion", "nodeSnapshot"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("uplink log leaked %q:\n%s", forbidden, out)
		}
	}
}
