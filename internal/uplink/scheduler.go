package uplink

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// SchedulerConfig holds the frozen M5.2 scheduling numbers. Production uses
// DefaultSchedulerConfig; tests shrink the durations for determinism.
type SchedulerConfig struct {
	// HeartbeatInterval is the M5.2 §24 heartbeat: a fresh projected snapshot
	// every 1 second, which is both heartbeat and state refresh.
	HeartbeatInterval time.Duration
	// RetryBackoff is the bounded transient-failure ladder, frozen by M5.2
	// §27 as 1s → 2s → 4s → 8s → 15s max. The Nth consecutive transient
	// failure waits RetryBackoff[min(N, len)-1].
	RetryBackoff []time.Duration
	// SlowRetryInterval bounds re-attempts after authentication/configuration
	// failures and persistent conflicts (M5.2 §28).
	SlowRetryInterval time.Duration
	// AdmissionWindow mirrors the receiver's snapshot admission window
	// (M5.2 §13): a pending envelope older than this is abandoned and rebuilt
	// fresh instead of retried stale.
	AdmissionWindow time.Duration
	// RequestTimeout bounds one HTTP request (M5.2 §26: 5 seconds maximum).
	RequestTimeout time.Duration
}

func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		HeartbeatInterval: time.Second,
		RetryBackoff: []time.Duration{
			1 * time.Second,
			2 * time.Second,
			4 * time.Second,
			8 * time.Second,
			15 * time.Second,
		},
		SlowRetryInterval: 30 * time.Second,
		AdmissionWindow:   30 * time.Second,
		RequestTimeout:    DefaultRequestTimeout,
	}
}

// Health is the node-local uplink operational state (M5.2 §29): enough for a
// later Settings UI, never containing the bearer token or any hub response
// body. It is informational only and plays no scheduling role.
type Health struct {
	Connected      bool
	LastAttemptAt  *time.Time
	LastSuccessAt  *time.Time
	LastErrorClass string
}

// Scheduler is the one-in-flight node uplink runtime. A single goroutine owns
// all sending: heartbeats, change-driven immediate sends, retries and session
// resynchronization all serialize through it, so at most one HTTP snapshot
// request is ever in flight (M5.2 §23).
type Scheduler struct {
	source  StateSource
	builder *SnapshotBuilder
	client  *Client
	cfg     SchedulerConfig
	logger  *slog.Logger
	now     func() time.Time
	// newSession generates one fresh random session identity. It defaults to
	// NewSessionID and exists as a field so deterministic tests can replace
	// it, including with failing generators (M5.2 §28 entropy behavior).
	newSession func() (string, error)

	mu     sync.Mutex
	health Health
	done   chan struct{}
}

func NewScheduler(source StateSource, builder *SnapshotBuilder, client *Client, cfg SchedulerConfig, logger *slog.Logger, now func() time.Time) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = time.Second
	}
	if len(cfg.RetryBackoff) == 0 {
		cfg.RetryBackoff = DefaultSchedulerConfig().RetryBackoff
	}
	if cfg.SlowRetryInterval <= 0 {
		cfg.SlowRetryInterval = 30 * time.Second
	}
	if cfg.AdmissionWindow <= 0 {
		cfg.AdmissionWindow = 30 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	return &Scheduler{source: source, builder: builder, client: client, cfg: cfg, logger: logger, now: now, newSession: NewSessionID, done: make(chan struct{})}
}

// schedState is the loop-private session/ordering state. It is only touched
// by the scheduler goroutine, so it needs no locking of its own.
type schedState struct {
	session string
	// seq is the last ISSUED new-snapshot sequence, not the last accepted
	// one: every fresh Build uses seq+1 and immediately becomes the new
	// issued value, so two different new snapshots can never reuse the same
	// (session, sequence) tuple even when the earlier envelope was rejected
	// or abandoned (M5.2 §5.4). Exact retries of a pending envelope do not
	// touch it.
	seq          uint64
	lastDigest   [32]byte
	haveDigest   bool
	pending      *pendingEnvelope
	transients   int // consecutive transient failures
	retryAt      time.Time
	eligibleAt   time.Time // earliest next fresh send after auth/conflict hold
	conflictMode bool
	resyncOwed   bool
	// newerStateOwed remembers that local state changed while a request was
	// pending or in flight. No concurrent request may start; once the pending
	// request resolves, the newest PublicState is delivered immediately
	// unless its digest already matches what the hub accepted (M5.2 §23/§27).
	newerStateOwed bool
}

// pendingEnvelope is one logical in-retry request. A transient failure keeps
// it so the exact same session, sequence and payload are retried (M5.2 §5.4,
// §27); the sequence is only advanced once the hub accepts it.
type pendingEnvelope struct {
	env    NodeSnapshot
	digest [32]byte
}

// Run drives the uplink until ctx is cancelled, then returns after the
// current in-flight request (if any) has completed. Cancelling ctx never
// aborts a request mid-flight: sends run under a detached context bounded by
// the request timeout, so shutdown waits instead of killing. Cancellation is
// checked before every NEW request, so cancel prevents each subsequent send
// — resynchronization, owed catch-up, retry and heartbeat alike — while the
// one in-flight request finishes undisturbed (M5.4 shutdown lifecycle). Run
// must be called exactly once.
func (s *Scheduler) Run(ctx context.Context) {
	defer close(s.done)
	st, err := s.newSessionState()
	if err != nil {
		// No session identity exists, so no HTTP request may ever start. The
		// failure stays a bounded generic local health error (M5.2 §29).
		s.recordFailure("session_entropy")
		s.logger.Error("uplink session unavailable", "node", s.builder.nodeID, "err", "entropy")
		return
	}
	s.logger.Info("uplink session started", "node", s.builder.nodeID, "session", st.session)

	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// Startup: send the first snapshot immediately (sequence 1).
	if ctx.Err() == nil {
		s.attempt(ctx, st)
	}

	for {
		if ctx.Err() != nil {
			s.logger.Info("uplink stopped", "node", s.builder.nodeID)
			return
		}
		if st.resyncOwed && !s.now().Before(st.eligibleAt) {
			// A conflict is owed a new-session resynchronization attempt. The
			// identity is generated here, not at conflict time: if entropy
			// fails, resync keeps the slow hold and the owed resync is
			// retried once generation succeeds — the conflicting old session
			// is never reused for sending (M5.2 §28).
			if s.resync(st) {
				st.resyncOwed = false
				s.attempt(ctx, st)
			}
			continue
		}
		if st.pending == nil && st.newerStateOwed && !s.now().Before(st.eligibleAt) {
			// A pending request resolved with newer local state remembered.
			// Deliver it immediately unless the hub already holds exactly
			// this state — for example the pending envelope was rebuilt
			// before expiry and already contained the change (M5.2 §23).
			st.newerStateOwed = false
			if !st.haveDigest || Digest(s.builder.Public()) != st.lastDigest {
				s.attempt(ctx, st)
				continue
			}
		}
		select {
		case <-ctx.Done():
			s.logger.Info("uplink stopped", "node", s.builder.nodeID)
			return
		case <-s.source.Changes():
			if ctx.Err() != nil {
				s.logger.Info("uplink stopped", "node", s.builder.nodeID)
				return
			}
			if st.pending != nil {
				// A retry is owed; a change wake may only accelerate a retry
				// that is already eligible — never bypass backoff and never
				// start a concurrent request. The newer state is remembered
				// and delivered once the pending request resolves (M5.2 §27).
				st.newerStateOwed = true
				if !s.now().Before(st.retryAt) {
					s.attempt(ctx, st)
				}
				continue
			}
			if s.now().Before(st.eligibleAt) {
				continue
			}
			if st.haveDigest && Digest(s.builder.Public()) == st.lastDigest {
				// No visible public change: heartbeat cadence is enough.
				continue
			}
			s.attempt(ctx, st)
		case <-ticker.C:
			if ctx.Err() != nil {
				s.logger.Info("uplink stopped", "node", s.builder.nodeID)
				return
			}
			if st.pending != nil {
				if !s.now().Before(st.retryAt) {
					s.attempt(ctx, st)
				}
				continue
			}
			if s.now().Before(st.eligibleAt) {
				continue
			}
			if st.conflictMode {
				// Slow conflict retry repeats the resynchronization
				// procedure rather than hammering the same session. On
				// entropy failure the hold simply extends.
				if !s.resync(st) {
					continue
				}
			}
			s.attempt(ctx, st)
		}
	}
}

// Wait blocks until Run has returned (in-flight request included).
func (s *Scheduler) Wait() {
	<-s.done
}

// Health returns a copy of the node-local operational health state.
func (s *Scheduler) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
}

// attempt performs one send cycle: reuse a still-valid pending envelope for
// an exact retry, otherwise build a fresh snapshot with sequence seq+1, then
// apply the outcome to the session/ordering state.
func (s *Scheduler) attempt(ctx context.Context, st *schedState) {
	if st.pending != nil && !s.pendingFresh(st.pending) {
		// The pending envelope left the receiver admission window; abandon it
		// and rebuild from the newest state with the next issued sequence
		// (M5.2 §27).
		s.logger.Info("uplink pending snapshot expired", "node", s.builder.nodeID, "sequence", st.pending.env.Sequence)
		st.pending = nil
	}
	if st.pending == nil {
		snap, err := s.builder.Build(st.session, st.seq+1)
		if err != nil {
			// Local build failure (identity binding): never transported.
			s.recordFailure("local_build")
			s.logger.Error("uplink snapshot build refused", "node", s.builder.nodeID, "err", "identity_binding")
			return
		}
		// The sequence is issued with the new snapshot, not on acceptance:
		// any later new snapshot in this session uses a strictly higher one
		// even if this envelope is rejected (M5.2 §5.4). An exact retry of a
		// pending envelope reuses it unchanged.
		st.seq = snap.Sequence
		st.pending = &pendingEnvelope{env: snap, digest: Digest(snap.State)}
	}
	env := st.pending.env
	s.recordAttempt()

	// Detached from ctx so an in-flight request survives shutdown; the client
	// still bounds it by the request timeout.
	err := s.client.Send(context.WithoutCancel(ctx), env)
	if err == nil {
		st.lastDigest = st.pending.digest
		st.haveDigest = true
		st.pending = nil
		st.transients = 0
		st.conflictMode = false
		st.resyncOwed = false
		st.eligibleAt = time.Time{}
		s.recordSuccess()
		s.logger.Debug("uplink snapshot accepted", "node", s.builder.nodeID, "session", env.SessionID, "sequence", env.Sequence)
		return
	}
	sendErr, ok := err.(*SendError)
	if !ok {
		sendErr = &SendError{Kind: ErrTransient}
	}
	switch sendErr.Kind {
	case ErrTransient:
		st.transients++
		delay := s.backoffDelay(st.transients)
		st.retryAt = s.now().Add(delay)
		// pending is retained: the exact same envelope is retried.
		s.recordFailure(string(sendErr.Kind))
		s.logger.Info("uplink send failed", "node", s.builder.nodeID, "status", sendErr.Status, "reason", string(sendErr.Kind), "retry_in", delay.String(), "sequence", env.Sequence)
	case ErrAuth:
		// 401/403: configuration failure. No automatic retry at heartbeat
		// rate; re-attempt slowly with a freshly built envelope (M5.2 §28).
		st.pending = nil
		st.transients = 0
		st.eligibleAt = s.now().Add(s.cfg.SlowRetryInterval)
		s.recordFailure(string(sendErr.Kind))
		s.logger.Warn("uplink authentication rejected", "node", s.builder.nodeID, "status", sendErr.Status, "retry_in", s.cfg.SlowRetryInterval.String())
	case ErrPayload:
		// 400/413/415: protocol or payload failure. The offending envelope is
		// dropped, not retried; the next fresh snapshot on normal cadence
		// waits for state or configuration to change (M5.2 §28).
		st.pending = nil
		st.transients = 0
		s.recordFailure(string(sendErr.Kind))
		s.logger.Warn("uplink snapshot rejected", "node", s.builder.nodeID, "status", sendErr.Status, "reason", string(sendErr.Kind))
	case ErrConflict:
		// 409: ordering/session conflict. Abandon the envelope and start a
		// new random session at sequence 1 with one immediate
		// resynchronization attempt; a persistent conflict falls into bounded
		// slow retry (M5.2 §28). The new identity is generated by the loop
		// before any resync send so an entropy failure can never fall back to
		// the conflicting session.
		st.pending = nil
		st.transients = 0
		if !st.conflictMode {
			st.conflictMode = true
			st.resyncOwed = true
			s.logger.Warn("uplink conflict: resynchronizing session", "node", s.builder.nodeID)
		} else {
			st.eligibleAt = s.now().Add(s.cfg.SlowRetryInterval)
			s.logger.Warn("uplink conflict persists", "node", s.builder.nodeID, "retry_in", s.cfg.SlowRetryInterval.String())
		}
		s.recordFailure(string(sendErr.Kind))
	}
}

// pendingFresh reports whether the pending envelope is still inside the
// receiver admission window.
func (s *Scheduler) pendingFresh(p *pendingEnvelope) bool {
	return p.env.SentAt.Add(s.cfg.AdmissionWindow).After(s.now())
}

func (s *Scheduler) backoffDelay(consecutiveTransients int) time.Duration {
	ladder := s.cfg.RetryBackoff
	if consecutiveTransients < 1 {
		consecutiveTransients = 1
	}
	if consecutiveTransients > len(ladder) {
		consecutiveTransients = len(ladder)
	}
	return ladder[consecutiveTransients-1]
}

func (s *Scheduler) newSessionState() (*schedState, error) {
	session, err := s.newSession()
	if err != nil {
		return nil, err
	}
	return &schedState{session: session, seq: 0}, nil
}

// resync applies the conflict recovery identity: a new random session with
// sequence reset to 1 (M5.2 §28). It reports whether the new identity could
// be generated. On entropy failure it never falls back to the previous —
// conflicting — session: the scheduler records a bounded generic health
// error, holds for the slow interval and retries identity generation before
// any resync send becomes possible again.
func (s *Scheduler) resync(st *schedState) bool {
	session, err := s.newSession()
	if err != nil {
		st.eligibleAt = s.now().Add(s.cfg.SlowRetryInterval)
		s.recordFailure("session_entropy")
		s.logger.Error("uplink resync entropy unavailable", "node", s.builder.nodeID, "err", "entropy")
		return false
	}
	st.session = session
	st.seq = 0
	return true
}

func (s *Scheduler) recordAttempt() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.LastAttemptAt = &now
}

func (s *Scheduler) recordSuccess() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.Connected = true
	s.health.LastSuccessAt = &now
	s.health.LastErrorClass = ""
}

func (s *Scheduler) recordFailure(class string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health.Connected = false
	s.health.LastErrorClass = class
}
