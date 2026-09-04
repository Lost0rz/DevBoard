package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func m4Fixture(t *testing.T) (*state.Store, *Reducer, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 22, 4, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "host", DisplayName: "Host"}))
	r := NewReducer(st, ReducerConfig{StaleAfter: 10 * time.Minute, CompleteHighVisibility: 5 * time.Minute, CompleteRetention: 30 * time.Minute, MaxSeenEventIDs: 64, MaxOldTurnsPerSession: 4, MaxSessions: 32})
	return st, r, now
}

func m4Event(p Provider, session, turn string, typ EventType, at time.Time) AgentEvent {
	var tp *string
	if turn != "" {
		tp = ptrString(turn)
	}
	return AgentEvent{SchemaVersion: 1, EventID: string(typ) + "-" + session + "-" + turn + "-" + at.Format(time.RFC3339Nano), Provider: p, SessionID: session, TurnID: tp, EventType: typ, OccurredAt: at, Metadata: Metadata{}}
}

func m4Task(t *testing.T, st *state.Store, provider Provider, session, turn string) state.TaskState {
	t.Helper()
	for _, task := range st.Snapshot().Tasks {
		if task.Provider == string(provider) && task.SessionID == session && task.TurnID == turn {
			return task
		}
	}
	t.Fatalf("missing task %s %s %s", provider, session, turn)
	return state.TaskState{}
}

func m4Agent(t *testing.T, st *state.Store, provider Provider, session string) state.AgentState {
	t.Helper()
	id := string(provider) + ":" + session
	for _, a := range st.Snapshot().Agents {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("missing agent %s", id)
	return state.AgentState{}
}

func submitOK(t *testing.T, r *Reducer, e AgentEvent) {
	t.Helper()
	if err := r.Submit(e); err != nil {
		t.Fatal(err)
	}
}

func TestM4TaskIdentityIsolationAndStableOpaqueIDs(t *testing.T) {
	st, r, now := m4Fixture(t)
	for _, tc := range []struct {
		p       Provider
		session string
		turn    string
	}{
		{ProviderCodex, "codex-A", "turn-1"},
		{ProviderCodex, "codex-B", "turn-1"},
		{ProviderClaude, "claude-A", "prompt-1"},
	} {
		e := m4Event(tc.p, tc.session, tc.turn, EventUserPromptSubmit, now)
		e.Cwd = ptrString(t.TempDir())
		e.Metadata.TaskTitle = ptrString("Implement task observability")
		submitOK(t, r, e)
	}
	first := m4Task(t, st, ProviderCodex, "codex-A", "turn-1")
	if strings.Contains(first.ID, "codex-A") || strings.Contains(first.ID, "turn-1") || first.ID != opaqueTaskID(ProviderCodex, "codex-A", "turn-1") {
		t.Fatalf("non-opaque or unstable id %q", first.ID)
	}
	ids := map[string]bool{}
	for _, task := range st.Snapshot().Tasks {
		if ids[task.ID] {
			t.Fatalf("duplicate task id %q", task.ID)
		}
		ids[task.ID] = true
	}

	secondTurn := m4Event(ProviderCodex, "codex-A", "turn-2", EventUserPromptSubmit, now.Add(time.Minute))
	secondTurn.Cwd = ptrString(t.TempDir())
	submitOK(t, r, secondTurn)
	if m4Task(t, st, ProviderCodex, "codex-A", "turn-2").ID == first.ID {
		t.Fatal("same session multiple turns collapsed into one task")
	}
}

func TestM4SyntheticClaudeDegradesAndAdvancesAcceptedSourceSuccess(t *testing.T) {
	st, r, now := m4Fixture(t)
	e := m4Event(ProviderClaude, "s", "synthetic:event-1", EventUserPromptSubmit, now.Add(time.Minute))
	e.Metadata.SyntheticTurnIdentity = true
	e.Metadata.TaskTitle = ptrString("Audit task reducer")
	submitOK(t, r, e)
	task := m4Task(t, st, ProviderClaude, "s", "synthetic:event-1")
	if task.Confidence != state.TaskConfidenceDegraded {
		t.Fatalf("confidence=%s", task.Confidence)
	}
	src := st.Snapshot().Sources["claude-hooks"]
	if src.Status != state.SourceDegraded || src.LastSuccessAt == nil || !src.LastSuccessAt.Equal(e.OccurredAt) {
		t.Fatalf("source=%+v", src)
	}
}

func TestM4MissingLaterIdentityIsNotGuessed(t *testing.T) {
	st, r, now := m4Fixture(t)
	begin := m4Event(ProviderClaude, "s", "prompt-1", EventUserPromptSubmit, now)
	submitOK(t, r, begin)
	before := m4Task(t, st, ProviderClaude, "s", "prompt-1")
	missing := m4Event(ProviderClaude, "s", "", EventPostToolUse, now.Add(time.Minute))
	missing.EventID = "missing-turn"
	missing.Metadata.ToolName = ptrString("Edit")
	submitOK(t, r, missing)
	after := m4Task(t, st, ProviderClaude, "s", "prompt-1")
	if after.Checkpoint.Kind != before.Checkpoint.Kind || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unattributed event guessed active task: before=%+v after=%+v", before, after)
	}
	if got := st.Snapshot().Sources["claude-hooks"]; got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(now) {
		t.Fatalf("unattributed event incorrectly advanced success: %+v", got)
	}
}

func TestM4ToolMappingUsesNameOnlyAndExactPriority(t *testing.T) {
	if got := toolCheckpoint("Bash"); got != state.CheckpointRunning {
		t.Fatalf("Bash => %s", got)
	}
	if got := toolCheckpoint("go_test"); got != state.CheckpointValidating {
		t.Fatalf("go_test => %s", got)
	}
	for name, want := range map[string]state.TaskCheckpointKind{"Read": state.CheckpointInspecting, "Grep": state.CheckpointInspecting, "apply_patch": state.CheckpointEditing, "Write": state.CheckpointEditing, "Build": state.CheckpointValidating, "mystery": state.CheckpointRunning} {
		if got := toolCheckpoint(name); got != want {
			t.Fatalf("%s => %s want %s", name, got, want)
		}
	}
	for kind, want := range map[state.TaskCheckpointKind]int{state.CheckpointBackgroundWait: 60, state.CheckpointSubtaskCompleted: 50, state.CheckpointDelegated: 45, state.CheckpointValidating: 40, state.CheckpointEditing: 30, state.CheckpointInspecting: 30, state.CheckpointRunning: 10, state.CheckpointStarted: 0} {
		if got := taskCheckpointPriority(kind); got != want {
			t.Fatalf("priority %s=%d want %d", kind, got, want)
		}
	}
}

func TestM4BashGoTestCommandRemainsRunning(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	raw := `{"session_id":"s","turn_id":"t","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`
	e, ok, err := Normalize(ProviderCodex, []byte(raw), at, "bash-test")
	if err != nil || !ok {
		t.Fatalf("normalize ok=%v err=%v", ok, err)
	}
	kind, _, _ := checkpointForEvent(e)
	if kind != state.CheckpointRunning {
		t.Fatalf("Bash command text affected mapping: %s", kind)
	}
}

func TestM4CheckpointReplacementAndBackgroundResume(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventUserPromptSubmit, now))
	edit := m4Event(ProviderCodex, "s", "t", EventPreToolUse, now.Add(time.Second))
	edit.Metadata.ToolName = ptrString("Edit")
	submitOK(t, r, edit)
	run := m4Event(ProviderCodex, "s", "t", EventPreToolUse, now.Add(10*time.Second))
	run.EventID = "run-too-soon"
	run.Metadata.ToolName = ptrString("Bash")
	submitOK(t, r, run)
	if got := m4Task(t, st, ProviderCodex, "s", "t").Checkpoint.Kind; got != state.CheckpointEditing {
		t.Fatalf("lower priority replaced too soon: %s", got)
	}
	runLate := run
	runLate.EventID = "run-late"
	runLate.OccurredAt = now.Add(32 * time.Second)
	submitOK(t, r, runLate)
	if got := m4Task(t, st, ProviderCodex, "s", "t").Checkpoint.Kind; got != state.CheckpointRunning {
		t.Fatalf("lower priority did not age in: %s", got)
	}

	// Claude background Stop is the highest checkpoint and a later authoritative
	// tool event is allowed to resume immediately despite lower priority.
	st2, r2, now2 := m4Fixture(t)
	submitOK(t, r2, m4Event(ProviderClaude, "c", "p", EventUserPromptSubmit, now2))
	bg := m4Event(ProviderClaude, "c", "p", EventStop, now2.Add(time.Second))
	bg.Metadata.BackgroundTaskCount = ptrInt(1)
	bg.Metadata.SessionCronCount = ptrInt(0)
	submitOK(t, r2, bg)
	if got := m4Task(t, st2, ProviderClaude, "c", "p").Checkpoint.Kind; got != state.CheckpointBackgroundWait {
		t.Fatalf("background checkpoint=%s", got)
	}
	resume := m4Event(ProviderClaude, "c", "p", EventPreToolUse, now2.Add(2*time.Second))
	resume.Metadata.ToolName = ptrString("Read")
	submitOK(t, r2, resume)
	if got := m4Task(t, st2, ProviderClaude, "c", "p").Checkpoint.Kind; got != state.CheckpointInspecting {
		t.Fatalf("background resume checkpoint=%s", got)
	}
}

func TestM4AttentionResolvesOnSameTurnProgressAndStaleClear(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderCodex, "A", "t", EventUserPromptSubmit, now))
	submitOK(t, r, m4Event(ProviderCodex, "B", "u", EventUserPromptSubmit, now))
	perm := m4Event(ProviderCodex, "A", "t", EventPermissionRequest, now.Add(time.Second))
	perm.Metadata.CorrelationID = ptrString("tool-1")
	submitOK(t, r, perm)
	task := m4Task(t, st, ProviderCodex, "A", "t")
	if task.Attention == nil || task.Attention.Kind != state.AttentionApprovalNeeded || task.Lifecycle != state.TaskLifecycleAttention {
		t.Fatalf("attention=%+v task=%+v", task.Attention, task)
	}

	unrelatedSameTask := m4Event(ProviderCodex, "A", "t", EventPostToolUse, now.Add(2*time.Second))
	unrelatedSameTask.Metadata.CorrelationID = ptrString("tool-2")
	submitOK(t, r, unrelatedSameTask)
	task = m4Task(t, st, ProviderCodex, "A", "t")
	if task.Attention != nil || task.Lifecycle != state.TaskWorking || m4Agent(t, st, ProviderCodex, "A").CurrentTurn.Activity != state.ActivityWorking {
		t.Fatalf("same-turn progress left a false attention state: task=%+v agent=%+v", task, m4Agent(t, st, ProviderCodex, "A"))
	}
	if m4Task(t, st, ProviderCodex, "B", "u").Attention != nil {
		t.Fatal("attention crossed tasks")
	}

	perm2 := perm
	perm2.EventID = "perm2"
	perm2.OccurredAt = now.Add(4 * time.Second)
	submitOK(t, r, perm2)
	if err := r.Maintenance(now.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := m4Task(t, st, ProviderCodex, "A", "t")
	if got.Attention != nil || got.Freshness != state.FreshnessStale || got.Lifecycle != state.TaskWorking {
		t.Fatalf("stale did not clear attention: %+v", got)
	}
}

func TestM4CodexNonInteractivePermissionRequestStaysWorking(t *testing.T) {
	for _, mode := range []string{"dontAsk", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			st, r, now := m4Fixture(t)
			submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventUserPromptSubmit, now))
			permission := m4Event(ProviderCodex, "s", "t", EventPermissionRequest, now.Add(time.Second))
			permission.Metadata.PermissionMode = ptrString(mode)
			submitOK(t, r, permission)
			task, agent := m4Task(t, st, ProviderCodex, "s", "t"), m4Agent(t, st, ProviderCodex, "s")
			if task.Attention != nil || task.Lifecycle != state.TaskWorking || agent.CurrentTurn.Activity != state.ActivityWorking {
				t.Fatalf("non-interactive permission request became attention: task=%+v agent=%+v", task, agent.CurrentTurn)
			}
		})
	}
}

func TestM4ClaudeAttentionKindsAndElicitationResolution(t *testing.T) {
	for _, tc := range []struct {
		typ      EventType
		errorTyp string
		want     state.TaskAttentionKind
	}{
		{EventPermissionRequest, "", state.AttentionApprovalNeeded},
		{EventAskUserQuestion, "", state.AttentionQuestionWaiting},
		{EventElicitation, "", state.AttentionElicitationWaiting},
		{EventStopFailure, "authentication_failed", state.AttentionAuthenticationRequired},
		{EventStopFailure, "billing_error", state.AttentionBillingRequired},
		{EventStopFailure, "rate_limit", state.AttentionRateLimited},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			st, r, now := m4Fixture(t)
			submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
			e := m4Event(ProviderClaude, "s", "p", tc.typ, now.Add(time.Second))
			if tc.errorTyp != "" {
				e.Metadata.ErrorType = ptrString(tc.errorTyp)
			}
			submitOK(t, r, e)
			got := m4Task(t, st, ProviderClaude, "s", "p")
			if got.Attention == nil || got.Attention.Kind != tc.want || len(got.Attention.Text) > maxAttentionTextBytes {
				t.Fatalf("attention=%+v", got.Attention)
			}
			if tc.typ == EventStopFailure && got.Lifecycle != state.TaskError {
				t.Fatalf("StopFailure not terminal error: %+v", got)
			}
		})
	}

	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventElicitation, now.Add(time.Second)))
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventElicitationResult, now.Add(2*time.Second)))
	if got := m4Task(t, st, ProviderClaude, "s", "p"); got.Attention != nil || got.Lifecycle != state.TaskWorking {
		t.Fatalf("elicitation result did not resolve: %+v", got)
	}
}

func TestM4RecoverableFailuresAreNotTaskErrors(t *testing.T) {
	for _, typ := range []EventType{EventPostToolUseFailure, EventPermissionDenied} {
		t.Run(string(typ), func(t *testing.T) {
			st, r, now := m4Fixture(t)
			submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
			e := m4Event(ProviderClaude, "s", "p", typ, now.Add(time.Second))
			submitOK(t, r, e)
			got := m4Task(t, st, ProviderClaude, "s", "p")
			if got.Lifecycle == state.TaskError || m4Agent(t, st, ProviderClaude, "s").CurrentTurn.Outcome == state.OutcomeFailed {
				t.Fatalf("recoverable event became error: %+v", got)
			}
		})
	}
}

func TestM4ChildWorkIsParentOnly(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	created := m4Event(ProviderClaude, "s", "p", EventTaskCreated, now.Add(time.Second))
	created.Metadata.ChildSubject = ptrString("Inspect auth flow")
	submitOK(t, r, created)
	if len(st.Snapshot().Tasks) != 1 {
		t.Fatalf("child task created top-level card: %d", len(st.Snapshot().Tasks))
	}
	if got := m4Task(t, st, ProviderClaude, "s", "p").Checkpoint; got == nil || got.Kind != state.CheckpointDelegated || !strings.Contains(got.Text, "Inspect auth flow") {
		t.Fatalf("delegated checkpoint=%+v", got)
	}
	completed := m4Event(ProviderClaude, "s", "p", EventTaskCompleted, now.Add(2*time.Second))
	completed.Metadata.ChildSubject = ptrString("Inspect auth flow")
	submitOK(t, r, completed)
	got := m4Task(t, st, ProviderClaude, "s", "p")
	if got.Checkpoint.Kind != state.CheckpointSubtaskCompleted || got.Lifecycle != state.TaskWorking {
		t.Fatalf("child completion affected root lifecycle: %+v", got)
	}
}

func TestM4ClaudeStopMissingBackgroundFieldsCompletesWithoutReadyState(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	stop := m4Event(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	submitOK(t, r, stop)
	task := m4Task(t, st, ProviderClaude, "s", "p")
	if task.Lifecycle != state.TaskComplete || task.Freshness != state.FreshnessFresh {
		t.Fatalf("missing capability did not complete cleanly: %+v", task)
	}
	src := st.Snapshot().Sources["claude-hooks"]
	if src.Status != state.SourceDegraded || src.LastSuccessAt == nil || !src.LastSuccessAt.Equal(stop.OccurredAt) {
		t.Fatalf("accepted degraded event did not advance success: %+v", src)
	}
}

func TestM4TerminalStopDoesNotReviveTaskOnLateSameTurnEvent(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderCodex, "s", "p", EventUserPromptSubmit, now))
	submitOK(t, r, m4Event(ProviderCodex, "s", "p", EventStop, now.Add(time.Minute)))
	late := m4Event(ProviderCodex, "s", "p", EventPreToolUse, now.Add(2*time.Minute))
	late.Metadata.ToolName = ptrString("Edit")
	submitOK(t, r, late)
	task := m4Task(t, st, ProviderCodex, "s", "p")
	if task.Lifecycle != state.TaskComplete {
		t.Fatalf("late same-turn event revived task: %+v", task)
	}
	if got := m4Agent(t, st, ProviderCodex, "s").CurrentTurn.Outcome; got != state.OutcomeCompleted {
		t.Fatalf("late same-turn event revived agent outcome: %s", got)
	}
}

func TestUnreadCompletedTaskSurvivesUntilSameSessionPromptAcknowledges(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "h"}))
	r := NewReducer(st, ReducerConfig{CompleteRetention: 30 * time.Minute})
	submitOK(t, r, m4Event(ProviderCodex, "session", "turn-1", EventUserPromptSubmit, now))
	submitOK(t, r, m4Event(ProviderCodex, "session", "turn-1", EventStop, now.Add(time.Minute)))

	if err := r.Maintenance(now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	snap := st.Snapshot()
	if len(snap.Tasks) != 1 || snap.Tasks[0].ReadAt != nil {
		t.Fatalf("unread completed task was removed or acknowledged: %+v", snap.Tasks)
	}
	pub := state.ProjectPublic(snap, state.RuntimeCapabilities{}, state.ProjectionConfig{}, now.Add(2*time.Hour))
	if len(pub.Tasks) != 1 || !pub.Tasks[0].Unread {
		t.Fatalf("public unread projection=%+v", pub.Tasks)
	}

	submitOK(t, r, m4Event(ProviderCodex, "session", "turn-2", EventUserPromptSubmit, now.Add(3*time.Hour)))
	snap = st.Snapshot()
	if len(snap.Tasks) != 2 || snap.Tasks[0].ReadAt == nil {
		t.Fatalf("same-session prompt did not acknowledge prior task: %+v", snap.Tasks)
	}
	if err := r.Maintenance(now.Add(3*time.Hour + 31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := len(st.Snapshot().Tasks); got != 1 {
		t.Fatalf("acknowledged task retention count=%d want 1", got)
	}
}

func TestStaleWorkingTaskIsRetainedThenPruned(t *testing.T) {
	now := time.Date(2026, 8, 25, 15, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "h"}))
	r := NewReducer(st, ReducerConfig{StaleAfter: 10 * time.Minute, StaleTaskRetention: time.Hour})
	submitOK(t, r, m4Event(ProviderCodex, "session", "turn", EventUserPromptSubmit, now))

	if err := r.Maintenance(now.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	snap := st.Snapshot()
	if len(snap.Tasks) != 1 || snap.Tasks[0].Freshness != state.FreshnessStale {
		t.Fatalf("stale task was not retained for diagnosis: %+v", snap.Tasks)
	}
	if len(snap.Alerts) != 1 || !snap.Alerts[0].Active || snap.Alerts[0].RetainUntil == nil {
		t.Fatalf("stale alert was not bounded: %+v", snap.Alerts)
	}

	if err := r.Maintenance(now.Add(1*time.Hour + 12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	snap = st.Snapshot()
	if len(snap.Tasks) != 0 || len(snap.Alerts) != 0 {
		t.Fatalf("expired stale state still occupies the snapshot: tasks=%+v alerts=%+v", snap.Tasks, snap.Alerts)
	}
}

func TestHistoricalTerminalPromptReplayDoesNotAcknowledgeOrCreateTask(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "h"}))
	r := NewReducer(st, ReducerConfig{CompleteRetention: 30 * time.Minute})
	submitOK(t, r, m4Event(ProviderCodex, "session", "turn-1", EventUserPromptSubmit, now))
	submitOK(t, r, m4Event(ProviderCodex, "session", "turn-1", EventStop, now.Add(time.Minute)))

	// Simulate restoring the same historical conversation after the reducer
	// has been recreated, so in-memory session metadata is unavailable.
	r = NewReducer(st, ReducerConfig{CompleteRetention: 30 * time.Minute})
	replay := m4Event(ProviderCodex, "session", "turn-1", EventUserPromptSubmit, now.Add(2*time.Minute))
	submitOK(t, r, replay)

	snap := st.Snapshot()
	if len(snap.Tasks) != 1 {
		t.Fatalf("historical replay created a task: %+v", snap.Tasks)
	}
	if snap.Tasks[0].ReadAt != nil || snap.Tasks[0].Lifecycle != state.TaskComplete {
		t.Fatalf("historical replay changed terminal task: %+v", snap.Tasks[0])
	}
	if got := m4Agent(t, st, ProviderCodex, "session").CurrentTurn; got.Outcome != state.OutcomeCompleted || got.TurnID != "turn-1" {
		t.Fatalf("historical replay changed agent turn: %+v", got)
	}
}

func TestM4AgentAuthorityAndTaskMirrorSameLifecycle(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventUserPromptSubmit, now))
	if a, task := m4Agent(t, st, ProviderCodex, "s"), m4Task(t, st, ProviderCodex, "s", "t"); a.CurrentTurn.Activity != state.ActivityWorking || task.Lifecycle != state.TaskWorking {
		t.Fatalf("working mismatch agent=%+v task=%+v", a, task)
	}
	submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventPermissionRequest, now.Add(time.Second)))
	if a, task := m4Agent(t, st, ProviderCodex, "s"), m4Task(t, st, ProviderCodex, "s", "t"); a.CurrentTurn.Activity != state.ActivityAttention || task.Lifecycle != state.TaskLifecycleAttention {
		t.Fatalf("attention mismatch agent=%+v task=%+v", a, task)
	}
	submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventStop, now.Add(2*time.Second)))
	if a, task := m4Agent(t, st, ProviderCodex, "s"), m4Task(t, st, ProviderCodex, "s", "t"); a.CurrentTurn.Outcome != state.OutcomeCompleted || task.Lifecycle != state.TaskComplete {
		t.Fatalf("complete mismatch agent=%+v task=%+v", a, task)
	}
}
