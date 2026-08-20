package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func testReducer(t *testing.T) (*state.Store, *Reducer, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "host", DisplayName: "Host"}))
	r := NewReducer(st, ReducerConfig{StaleAfter: 10 * time.Minute, CompleteHighVisibility: 5 * time.Minute, CompleteRetention: 30 * time.Minute, MaxSeenEventIDs: 64, MaxOldTurnsPerSession: 4})
	return st, r, now
}
func ev(p Provider, s, turn string, et EventType, at time.Time) AgentEvent {
	var tp *string
	if turn != "" {
		x := turn
		tp = &x
	}
	return AgentEvent{SchemaVersion: 1, EventID: string(et) + s + turn + at.Format(time.RFC3339Nano), Provider: p, SessionID: s, TurnID: tp, EventType: et, OccurredAt: at, Metadata: Metadata{}}
}
func agentByID(t *testing.T, st *state.Store, id string) state.AgentState {
	t.Helper()
	for _, a := range st.Snapshot().Agents {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("missing agent %s", id)
	return state.AgentState{}
}
func TestReducerMatrix(t *testing.T) {
	st, r, now := testReducer(t)
	prompt := ev(ProviderCodex, "s", "a", EventUserPromptSubmit, now.Add(time.Minute))
	if err := r.Submit(prompt); err != nil {
		t.Fatal(err)
	}
	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityWorking {
		t.Fatal(a.CurrentTurn.Activity)
	}
	perm := ev(ProviderCodex, "s", "a", EventPermissionRequest, now.Add(2*time.Minute))
	_ = r.Submit(perm)
	a = agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityAttention {
		t.Fatal(a.CurrentTurn.Activity)
	}
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventPostToolUse, now.Add(3*time.Minute)))
	a = agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityWorking {
		t.Fatal(a.CurrentTurn.Activity)
	}
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventStop, now.Add(4*time.Minute)))
	a = agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeCompleted {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventSessionEnd, now.Add(5*time.Minute)))
	a = agentByID(t, st, "codex:s")
	if a.CurrentTurn.Outcome != state.OutcomeCompleted {
		t.Fatal("session end erased completion")
	}
}
func TestSameTurnResumeClearsCompletion(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventStop, now.Add(time.Minute)))
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventPreToolUse, now.Add(2*time.Minute)))
	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityWorking || a.CurrentTurn.Outcome != state.OutcomeNone || a.CurrentTurn.CompletedAt != nil {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	for _, al := range st.Snapshot().Alerts {
		if al.Type == state.AlertComplete && al.Active {
			t.Fatal("complete alert remained")
		}
	}
}
func TestOldTurnDuplicateAndLateBeginCannotRollback(t *testing.T) {
	st, r, now := testReducer(t)
	a := ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now)
	_ = r.Submit(a)
	b := ev(ProviderCodex, "s", "B", EventUserPromptSubmit, now.Add(2*time.Minute))
	_ = r.Submit(b)
	old := ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(3*time.Minute))
	_ = r.Submit(old)
	late := ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now.Add(time.Minute))
	late.EventID = "late"
	_ = r.Submit(late)
	got := agentByID(t, st, "codex:s")
	if got.CurrentTurn.TurnID != "B" || got.CurrentTurn.Activity != state.ActivityWorking {
		t.Fatalf("%+v", got.CurrentTurn)
	}
	dup := ev(ProviderCodex, "s", "B", EventPermissionRequest, now.Add(4*time.Minute))
	dup.EventID = "dup"
	_ = r.Submit(dup)
	_ = r.Submit(dup)
	count := 0
	for _, al := range st.Snapshot().Alerts {
		if al.Type == state.AlertAttention && al.Active {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("attention alerts=%d", count)
	}
}
func TestOlderCurrentEventCannotRollback(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(3*time.Minute)))
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventPostToolUse, now.Add(2*time.Minute)))
	if got := agentByID(t, st, "codex:s").CurrentTurn.Activity; got != state.ActivityAttention {
		t.Fatalf("got %s", got)
	}
}
func TestClaudeStopBackgroundNotCompleteAndFailureError(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	stop := ev(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	stop.Metadata.BackgroundTaskCount = ptrInt(1)
	stop.Metadata.SessionCronCount = ptrInt(0)
	_ = r.Submit(stop)
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Outcome != state.OutcomeNone || a.CurrentTurn.Activity != state.ActivityWorking {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventStopFailure, now.Add(2*time.Minute)))
	a = agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityError || a.CurrentTurn.Outcome != state.OutcomeFailed {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	_ = r.Submit(ev(ProviderClaude, "s", "q", EventUserPromptSubmit, now.Add(3*time.Minute)))
	a = agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityWorking || a.CurrentTurn.Outcome != state.OutcomeNone {
		t.Fatalf("%+v", a.CurrentTurn)
	}
}
func TestAttentionSourcesAndResume(t *testing.T) {
	for _, kind := range []string{"ask", "permission", "elicitation", "notification"} {
		t.Run(kind, func(t *testing.T) {
			st, r, now := testReducer(t)
			_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
			e := ev(ProviderClaude, "s", "p", EventPreToolUse, now.Add(time.Minute))
			switch kind {
			case "ask":
				e.EventType = EventAskUserQuestion
				e.Metadata.ToolName = ptrString("AskUserQuestion")
			case "permission":
				e.EventType = EventPermissionRequest
			case "elicitation":
				e.EventType = EventElicitation
			case "notification":
				e.EventType = EventNotification
				e.Metadata.NotificationType = ptrString("permission_prompt")
			}
			e.EventID = kind
			_ = r.Submit(e)
			if got := agentByID(t, st, "claude-code:s").CurrentTurn.Activity; got != state.ActivityAttention {
				t.Fatalf("got %s", got)
			}
			resume := ev(ProviderClaude, "s", "p", EventPostToolUse, now.Add(2*time.Minute))
			resume.EventID = kind + "r"
			_ = r.Submit(resume)
			if got := agentByID(t, st, "claude-code:s").CurrentTurn.Activity; got != state.ActivityWorking {
				t.Fatalf("got %s", got)
			}
		})
	}
}
func TestMissingClaudeTurnDegradesAndMarksStale(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	e := ev(ProviderClaude, "s", "", EventPostToolUse, now.Add(time.Minute))
	_ = r.Submit(e)
	snap := st.Snapshot()
	if snap.Sources["claude-hooks"].Status != state.SourceDegraded {
		t.Fatal("source not degraded")
	}
	if agentByID(t, st, "claude-code:s").CurrentTurn.Freshness != state.FreshnessStale {
		t.Fatal("agent not stale")
	}
}
func TestStaleMaintenanceAndFreshRecovery(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventUserPromptSubmit, now))
	_ = r.Maintenance(now.Add(11 * time.Minute))
	if agentByID(t, st, "codex:s").CurrentTurn.Freshness != state.FreshnessStale {
		t.Fatal("not stale")
	}
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventPostToolUse, now.Add(12*time.Minute)))
	if agentByID(t, st, "codex:s").CurrentTurn.Freshness != state.FreshnessFresh {
		t.Fatal("not fresh")
	}
}
func TestCompleteAlertTTL(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventStop, now.Add(time.Minute)))
	if len(st.Snapshot().Alerts) == 0 {
		t.Fatal("missing alert")
	}
	_ = r.Maintenance(now.Add(32 * time.Minute))
	for _, a := range st.Snapshot().Alerts {
		if a.Type == state.AlertComplete {
			t.Fatal("expired complete alert retained")
		}
	}
}
func TestMultiSessionIsolation(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "A", "a", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderClaude, "B", "b", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderClaude, "B", "b", EventPermissionRequest, now.Add(time.Minute)))
	_ = r.Submit(ev(ProviderCodex, "C", "c", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "C", "c", EventStop, now.Add(time.Minute)))
	a := agentByID(t, st, "codex:A")
	b := agentByID(t, st, "claude-code:B")
	c := agentByID(t, st, "codex:C")
	if a.CurrentTurn.Activity != state.ActivityWorking || b.CurrentTurn.Activity != state.ActivityAttention || c.CurrentTurn.Outcome != state.OutcomeCompleted {
		t.Fatalf("A=%+v B=%+v C=%+v", a.CurrentTurn, b.CurrentTurn, c.CurrentTurn)
	}
	_ = r.Submit(ev(ProviderCodex, "A", "a", EventPostToolUse, now.Add(2*time.Minute)))
	if agentByID(t, st, "claude-code:B").CurrentTurn.Activity != state.ActivityAttention || agentByID(t, st, "codex:C").CurrentTurn.Outcome != state.OutcomeCompleted {
		t.Fatal("session cross-talk")
	}
}

func TestClaudeRecoverableEventsRemainWorking(t *testing.T) {
	for _, et := range []EventType{EventPostToolUseFailure, EventPermissionDenied, EventElicitationResult} {
		t.Run(string(et), func(t *testing.T) {
			st, r, now := testReducer(t)
			_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
			att := ev(ProviderClaude, "s", "p", EventPermissionRequest, now.Add(time.Second))
			att.EventID = "att"
			_ = r.Submit(att)
			e := ev(ProviderClaude, "s", "p", et, now.Add(2*time.Second))
			e.EventID = string(et)
			_ = r.Submit(e)
			a := agentByID(t, st, "claude-code:s")
			if a.CurrentTurn.Activity != state.ActivityWorking || a.CurrentTurn.Outcome != state.OutcomeNone {
				t.Fatalf("%+v", a.CurrentTurn)
			}
		})
	}
}
func TestClaudeNotificationVariantsAndIdleFallback(t *testing.T) {
	for _, n := range []string{"permission_prompt", "elicitation_dialog", "elicitation_url_dialog"} {
		t.Run(n, func(t *testing.T) {
			st, r, now := testReducer(t)
			_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
			e := ev(ProviderClaude, "s", "p", EventNotification, now.Add(time.Second))
			e.EventID = n
			e.Metadata.NotificationType = ptrString(n)
			_ = r.Submit(e)
			if agentByID(t, st, "claude-code:s").CurrentTurn.Activity != state.ActivityAttention {
				t.Fatal("notification did not request attention")
			}
		})
	}
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	idle := ev(ProviderClaude, "s", "p", EventNotification, now.Add(time.Second))
	idle.EventID = "idle"
	idle.Metadata.NotificationType = ptrString("idle_prompt")
	_ = r.Submit(idle)
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeNone {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	unknown := ev(ProviderClaude, "s", "p", EventNotification, now.Add(2*time.Second))
	unknown.EventID = "unknown"
	unknown.Metadata.NotificationType = ptrString("other")
	_ = r.Submit(unknown)
	a = agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle {
		t.Fatalf("unknown notification mutated state: %+v", a.CurrentTurn)
	}
}
func TestClaudeStopWithSessionCronNotComplete(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	e := ev(ProviderClaude, "s", "p", EventStop, now.Add(time.Second))
	e.Metadata.BackgroundTaskCount = ptrInt(0)
	e.Metadata.SessionCronCount = ptrInt(1)
	_ = r.Submit(e)
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityWorking || a.CurrentTurn.Outcome != state.OutcomeNone {
		t.Fatalf("%+v", a.CurrentTurn)
	}
}
func TestSessionEndPreservesFailureAndErrorAlert(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventStopFailure, now.Add(time.Second)))
	end := ev(ProviderClaude, "s", "", EventSessionEnd, now.Add(2*time.Second))
	_ = r.Submit(end)
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeFailed {
		t.Fatalf("%+v", a.CurrentTurn)
	}
	found := false
	for _, al := range st.Snapshot().Alerts {
		if al.Type == state.AlertError && al.Active {
			found = true
		}
	}
	if !found {
		t.Fatal("terminal error alert erased")
	}
}
func TestSourceHealthNotDegradedByNormalSilence(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "a", EventStop, now.Add(time.Second)))
	_ = r.Maintenance(now.Add(time.Hour))
	if st.Snapshot().Sources["codex-hooks"].Status != state.SourceAvailable {
		t.Fatal("normal silence degraded source")
	}
}

func TestSameTurnBeginDoesNotRestartTurn(t *testing.T) {
	st, r, now := testReducer(t)
	first := ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now)
	first.EventID = "begin-1"
	if err := r.Submit(first); err != nil {
		t.Fatal(err)
	}
	if err := r.Submit(ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	second := ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now.Add(2*time.Minute))
	second.EventID = "begin-2"
	if err := r.Submit(second); err != nil {
		t.Fatal(err)
	}
	a := agentByID(t, st, "codex:s")
	if !a.CurrentTurn.StartedAt.Equal(now) || a.CurrentTurn.Activity != state.ActivityAttention {
		t.Fatalf("same-turn begin restarted state: %+v", a.CurrentTurn)
	}
}

func TestSourceHealthTimestampsDoNotRollBackward(t *testing.T) {
	st, r, now := testReducer(t)
	if err := r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	late := ev(ProviderCodex, "s", "A", EventPostToolUse, now.Add(time.Minute))
	late.EventID = "late-source-event"
	if err := r.Submit(late); err != nil {
		t.Fatal(err)
	}
	src := st.Snapshot().Sources["codex-hooks"]
	want := now.Add(2 * time.Minute)
	if src.LastAttemptAt == nil || src.LastSuccessAt == nil || !src.LastAttemptAt.Equal(want) || !src.LastSuccessAt.Equal(want) {
		t.Fatalf("source timestamps rolled backward: %+v", src)
	}
}

func TestDegradedUnattributedEventDoesNotClaimNewSuccess(t *testing.T) {
	st, r, now := testReducer(t)
	if err := r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now)); err != nil {
		t.Fatal(err)
	}
	unattributed := ev(ProviderClaude, "s", "", EventPostToolUse, now.Add(time.Minute))
	unattributed.EventID = "unattributed"
	if err := r.Submit(unattributed); err != nil {
		t.Fatal(err)
	}
	src := st.Snapshot().Sources["claude-hooks"]
	if src.Status != state.SourceDegraded || src.LastAttemptAt == nil || !src.LastAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected degraded source: %+v", src)
	}
	if src.LastSuccessAt == nil || !src.LastSuccessAt.Equal(now) {
		t.Fatalf("unattributed event incorrectly advanced success: %+v", src)
	}
}

func TestReducerSessionMetadataIsBounded(t *testing.T) {
	st := state.NewStore(state.LiveInitialState(time.Unix(0, 0).UTC(), state.HostState{ID: "host"}))
	r := NewReducer(st, ReducerConfig{MaxSessions: 3})
	base := time.Unix(100, 0).UTC()
	for i := 0; i < 6; i++ {
		s := fmt.Sprintf("s%d", i)
		if err := r.Submit(ev(ProviderCodex, s, "t", EventUserPromptSubmit, base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(r.sessions); got != 3 {
		t.Fatalf("session metadata size=%d want 3", got)
	}
}

func TestMaintenancePrunesResolvedNonCompleteAlerts(t *testing.T) {
	st, r, now := testReducer(t)
	if err := r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now)); err != nil {
		t.Fatal(err)
	}
	if err := r.Submit(ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := r.Submit(ev(ProviderCodex, "s", "A", EventPostToolUse, now.Add(2*time.Second))); err != nil {
		t.Fatal(err)
	}
	if len(st.Snapshot().Alerts) == 0 {
		t.Fatal("expected resolved attention alert before maintenance")
	}
	if err := r.Maintenance(now.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, al := range st.Snapshot().Alerts {
		if !al.Active {
			t.Fatalf("resolved alert retained: %+v", al)
		}
	}
}

func TestClaudeCleanStopRequiresObservedBackgroundCapability(t *testing.T) {
	st, r, now := testReducer(t)
	if err := r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now)); err != nil {
		t.Fatal(err)
	}
	clean := ev(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	clean.Metadata.BackgroundTaskCount = ptrInt(0)
	clean.Metadata.SessionCronCount = ptrInt(0)
	if err := r.Submit(clean); err != nil {
		t.Fatal(err)
	}
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeCompleted {
		t.Fatalf("clean Claude Stop did not complete: %+v", a.CurrentTurn)
	}
}

func TestClaudeStopWithoutBackgroundCapabilityDegradesInsteadOfCompleting(t *testing.T) {
	st, r, now := testReducer(t)
	if err := r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now)); err != nil {
		t.Fatal(err)
	}
	missing := ev(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	missing.EventID = "stop-missing-capability"
	if err := r.Submit(missing); err != nil {
		t.Fatal(err)
	}
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Outcome != state.OutcomeNone || a.CurrentTurn.Freshness != state.FreshnessStale {
		t.Fatalf("missing capability fabricated completion: %+v", a.CurrentTurn)
	}
	if st.Snapshot().Sources["claude-hooks"].Status != state.SourceDegraded {
		t.Fatal("missing capability did not degrade source")
	}
}
