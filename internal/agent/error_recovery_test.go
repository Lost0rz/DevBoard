package agent

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// These tests pin the recovered-error rule from the 2026-08-25 observability
// contract amendment: a Claude StopFailure error card from an older turn of a
// session stops occupying Pad READY once a newer turn of the same session
// terminates with a valid terminal Stop (background_tasks/session_crons both
// present and zero). Without such a recovery event the error stays auditable
// and is never rewritten to COMPLETE.
func TestRateLimitErrorSupersededByLaterTerminalStop(t *testing.T) {
	st, r, now := m4Fixture(t)
	session := "claude-s"
	rateLimited := "rate_limit"

	// Old turn: user prompt then a rate-limited StopFailure.
	submitOK(t, r, m4Event(ProviderClaude, session, "turn-old", EventUserPromptSubmit, now))
	failure := m4Event(ProviderClaude, session, "turn-old", EventStopFailure, now.Add(time.Minute))
	failure.Metadata.ErrorType = &rateLimited
	submitOK(t, r, failure)
	old := m4Task(t, st, ProviderClaude, session, "turn-old")
	if old.Lifecycle != state.TaskError || old.Attention == nil || old.Attention.Kind != state.AttentionRateLimited {
		t.Fatalf("old turn did not become a rate-limited error: %+v", old)
	}

	// New turn starts: the old error must remain auditable and unresolved.
	submitOK(t, r, m4Event(ProviderClaude, session, "turn-new", EventUserPromptSubmit, now.Add(2*time.Minute)))
	old = m4Task(t, st, ProviderClaude, session, "turn-old")
	if old.Lifecycle != state.TaskError || old.SupersededAt != nil {
		t.Fatalf("new prompt alone must not supersede the old error: %+v", old)
	}

	// New turn terminates with a valid terminal Stop (both background-work
	// fields present and zero).
	stop := m4Event(ProviderClaude, session, "turn-new", EventStop, now.Add(3*time.Minute))
	stop.Metadata.BackgroundTaskCount = ptrInt(0)
	stop.Metadata.SessionCronCount = ptrInt(0)
	submitOK(t, r, stop)

	fresh := m4Task(t, st, ProviderClaude, session, "turn-new")
	if fresh.Lifecycle != state.TaskComplete {
		t.Fatalf("new turn did not complete: %+v", fresh)
	}
	old = m4Task(t, st, ProviderClaude, session, "turn-old")
	if old.SupersededAt == nil {
		t.Fatal("old error was not superseded by the later terminal Stop")
	}
	if old.Lifecycle != state.TaskError {
		t.Fatalf("superseded error must stay an auditable error, not become %q", old.Lifecycle)
	}
	if !old.SupersededAt.Equal(now.Add(3 * time.Minute)) {
		t.Fatalf("superseded timestamp must be the terminal Stop time: %v", old.SupersededAt)
	}
}

func TestInvalidClaudeStopDoesNotSupersedePriorError(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop func(e AgentEvent)
	}{
		{
			name: "degraded stop without background fields",
			stop: func(e AgentEvent) {},
		},
		{
			name: "non-terminal stop with background work",
			stop: func(e AgentEvent) {
				e.Metadata.BackgroundTaskCount = ptrInt(1)
				e.Metadata.SessionCronCount = ptrInt(0)
			},
		},
		{
			name: "non-terminal stop with session crons",
			stop: func(e AgentEvent) {
				e.Metadata.BackgroundTaskCount = ptrInt(0)
				e.Metadata.SessionCronCount = ptrInt(2)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, r, now := m4Fixture(t)
			session := "claude-s"
			rateLimited := "rate_limit"
			submitOK(t, r, m4Event(ProviderClaude, session, "turn-old", EventUserPromptSubmit, now))
			failure := m4Event(ProviderClaude, session, "turn-old", EventStopFailure, now.Add(time.Minute))
			failure.Metadata.ErrorType = &rateLimited
			submitOK(t, r, failure)
			submitOK(t, r, m4Event(ProviderClaude, session, "turn-new", EventUserPromptSubmit, now.Add(2*time.Minute)))
			stop := m4Event(ProviderClaude, session, "turn-new", EventStop, now.Add(3*time.Minute))
			stop.EventID = "stop-" + tc.name
			tc.stop(stop)
			submitOK(t, r, stop)
			old := m4Task(t, st, ProviderClaude, session, "turn-old")
			if old.Lifecycle != state.TaskError || old.SupersededAt != nil {
				t.Fatalf("invalid terminal Stop must not supersede the old error: %+v", old)
			}
		})
	}
}

func TestUnrecoveredErrorStaysAuditable(t *testing.T) {
	st, r, now := m4Fixture(t)
	session := "claude-s"
	rateLimited := "rate_limit"
	submitOK(t, r, m4Event(ProviderClaude, session, "turn-a", EventUserPromptSubmit, now))
	failure := m4Event(ProviderClaude, session, "turn-a", EventStopFailure, now.Add(time.Minute))
	failure.Metadata.ErrorType = &rateLimited
	submitOK(t, r, failure)

	// No later turn, no maintenance magic: the error must survive untouched.
	if err := r.Maintenance(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	old := m4Task(t, st, ProviderClaude, session, "turn-a")
	if old.Lifecycle != state.TaskError || old.SupersededAt != nil || old.Completion != nil {
		t.Fatalf("unrecovered error must remain an auditable error without completion: %+v", old)
	}

	// A later turn that itself fails must not supersede the older error
	// either: only a successful terminal Stop recovers prior errors.
	submitOK(t, r, m4Event(ProviderClaude, session, "turn-b", EventUserPromptSubmit, now.Add(2*time.Hour)))
	failureB := m4Event(ProviderClaude, session, "turn-b", EventStopFailure, now.Add(2*time.Hour+time.Minute))
	failureB.Metadata.ErrorType = &rateLimited
	failureB.EventID = "failure-b"
	submitOK(t, r, failureB)
	old = m4Task(t, st, ProviderClaude, session, "turn-a")
	newer := m4Task(t, st, ProviderClaude, session, "turn-b")
	if old.SupersededAt != nil || newer.SupersededAt != nil {
		t.Fatalf("a failing later turn must not recover older errors: old=%+v newer=%+v", old, newer)
	}
}

func TestMaintenanceExpiresSupersededErrorsButKeepsUnrecovered(t *testing.T) {
	st, r, now := m4Fixture(t)
	session := "claude-s"
	rateLimited := "rate_limit"
	submitOK(t, r, m4Event(ProviderClaude, session, "turn-old", EventUserPromptSubmit, now))
	failure := m4Event(ProviderClaude, session, "turn-old", EventStopFailure, now.Add(time.Minute))
	failure.Metadata.ErrorType = &rateLimited
	submitOK(t, r, failure)

	// Unrelated session whose error stays unrecovered forever.
	submitOK(t, r, m4Event(ProviderClaude, "claude-stuck", "turn-x", EventUserPromptSubmit, now))
	stuck := m4Event(ProviderClaude, "claude-stuck", "turn-x", EventStopFailure, now.Add(time.Minute))
	stuck.Metadata.ErrorType = &rateLimited
	stuck.EventID = "stuck-failure"
	submitOK(t, r, stuck)

	submitOK(t, r, m4Event(ProviderClaude, session, "turn-new", EventUserPromptSubmit, now.Add(2*time.Minute)))
	stop := m4Event(ProviderClaude, session, "turn-new", EventStop, now.Add(3*time.Minute))
	stop.Metadata.BackgroundTaskCount = ptrInt(0)
	stop.Metadata.SessionCronCount = ptrInt(0)
	submitOK(t, r, stop)

	// Superseded errors stay auditable for the complete retention window...
	if err := r.Maintenance(now.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	m4Task(t, st, ProviderClaude, session, "turn-old")

	// ...then leave the internal state so recovered cards cannot accumulate.
	if err := r.Maintenance(now.Add(45 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, task := range st.Snapshot().Tasks {
		if task.SessionID == session && task.TurnID == "turn-old" {
			t.Fatalf("superseded error outlived retention: %+v", task)
		}
	}
	// The unrecovered error from the other session must still be auditable.
	stuckTask := m4Task(t, st, ProviderClaude, "claude-stuck", "turn-x")
	if stuckTask.Lifecycle != state.TaskError || stuckTask.SupersededAt != nil {
		t.Fatalf("unrecovered error must never be expired or superseded: %+v", stuckTask)
	}
}
