package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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
	// The admission window must comfortably outlast the 60ms+120ms backoff
	// ladder so every retry in this test is provably an exact retry of the
	// still-fresh pending envelope, never a stale rebuild.
	cfg := newTestConfig()
	cfg.AdmissionWindow = time.Second
	handler := &recordingHandler{}
	handler.setStatus(500, 500, 200)
	h := startHarnessCfg(t, handler, cfg, nil)
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

	// Every fresh envelope after a payload rejection is a NEW snapshot and
	// therefore carries a strictly higher issued sequence (M5.2 §5.4) —
	// only an exact transient retry may reuse a sequence.
	for i, req := range reqs {
		snap := decodeEnvelope(t, req)
		if snap.Sequence != uint64(i+1) {
			t.Fatalf("fresh payload-rejected envelope %d must advance sequence, got %d", i, snap.Sequence)
		}
	}
	// Each new attempt must be a freshly built envelope, never a byte-equal
	// retry of the rejected one.
	for i := 1; i < len(reqs); i++ {
		if bytes.Equal(reqs[i-1].body, reqs[i].body) {
			t.Fatalf("rejected envelope %d must not be retried as the same payload", i)
		}
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
	// the receiver window and must be abandoned. The rebuilt snapshot is a
	// NEW snapshot and therefore carries the NEXT issued sequence (2), never
	// the abandoned sequence again (M5.2 §5.4/§27).
	clock.Advance(250 * time.Millisecond)
	reqs := h.waitRequests(2)
	second := decodeEnvelope(t, reqs[1])
	if second.Sequence != 2 {
		t.Fatalf("rebuilt snapshot must use the next issued sequence 2, got %d", second.Sequence)
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
	// M5.2 §27 freezes the transient-failure backoff as 1s → 2s → 4s → 8s →
	// 15s max; the sixth and every later consecutive failure wait 15s.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
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

// TestM54SchedulerFreshSnapshotsAlwaysAdvanceSequence freezes the corrected
// M5.2 §5.4 model: sequence tracks issued NEW snapshots, not accepted ones, so
// two different fresh envelopes in one session never share a
// (session, sequence) tuple — even when every earlier one was rejected.
func TestM54SchedulerFreshSnapshotsAlwaysAdvanceSequence(t *testing.T) {
	t.Run("payload rejected fresh envelopes advance", func(t *testing.T) {
		handler := &recordingHandler{}
		handler.setStatus(400, 400, 400)
		h := startHarness(t, handler, nil)
		reqs := h.waitRequests(3)
		session := decodeEnvelope(t, reqs[0]).SessionID
		for i, req := range reqs {
			snap := decodeEnvelope(t, req)
			if snap.SessionID != session {
				t.Fatalf("request %d switched session mid-run", i)
			}
			if snap.Sequence != uint64(i+1) {
				t.Fatalf("fresh envelope %d must carry sequence %d, got %d", i, i+1, snap.Sequence)
			}
		}
		for i := 1; i < len(reqs); i++ {
			if bytes.Equal(reqs[i-1].body, reqs[i].body) {
				t.Fatalf("fresh envelope %d repeated a rejected payload byte-equal", i)
			}
		}
	})

	t.Run("auth rejected fresh envelope advances after slow hold", func(t *testing.T) {
		handler := &recordingHandler{}
		handler.setStatus(401, 200)
		h := startHarness(t, handler, nil)
		reqs := h.waitRequests(1)
		first := decodeEnvelope(t, reqs[0])
		h.waitHealthClass("auth")

		// The slow interval (150ms) must not be bypassed by the heartbeat
		// (20ms): no re-attempt may run at heartbeat rate.
		h.assertQuiet(80, 1)
		reqs = h.waitRequests(2)
		second := decodeEnvelope(t, reqs[1])
		if second.SessionID != first.SessionID {
			t.Fatalf("401 must not rotate the session identity")
		}
		if second.Sequence <= first.Sequence {
			t.Fatalf("first fresh envelope after 401 must use a higher sequence, got %d after %d", second.Sequence, first.Sequence)
		}
		if gap := reqs[1].at.Sub(reqs[0].at); gap < 140*time.Millisecond {
			t.Fatalf("auth re-attempt bypassed the slow interval: %v", gap)
		}
	})
}

// TestM54SchedulerRemembersPublicChangeDuringTransientBackoff freezes M5.2
// §23/§27: a public state change during transient backoff must neither bypass
// the backoff nor be forgotten. The exact pending envelope is retried
// unchanged; once it is accepted, the newest state is delivered immediately
// with the next sequence.
func TestM54SchedulerRemembersPublicChangeDuringTransientBackoff(t *testing.T) {
	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second // the catch-up cannot be a heartbeat
	handler := &recordingHandler{}
	handler.setStatus(500, 200)
	h := startHarnessCfg(t, handler, cfg, nil)

	h.waitRequests(1) // envelope A: transient failure
	h.waitHealthClass("transient")

	// Public state changes while the retry is still under backoff.
	h.mutatePublic("task-catchup")
	// The backoff (60ms) must not be bypassed: no request appears during it.
	h.assertQuiet(30, 1)
	// A wake after the backoff window makes the exact retry eligible.
	time.Sleep(60 * time.Millisecond)
	h.mutatePublic("task-catchup-2")

	// req2 is the exact retry of A, unchanged despite the newer local state.
	reqs := h.waitRequests(2)
	if !bytes.Equal(reqs[0].body, reqs[1].body) {
		t.Fatalf("retry during remembered change must reuse the exact pending envelope bytes")
	}
	if gap := reqs[1].at.Sub(reqs[0].at); gap < 55*time.Millisecond {
		t.Fatalf("remembered change bypassed the transient backoff: %v", gap)
	}

	// The remembered newer state is delivered immediately after acceptance:
	// next sequence, newest public content, no heartbeat involvement.
	reqs = h.waitRequests(3)
	catchUp := decodeEnvelope(t, reqs[2])
	if catchUp.Sequence != 2 {
		t.Fatalf("catch-up snapshot must use the next sequence 2, got %d", catchUp.Sequence)
	}
	found := false
	for _, task := range catchUp.State.Tasks {
		if task.ID == "task-catchup-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("catch-up snapshot must carry the newest public task state")
	}
	if gap := reqs[2].at.Sub(reqs[1].at); gap > 100*time.Millisecond {
		t.Fatalf("catch-up must follow acceptance immediately, took %v", gap)
	}
	// No redundant extra request follows the satisfied catch-up.
	h.assertQuiet(150, 3)
}

// TestM54SchedulerInternalOnlyChangeDuringBackoffDoesNotDuplicate freezes the
// other half of M5.2 §23: an internal-only change may be remembered as a dirty
// hint, but once the pending envelope is accepted and the newest public digest
// is unchanged, no redundant catch-up request may be sent.
func TestM54SchedulerInternalOnlyChangeDuringBackoffDoesNotDuplicate(t *testing.T) {
	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second
	handler := &recordingHandler{}
	handler.setStatus(500, 200)
	h := startHarnessCfg(t, handler, cfg, nil)

	h.waitRequests(1)
	h.waitHealthClass("transient")

	h.mutateInternalOnly() // remembered hint, no public change
	h.assertQuiet(30, 1)   // backoff still enforced
	time.Sleep(60 * time.Millisecond)
	h.mutateInternalOnly() // wake makes the exact retry eligible

	reqs := h.waitRequests(2)
	if !bytes.Equal(reqs[0].body, reqs[1].body) {
		t.Fatalf("retry must reuse the exact pending envelope bytes")
	}
	h.waitConnected()
	// Newest public digest identical to the accepted envelope: the owed hint
	// is satisfied without any third request.
	h.assertQuiet(200, 2)
}

// TestM54SchedulerCancelAfterInflightConflictDoesNotResync freezes the
// shutdown invariant: cancelling the scheduler prevents every NEW request, so
// a 409 observed by an in-flight request must never trigger a resync send.
func TestM54SchedulerCancelAfterInflightConflictDoesNotResync(t *testing.T) {
	handler := &recordingHandler{}
	handler.mu.Lock()
	handler.block = make(chan struct{})
	handler.mu.Unlock()
	handler.setStatus(http.StatusConflict)

	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second // only the startup send exists
	h := startHarnessCfg(t, handler, cfg, nil)
	h.waitRequests(1)

	h.cancel()           // stop while the startup request is in flight
	close(handler.block) // release it with a 409
	h.sched.Wait()

	if got := len(h.requests()); got != 1 {
		t.Fatalf("cancel must prevent the post-conflict resync request, saw %d requests", got)
	}
	if hl := h.sched.Health(); hl.LastErrorClass != "conflict" {
		t.Fatalf("the in-flight 409 must still be processed, health %+v", hl)
	}
}

// TestM54SchedulerCancelAfterInflightSuccessDoesNotCatchUp freezes the other
// shutdown half: a state change remembered while a request is in flight must
// not produce a catch-up request after cancellation, even though the buffered
// wake is still pending.
func TestM54SchedulerCancelAfterInflightSuccessDoesNotCatchUp(t *testing.T) {
	handler := &recordingHandler{}
	handler.mu.Lock()
	handler.block = make(chan struct{})
	handler.mu.Unlock()

	cfg := newTestConfig()
	cfg.HeartbeatInterval = 10 * time.Second
	h := startHarnessCfg(t, handler, cfg, nil)
	h.waitRequests(1)

	h.mutatePublic("task-owed") // buffered wake: newer state exists
	h.cancel()                  // stop while the request is in flight
	close(handler.block)        // release it with 200
	h.sched.Wait()

	if got := len(h.requests()); got != 1 {
		t.Fatalf("cancel must prevent the buffered catch-up request, saw %d requests", got)
	}
	if hl := h.sched.Health(); !hl.Connected {
		t.Fatalf("the in-flight request must complete and be accepted, health %+v", hl)
	}
}

// TestM54SchedulerResyncEntropyFailureNeverReusesConflictSession freezes
// M5.2 §28: after a 409, a failing session-identity generator must never cause
// the old conflicting session to be reused for another request. The scheduler
// records a bounded local health error, holds at the slow interval and only
// sends again once a NEW session could be generated — at sequence 1.
func TestM54SchedulerResyncEntropyFailureNeverReusesConflictSession(t *testing.T) {
	const (
		sessionA = "aabbccddeeff00112233445566778899"
		sessionB = "11223344556677889900aabbccddeeff"
	)
	handler := &recordingHandler{}
	handler.setStatus(409, 200)
	server := httptestServer(t, handler)
	store := m54Store(t)
	cfg := newTestConfig()
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	sched := NewScheduler(store, builder, NewClient(server, m54Token, cfg.RequestTimeout), cfg, logger, nil)

	calls := 0
	sched.newSession = func() (string, error) {
		calls++
		switch calls {
		case 1:
			return sessionA, nil // startup identity
		case 2:
			return "", errors.New("entropy exhausted") // first resync fails
		default:
			return sessionB, nil // recovery identity
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go sched.Run(ctx)
	defer func() { cancel(); sched.Wait() }()
	h := &testHarness{t: t, handler: handler, sched: sched}

	// req1: session A sequence 1 -> 409.
	h.waitRequests(1)
	// The resync cannot generate a new identity: the failure is a bounded
	// local health error and no request with the conflicting session A may
	// follow while the slow hold runs.
	h.waitHealthClass("session_entropy")
	h.assertQuiet(80, 1)

	// Recovery: once generation succeeds, the resync send uses the NEW
	// session at sequence 1 — the conflicting session is never reused.
	reqs := h.waitRequests(2)
	if gap := reqs[1].at.Sub(reqs[0].at); gap < 100*time.Millisecond {
		t.Fatalf("entropy failure must hold at the slow interval, retried after only %v", gap)
	}
	recovery := decodeEnvelope(t, reqs[1])
	if recovery.SessionID != sessionB {
		t.Fatalf("recovery must use the newly generated session, got %s", recovery.SessionID)
	}
	if recovery.Sequence != 1 {
		t.Fatalf("resync must reset sequence to 1, got %d", recovery.Sequence)
	}
}

// TestM54SchedulerStartupEntropyFailureNeverSends freezes the startup half of
// the entropy rule: without a session identity no HTTP request may ever start;
// the failure stays a bounded generic local health error.
func TestM54SchedulerStartupEntropyFailureNeverSends(t *testing.T) {
	handler := &recordingHandler{}
	server := httptestServer(t, handler)
	store := m54Store(t)
	cfg := newTestConfig()
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, nil)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	sched := NewScheduler(store, builder, NewClient(server, m54Token, cfg.RequestTimeout), cfg, logger, nil)
	sched.newSession = func() (string, error) { return "", errors.New("no entropy") }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Run(ctx)
	sched.Wait()

	if got := len(handler.recorded()); got != 0 {
		t.Fatalf("startup entropy failure must not send any request, saw %d", got)
	}
	hl := sched.Health()
	if hl.Connected || hl.LastErrorClass != "session_entropy" {
		t.Fatalf("startup entropy failure must be a bounded local health error, got %+v", hl)
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
