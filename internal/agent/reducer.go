package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type ReducerConfig struct {
	StaleAfter             time.Duration
	CompleteHighVisibility time.Duration
	CompleteRetention      time.Duration
	MaxSeenEventIDs        int
	MaxOldTurnsPerSession  int
	MaxSessions            int
}

type sessionMeta struct {
	CurrentTurnID        string
	CurrentTurnStartedAt time.Time
	LatestTurnEventAt    time.Time
	LatestSessionEventAt time.Time
	SessionEndedAt       time.Time
	OldTurns             []string
}

type sourceCapabilityState struct {
	turnAttributionDegraded bool
	claudeStopDegraded      bool
}

type eventEnrichment struct {
	Project *state.TaskProjectContext
}

type Reducer struct {
	mu           sync.Mutex
	store        *state.Store
	cfg          ReducerConfig
	seen         map[string]struct{}
	seenOrder    []string
	sessions     map[string]*sessionMeta
	sessionOrder []string
	capabilities map[Provider]*sourceCapabilityState
}

func NewReducer(store *state.Store, cfg ReducerConfig) *Reducer {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 15 * time.Minute
	}
	if cfg.CompleteHighVisibility < 0 {
		cfg.CompleteHighVisibility = 0
	}
	if cfg.CompleteRetention <= 0 {
		cfg.CompleteRetention = 30 * time.Minute
	}
	if cfg.MaxSeenEventIDs <= 0 {
		cfg.MaxSeenEventIDs = 4096
	}
	if cfg.MaxOldTurnsPerSession <= 0 {
		cfg.MaxOldTurnsPerSession = 16
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 512
	}
	return &Reducer{
		store:        store,
		cfg:          cfg,
		seen:         map[string]struct{}{},
		sessions:     map[string]*sessionMeta{},
		capabilities: map[Provider]*sourceCapabilityState{},
	}
}

func sessionKey(p Provider, s string) string { return string(p) + ":" + s }

func (r *Reducer) Submit(e AgentEvent) error {
	if err := e.Validate(); err != nil {
		return err
	}
	// Project/worktree inspection is deliberately outside the Store lock. The
	// resulting bounded identity is consumed in the same single Store.Update as
	// the lifecycle mutation.
	enrichment := eventEnrichment{}
	if e.EventType == EventUserPromptSubmit && e.Cwd != nil {
		enrichment.Project = resolveProjectContext(*e.Cwd)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[e.EventID]; ok {
		return nil
	}
	key := sessionKey(e.Provider, e.SessionID)
	m := r.sessions[key]
	if m == nil {
		m = &sessionMeta{}
		r.sessions[key] = m
		r.sessionOrder = append(r.sessionOrder, key)
		for len(r.sessionOrder) > r.cfg.MaxSessions {
			old := r.sessionOrder[0]
			r.sessionOrder = r.sessionOrder[1:]
			delete(r.sessions, old)
		}
	}
	err := r.store.Update(func(root *state.InternalRootState) error {
		return r.reduce(root, m, e, enrichment)
	})
	if err == nil {
		r.rememberEventID(e.EventID)
	}
	return err
}

func (r *Reducer) rememberEventID(id string) {
	r.seen[id] = struct{}{}
	r.seenOrder = append(r.seenOrder, id)
	for len(r.seenOrder) > r.cfg.MaxSeenEventIDs {
		old := r.seenOrder[0]
		r.seenOrder = r.seenOrder[1:]
		delete(r.seen, old)
	}
}

func (r *Reducer) reduce(root *state.InternalRootState, m *sessionMeta, e AgentEvent, enrichment eventEnrichment) error {
	if e.EventType == EventSessionEnd && !m.LatestSessionEventAt.IsZero() && e.OccurredAt.Before(m.LatestSessionEventAt) {
		return nil
	}
	if e.EventType != EventUserPromptSubmit && e.EventType != EventSessionEnd &&
		!m.SessionEndedAt.IsZero() && e.TurnID != nil && *e.TurnID == m.CurrentTurnID {
		return nil
	}

	root.GeneratedAt = e.OccurredAt
	if root.Sources == nil {
		root.Sources = map[string]state.SourceHealth{}
	}

	if e.EventType == EventUserPromptSubmit {
		r.observeCapabilities(e)
		r.setSourceFromCapabilities(root, e.Provider, e.OccurredAt, true)
		return r.beginTurn(root, m, e, enrichment.Project)
	}
	if e.EventType == EventSessionEnd {
		r.setSourceFromCapabilities(root, e.Provider, e.OccurredAt, true)
		return r.sessionEnd(root, m, e)
	}
	if e.TurnID == nil || *e.TurnID == "" {
		caps := r.capabilitiesFor(e.Provider)
		caps.turnAttributionDegraded = true
		// A syntactically valid but unattributable event is an attempt, not a
		// successful accepted monitoring observation.
		r.setSourceFromCapabilities(root, e.Provider, e.OccurredAt, false)
		if a := findAgent(root, keyForEvent(e)); a != nil && a.CurrentTurn.Activity != state.ActivityIdle && a.CurrentTurn.Activity != state.ActivityError {
			a.CurrentTurn.Freshness = state.FreshnessStale
			r.upsertAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), e.OccurredAt, nil, nil)
		}
		return nil
	}

	r.observeCapabilities(e)
	r.setSourceFromCapabilities(root, e.Provider, e.OccurredAt, true)
	if e.Provider == ProviderClaude && e.EventType == EventStop && (e.Metadata.BackgroundTaskCount == nil || e.Metadata.SessionCronCount == nil) {
		if a := findAgent(root, keyForEvent(e)); a != nil && a.CurrentTurn.TurnID == *e.TurnID && a.CurrentTurn.Activity != state.ActivityError {
			a.CurrentTurn.Freshness = state.FreshnessStale
			r.upsertAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), e.OccurredAt, nil, nil)
			if t := currentTask(root, e); t != nil {
				t.Freshness = state.FreshnessStale
				t.Attention = nil
				t.UpdatedAt = e.OccurredAt
			}
		}
		return nil
	}

	turn := *e.TurnID
	if m.CurrentTurnID == "" || turn != m.CurrentTurnID {
		return nil
	}
	if !m.LatestTurnEventAt.IsZero() && e.OccurredAt.Before(m.LatestTurnEventAt) {
		return nil
	}
	m.LatestTurnEventAt = e.OccurredAt
	if e.OccurredAt.After(m.LatestSessionEventAt) {
		m.LatestSessionEventAt = e.OccurredAt
	}
	a := findAgent(root, keyForEvent(e))
	if a == nil {
		return nil
	}
	t := currentTask(root, e)
	stickyAttention := t != nil && t.Attention != nil && !canClearTaskAttention(t.Attention, e)

	switch e.EventType {
	case EventPreToolUse:
		if stickyAttention {
			r.refreshAttentionActivity(a, e.OccurredAt)
		} else {
			r.applyWorking(root, a, e.OccurredAt)
		}
	case EventAskUserQuestion, EventPermissionRequest, EventElicitation:
		r.applyAttention(root, a, e.OccurredAt)
	case EventPostToolUse, EventPostToolUseFailure, EventPermissionDenied, EventElicitationResult,
		EventSubagentStart, EventSubagentStop, EventTaskCreated, EventTaskCompleted:
		if stickyAttention {
			r.refreshAttentionActivity(a, e.OccurredAt)
		} else {
			r.applyWorking(root, a, e.OccurredAt)
		}
	case EventNotification:
		r.applyNotification(root, a, e)
	case EventStop:
		if e.Provider == ProviderClaude {
			bg, cr := 0, 0
			if e.Metadata.BackgroundTaskCount != nil {
				bg = *e.Metadata.BackgroundTaskCount
			}
			if e.Metadata.SessionCronCount != nil {
				cr = *e.Metadata.SessionCronCount
			}
			if bg > 0 || cr > 0 {
				r.applyWorking(root, a, e.OccurredAt)
				if t != nil {
					t.Attention = nil
					t.Completion = nil // raw final material from this non-terminal Stop is discarded.
					setTaskCheckpoint(t, state.CheckpointBackgroundWait, checkpointText(state.CheckpointBackgroundWait, nil), e.OccurredAt, false)
					syncTaskLifecycle(t, a, e.OccurredAt)
				}
				return nil
			}
		}
		r.applyComplete(root, a, e.OccurredAt)
	case EventStopFailure:
		if e.Provider == ProviderClaude {
			r.applyError(root, a, e.OccurredAt)
		}
	}

	if t != nil {
		applyTaskEvent(t, a, e)
		if e.EventType == EventStop && a.CurrentTurn.Outcome == state.OutcomeCompleted {
			t.Attention = nil
			if e.Metadata.CompletionSummary != nil || e.Metadata.ResultIdentifier != nil {
				t.Completion = &state.TaskCompletion{
					Summary:          cloneStringPtr(e.Metadata.CompletionSummary),
					ResultIdentifier: cloneStringPtr(e.Metadata.ResultIdentifier),
					At:               e.OccurredAt,
				}
			} else {
				t.Completion = nil
			}
		}
	}
	return nil
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func (r *Reducer) capabilitiesFor(p Provider) *sourceCapabilityState {
	caps := r.capabilities[p]
	if caps == nil {
		caps = &sourceCapabilityState{}
		r.capabilities[p] = caps
	}
	return caps
}

func (r *Reducer) observeCapabilities(e AgentEvent) {
	caps := r.capabilitiesFor(e.Provider)
	if reliableTurnIdentity(e) {
		caps.turnAttributionDegraded = false
	}
	if e.Provider == ProviderClaude && e.EventType == EventUserPromptSubmit && e.Metadata.SyntheticTurnIdentity {
		caps.turnAttributionDegraded = true
	}
	if e.Provider == ProviderClaude && e.EventType == EventStop {
		caps.claudeStopDegraded = e.Metadata.BackgroundTaskCount == nil || e.Metadata.SessionCronCount == nil
	}
}

func reliableTurnIdentity(e AgentEvent) bool {
	return e.TurnID != nil && *e.TurnID != "" && !e.Metadata.SyntheticTurnIdentity && !strings.HasPrefix(*e.TurnID, "synthetic:")
}

func (r *Reducer) setSourceFromCapabilities(root *state.InternalRootState, p Provider, at time.Time, accepted bool) {
	caps := r.capabilitiesFor(p)
	status := state.SourceAvailable
	msg := "Lifecycle event observed."
	switch {
	case caps.turnAttributionDegraded && caps.claudeStopDegraded:
		status = state.SourceDegraded
		msg = "Lifecycle capability degraded: reliable turn attribution and Claude Stop background-work fields unavailable."
	case caps.turnAttributionDegraded:
		status = state.SourceDegraded
		msg = "Lifecycle capability degraded: reliable turn attribution unavailable."
	case caps.claudeStopDegraded:
		status = state.SourceDegraded
		msg = "Lifecycle capability degraded: Claude Stop background-work fields unavailable."
	}
	setSource(root, sourceID(p), status, at, msg, accepted)
}

func sourceID(p Provider) string {
	if p == ProviderCodex {
		return "codex-hooks"
	}
	return "claude-hooks"
}

func setSource(root *state.InternalRootState, id string, status state.SourceStatus, at time.Time, msg string, accepted bool) {
	prev := root.Sources[id]
	x := at
	prev.Status = status
	if prev.LastAttemptAt == nil || !at.Before(*prev.LastAttemptAt) {
		prev.LastAttemptAt = &x
	}
	if accepted && (prev.LastSuccessAt == nil || !at.Before(*prev.LastSuccessAt)) {
		prev.LastSuccessAt = &x
	}
	prev.Message = msg
	root.Sources[id] = prev
}

func keyForEvent(e AgentEvent) string { return string(e.Provider) + ":" + e.SessionID }

func findAgent(root *state.InternalRootState, id string) *state.AgentState {
	for i := range root.Agents {
		if root.Agents[i].ID == id {
			return &root.Agents[i]
		}
	}
	return nil
}

func (r *Reducer) beginTurn(root *state.InternalRootState, m *sessionMeta, e AgentEvent, project *state.TaskProjectContext) error {
	if e.TurnID == nil || *e.TurnID == "" {
		return fmt.Errorf("begin turn missing turn id")
	}
	turn := *e.TurnID
	if containsString(m.OldTurns, turn) {
		return nil
	}
	if m.CurrentTurnID == turn {
		return nil
	}
	if m.CurrentTurnID != "" && turn != m.CurrentTurnID && !m.LatestSessionEventAt.IsZero() && e.OccurredAt.Before(m.LatestSessionEventAt) {
		return nil
	}
	if m.CurrentTurnID != "" && turn != m.CurrentTurnID {
		m.OldTurns = append(m.OldTurns, m.CurrentTurnID)
		if len(m.OldTurns) > r.cfg.MaxOldTurnsPerSession {
			m.OldTurns = m.OldTurns[len(m.OldTurns)-r.cfg.MaxOldTurnsPerSession:]
		}
	}
	m.CurrentTurnID = turn
	m.CurrentTurnStartedAt = e.OccurredAt
	m.SessionEndedAt = time.Time{}
	m.LatestTurnEventAt = e.OccurredAt
	if e.OccurredAt.After(m.LatestSessionEventAt) {
		m.LatestSessionEventAt = e.OccurredAt
	}

	id := keyForEvent(e)
	a := findAgent(root, id)
	if a == nil {
		root.Agents = append(root.Agents, state.AgentState{ID: id, Provider: string(e.Provider), SessionID: e.SessionID})
		a = &root.Agents[len(root.Agents)-1]
	}
	a.CurrentTurn = state.CurrentTurn{TurnID: turn, Activity: state.ActivityWorking, Outcome: state.OutcomeNone, Freshness: state.FreshnessFresh, StartedAt: e.OccurredAt, UpdatedAt: e.OccurredAt}
	r.resolveAllAgentAlerts(root, id, e.OccurredAt)

	// New turns do not overwrite retained terminal delivery cards. Any abandoned
	// non-terminal card for the same provider session is removed.
	out := root.Tasks[:0]
	for _, existing := range root.Tasks {
		if existing.Provider == string(e.Provider) && existing.SessionID == e.SessionID && existing.TurnID != turn && existing.Lifecycle != state.TaskComplete && existing.Lifecycle != state.TaskError {
			continue
		}
		out = append(out, existing)
	}
	root.Tasks = out
	root.Tasks = append(root.Tasks, newTaskState(e, project))
	return nil
}

func (r *Reducer) sessionEnd(root *state.InternalRootState, m *sessionMeta, e AgentEvent) error {
	if !m.LatestSessionEventAt.IsZero() && e.OccurredAt.Before(m.LatestSessionEventAt) {
		return nil
	}
	m.LatestSessionEventAt = e.OccurredAt
	m.SessionEndedAt = e.OccurredAt
	a := findAgent(root, keyForEvent(e))
	if a == nil {
		return nil
	}
	a.CurrentTurn.Activity = state.ActivityIdle
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.UpdatedAt = e.OccurredAt
	r.resolveAlert(root, state.AlertAttention, a.ID, nil, e.OccurredAt)
	r.resolveAlert(root, state.AlertStale, a.ID, nil, e.OccurredAt)
	if m.CurrentTurnID != "" {
		if t := findTask(root, e.Provider, e.SessionID, m.CurrentTurnID); t != nil {
			t.Attention = nil
			syncTaskLifecycle(t, a, e.OccurredAt)
			if a.CurrentTurn.Outcome == state.OutcomeNone {
				t.Freshness = state.FreshnessStale
				t.Lifecycle = state.TaskWorking
			}
		}
	}
	return nil
}

func (r *Reducer) applyWorking(root *state.InternalRootState, a *state.AgentState, at time.Time) {
	a.CurrentTurn.Activity = state.ActivityWorking
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.UpdatedAt = at
	if a.CurrentTurn.Outcome != state.OutcomeNone {
		a.CurrentTurn.Outcome = state.OutcomeNone
		a.CurrentTurn.CompletedAt = nil
	}
	r.resolveAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertComplete, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertError, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
}

func (r *Reducer) refreshAttentionActivity(a *state.AgentState, at time.Time) {
	if a == nil {
		return
	}
	a.CurrentTurn.Activity = state.ActivityAttention
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.UpdatedAt = at
}

func (r *Reducer) applyAttention(root *state.InternalRootState, a *state.AgentState, at time.Time) {
	if a.CurrentTurn.Outcome != state.OutcomeNone {
		a.CurrentTurn.Outcome = state.OutcomeNone
		a.CurrentTurn.CompletedAt = nil
		r.resolveAlert(root, state.AlertComplete, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
		r.resolveAlert(root, state.AlertError, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	}
	a.CurrentTurn.Activity = state.ActivityAttention
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.UpdatedAt = at
	r.resolveAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.upsertAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), at, nil, nil)
}

func (r *Reducer) applyComplete(root *state.InternalRootState, a *state.AgentState, at time.Time) {
	a.CurrentTurn.Activity = state.ActivityIdle
	a.CurrentTurn.Outcome = state.OutcomeCompleted
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.CompletedAt = timePtrLocal(at)
	a.CurrentTurn.UpdatedAt = at
	r.resolveAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertError, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	hi := at.Add(r.cfg.CompleteHighVisibility)
	ret := at.Add(r.cfg.CompleteRetention)
	r.upsertAlert(root, state.AlertComplete, a.ID, turnPtr(a.CurrentTurn.TurnID), at, &hi, &ret)
}

func (r *Reducer) applyError(root *state.InternalRootState, a *state.AgentState, at time.Time) {
	a.CurrentTurn.Activity = state.ActivityError
	a.CurrentTurn.Outcome = state.OutcomeFailed
	a.CurrentTurn.Freshness = state.FreshnessFresh
	a.CurrentTurn.CompletedAt = nil
	a.CurrentTurn.UpdatedAt = at
	r.resolveAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.resolveAlert(root, state.AlertComplete, a.ID, turnPtr(a.CurrentTurn.TurnID), at)
	r.upsertAlert(root, state.AlertError, a.ID, turnPtr(a.CurrentTurn.TurnID), at, nil, nil)
}

func (r *Reducer) applyNotification(root *state.InternalRootState, a *state.AgentState, e AgentEvent) {
	if e.Metadata.NotificationType == nil {
		return
	}
	switch *e.Metadata.NotificationType {
	case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog":
		r.applyAttention(root, a, e.OccurredAt)
	case "idle_prompt":
		a.CurrentTurn.Activity = state.ActivityIdle
		a.CurrentTurn.Freshness = state.FreshnessFresh
		a.CurrentTurn.UpdatedAt = e.OccurredAt
		r.resolveAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), e.OccurredAt)
		r.resolveAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), e.OccurredAt)
	}
}

func (r *Reducer) upsertAlert(root *state.InternalRootState, typ state.AlertType, agentID string, turnID *string, at time.Time, hi, ret *time.Time) {
	for i := range root.Alerts {
		a := &root.Alerts[i]
		if a.Type == typ && a.AgentID == agentID && sameTurn(a.TurnID, turnID) {
			a.Active = true
			a.UpdatedAt = at
			if hi != nil {
				a.HighVisibilityUntil = timePtrLocal(*hi)
			}
			if ret != nil {
				a.RetainUntil = timePtrLocal(*ret)
			}
			return
		}
	}
	id, err := NewAlertID()
	if err != nil {
		turn := ""
		if turnID != nil {
			turn = *turnID
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", typ, agentID, turn, at.UnixNano())))
		id = "alert-" + hex.EncodeToString(sum[:16])
	}
	root.Alerts = append(root.Alerts, state.AlertState{AlertID: id, Type: typ, AgentID: agentID, TurnID: cloneTurn(turnID), Active: true, CreatedAt: at, UpdatedAt: at, HighVisibilityUntil: cloneTimeLocal(hi), RetainUntil: cloneTimeLocal(ret)})
}

func (r *Reducer) resolveAlert(root *state.InternalRootState, typ state.AlertType, agentID string, turnID *string, at time.Time) {
	for i := range root.Alerts {
		a := &root.Alerts[i]
		if !a.Active || a.Type != typ || a.AgentID != agentID {
			continue
		}
		if turnID != nil && !sameTurn(a.TurnID, turnID) {
			continue
		}
		a.Active = false
		a.UpdatedAt = at
	}
}

func (r *Reducer) resolveAllAgentAlerts(root *state.InternalRootState, agentID string, at time.Time) {
	for i := range root.Alerts {
		if root.Alerts[i].AgentID == agentID && root.Alerts[i].Active {
			root.Alerts[i].Active = false
			root.Alerts[i].UpdatedAt = at
		}
	}
}

func (r *Reducer) Maintenance(now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.Update(func(root *state.InternalRootState) error {
		for i := range root.Agents {
			a := &root.Agents[i]
			if (a.CurrentTurn.Activity == state.ActivityWorking || a.CurrentTurn.Activity == state.ActivityAttention) && a.CurrentTurn.Freshness == state.FreshnessFresh && !a.CurrentTurn.UpdatedAt.IsZero() && now.Sub(a.CurrentTurn.UpdatedAt) > r.cfg.StaleAfter {
				wasAttention := a.CurrentTurn.Activity == state.ActivityAttention
				a.CurrentTurn.Freshness = state.FreshnessStale
				if wasAttention {
					a.CurrentTurn.Activity = state.ActivityWorking
					r.resolveAlert(root, state.AlertAttention, a.ID, turnPtr(a.CurrentTurn.TurnID), now)
				}
				r.upsertAlert(root, state.AlertStale, a.ID, turnPtr(a.CurrentTurn.TurnID), now, nil, nil)
				for j := range root.Tasks {
					t := &root.Tasks[j]
					if t.Provider == a.Provider && t.SessionID == a.SessionID && t.TurnID == a.CurrentTurn.TurnID {
						t.Freshness = state.FreshnessStale
						t.Lifecycle = state.TaskWorking
						t.Attention = nil
						t.UpdatedAt = now
					}
				}
			}
		}

		alerts := root.Alerts[:0]
		for _, a := range root.Alerts {
			if !a.Active {
				continue
			}
			if a.Type == state.AlertComplete && a.RetainUntil != nil && !now.Before(*a.RetainUntil) {
				continue
			}
			alerts = append(alerts, a)
		}
		root.Alerts = alerts

		tasks := root.Tasks[:0]
		for _, t := range root.Tasks {
			if t.Lifecycle == state.TaskComplete && !t.UpdatedAt.IsZero() && !now.Before(t.UpdatedAt.Add(r.cfg.CompleteRetention)) {
				continue
			}
			tasks = append(tasks, t)
		}
		root.Tasks = tasks
		root.GeneratedAt = now
		return nil
	})
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func turnPtr(s string) *string {
	if s == "" {
		return nil
	}
	x := s
	return &x
}

func sameTurn(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneTurn(v *string) *string {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func cloneTimeLocal(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func timePtrLocal(v time.Time) *time.Time { x := v; return &x }
