package agent

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestClaudeStopFailureUnknownErrorIsNormalizedToUnknown(t *testing.T) {
	raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"StopFailure","error":"PRIVATE_ERROR_SENTINEL"}`
	e, ok := norm(t, ProviderClaude, raw)
	if !ok {
		t.Fatal("StopFailure unexpectedly ignored")
	}
	if e.Metadata.ErrorType == nil || *e.Metadata.ErrorType != "unknown" {
		t.Fatalf("unsafe error was not normalized to unknown: %+v", e.Metadata)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "PRIVATE_ERROR_SENTINEL") {
		t.Fatalf("unsafe error leaked into normalized event: %s", b)
	}
}

func TestClaudeSafeErrorTypeAllowList(t *testing.T) {
	allowed := []string{"rate_limit", "overloaded", "authentication_failed", "oauth_org_not_allowed", "billing_error", "invalid_request", "model_not_found", "server_error", "max_output_tokens", "unknown"}
	for _, want := range allowed {
		t.Run(want, func(t *testing.T) {
			raw := `{"session_id":"s","prompt_id":"p","hook_event_name":"StopFailure","error":"` + want + `"}`
			e, ok := norm(t, ProviderClaude, raw)
			if !ok || e.Metadata.ErrorType == nil || *e.Metadata.ErrorType != want {
				t.Fatalf("error type %q normalized as %+v ok=%v", want, e.Metadata, ok)
			}
		})
	}
}

func TestClaudeUnknownNotificationTypeIsIgnored(t *testing.T) {
	e, ok := norm(t, ProviderClaude, `{"session_id":"s","prompt_id":"p","hook_event_name":"Notification","notification_type":"PRIVATE_NOTIFICATION_TYPE_SENTINEL"}`)
	if ok {
		t.Fatalf("unsafe notification type was normalized: %+v", e)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "PRIVATE_NOTIFICATION_TYPE_SENTINEL") {
		t.Fatalf("unsafe notification type leaked: %s", b)
	}
}

func TestNormalizedIPCValidationRejectsUnsafeMetadata(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	badNotification := AgentEvent{SchemaVersion: 1, EventID: "bad-notification", Provider: ProviderClaude, SessionID: "s", TurnID: ptrString("p"), EventType: EventNotification, OccurredAt: at, Metadata: Metadata{NotificationType: ptrString("PRIVATE_NOTIFICATION_TYPE_SENTINEL")}}
	if err := badNotification.Validate(); err == nil {
		t.Fatal("unsafe notification metadata accepted for normalized IPC")
	}
	badError := AgentEvent{SchemaVersion: 1, EventID: "bad-error", Provider: ProviderClaude, SessionID: "s", TurnID: ptrString("p"), EventType: EventStopFailure, OccurredAt: at, Metadata: Metadata{ErrorType: ptrString("PRIVATE_ERROR_SENTINEL")}}
	if err := badError.Validate(); err == nil {
		t.Fatal("unsafe error metadata accepted for normalized IPC")
	}
}

func activeAlert(st *state.Store, alertType state.AlertType) bool {
	for _, alert := range st.Snapshot().Alerts {
		if alert.Type == alertType && alert.Active {
			return true
		}
	}
	return false
}

func TestSessionEndBarrierPreservesCompletedTurnAgainstLatePostToolUse(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventStop, now.Add(time.Minute)))
	_ = r.Submit(ev(ProviderCodex, "s", "", EventSessionEnd, now.Add(2*time.Minute)))
	before := st.Snapshot()

	late := ev(ProviderCodex, "s", "A", EventPostToolUse, now.Add(3*time.Minute))
	late.EventID = "late-post-after-session-end"
	_ = r.Submit(late)

	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeCompleted || a.CurrentTurn.CompletedAt == nil {
		t.Fatalf("late event revived completed turn: %+v", a.CurrentTurn)
	}
	if !activeAlert(st, state.AlertComplete) {
		t.Fatal("late event resolved completion alert")
	}
	after := st.Snapshot()
	if !after.GeneratedAt.Equal(before.GeneratedAt) {
		t.Fatalf("ignored late event mutated generatedAt: before=%s after=%s", before.GeneratedAt, after.GeneratedAt)
	}
}

func TestSessionEndBarrierPreventsAttentionReactivation(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(time.Minute)))
	_ = r.Submit(ev(ProviderCodex, "s", "", EventSessionEnd, now.Add(2*time.Minute)))
	if activeAlert(st, state.AlertAttention) {
		t.Fatal("SessionEnd did not resolve attention")
	}

	late := ev(ProviderCodex, "s", "A", EventPermissionRequest, now.Add(3*time.Minute))
	late.EventID = "late-permission-after-session-end"
	_ = r.Submit(late)
	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityIdle {
		t.Fatalf("late permission reactivated attention: %+v", a.CurrentTurn)
	}
	if activeAlert(st, state.AlertAttention) {
		t.Fatal("late permission recreated attention alert")
	}
}

func TestSessionEndBarrierPreservesFailureAgainstLatePostToolUse(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderClaude, "s", "A", EventStopFailure, now.Add(time.Minute)))
	_ = r.Submit(ev(ProviderClaude, "s", "", EventSessionEnd, now.Add(2*time.Minute)))

	late := ev(ProviderClaude, "s", "A", EventPostToolUse, now.Add(3*time.Minute))
	late.EventID = "late-post-after-failure-session-end"
	_ = r.Submit(late)
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Outcome != state.OutcomeFailed {
		t.Fatalf("late event cleared failed terminal state: %+v", a.CurrentTurn)
	}
	if !activeAlert(st, state.AlertError) {
		t.Fatal("late event resolved terminal error alert")
	}
}

func TestNewTurnAfterSessionEndReopensSession(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "", EventSessionEnd, now.Add(time.Minute)))
	beginB := ev(ProviderCodex, "s", "B", EventUserPromptSubmit, now.Add(2*time.Minute))
	beginB.EventID = "begin-turn-b-after-session-end"
	_ = r.Submit(beginB)

	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.TurnID != "B" || a.CurrentTurn.Activity != state.ActivityWorking || a.CurrentTurn.Outcome != state.OutcomeNone {
		t.Fatalf("new turn did not reopen session: %+v", a.CurrentTurn)
	}
	if !r.sessions[sessionKey(ProviderCodex, "s")].SessionEndedAt.IsZero() {
		t.Fatal("new accepted turn did not clear SessionEndedAt")
	}
}

func TestDuplicateEndedTurnBeginCannotReopenSession(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now))
	_ = r.Submit(ev(ProviderCodex, "s", "", EventSessionEnd, now.Add(time.Minute)))
	dup := ev(ProviderCodex, "s", "A", EventUserPromptSubmit, now.Add(2*time.Minute))
	dup.EventID = "duplicate-ended-turn-begin"
	_ = r.Submit(dup)

	a := agentByID(t, st, "codex:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.TurnID != "A" {
		t.Fatalf("duplicate ended begin reopened session: %+v", a.CurrentTurn)
	}
	if r.sessions[sessionKey(ProviderCodex, "s")].SessionEndedAt.IsZero() {
		t.Fatal("duplicate ended begin cleared SessionEndedAt")
	}
}

func TestTurnAttributionCapabilityDegradationSurvivesSessionEnd(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	missing := ev(ProviderClaude, "s", "", EventPostToolUse, now.Add(time.Minute))
	missing.EventID = "missing-turn-capability"
	_ = r.Submit(missing)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceDegraded {
		t.Fatalf("missing turn status=%s", got)
	}

	_ = r.Submit(ev(ProviderClaude, "s", "", EventSessionEnd, now.Add(2*time.Minute)))
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceDegraded {
		t.Fatalf("SessionEnd erased turn capability degradation: %s", got)
	}
	a := agentByID(t, st, "claude-code:s")
	if a.CurrentTurn.Activity != state.ActivityIdle || a.CurrentTurn.Freshness != state.FreshnessFresh {
		t.Fatalf("agent terminal freshness coupled to source degradation: %+v", a.CurrentTurn)
	}
}

func TestTurnAttributionCapabilityRecoversOnReliableTurnIdentity(t *testing.T) {
	st, r, now := testReducer(t)
	begin := ev(ProviderClaude, "s", "", EventUserPromptSubmit, now)
	begin.EventID = "synthetic-begin"
	begin.TurnID = ptrString("synthetic:synthetic-begin")
	begin.Metadata.SyntheticTurnIdentity = true
	_ = r.Submit(begin)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceDegraded {
		t.Fatalf("synthetic turn did not degrade attribution capability: %s", got)
	}

	reliable := ev(ProviderClaude, "s", "prompt-real", EventPostToolUse, now.Add(time.Minute))
	reliable.EventID = "reliable-turn-evidence"
	_ = r.Submit(reliable)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceAvailable {
		t.Fatalf("reliable turn identity did not recover capability: %s", got)
	}
}

func TestClaudeStopCapabilityDegradationLatchesUntilValidStop(t *testing.T) {
	st, r, now := testReducer(t)
	_ = r.Submit(ev(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	missing := ev(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	missing.EventID = "missing-stop-capability"
	_ = r.Submit(missing)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceDegraded {
		t.Fatalf("missing Stop capability status=%s", got)
	}

	post := ev(ProviderClaude, "s", "p", EventPostToolUse, now.Add(2*time.Minute))
	post.EventID = "unrelated-post-tool-use"
	_ = r.Submit(post)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceDegraded {
		t.Fatalf("unrelated tool event erased Stop capability degradation: %s", got)
	}
	if got := agentByID(t, st, "claude-code:s").CurrentTurn.Freshness; got != state.FreshnessFresh {
		t.Fatalf("agent freshness did not recover independently: %s", got)
	}

	valid := ev(ProviderClaude, "s", "p", EventStop, now.Add(3*time.Minute))
	valid.EventID = "valid-stop-capability"
	valid.Metadata.BackgroundTaskCount = ptrInt(0)
	valid.Metadata.SessionCronCount = ptrInt(0)
	_ = r.Submit(valid)
	if got := st.Snapshot().Sources["claude-hooks"].Status; got != state.SourceAvailable {
		t.Fatalf("valid Stop did not recover capability: %s", got)
	}
	if got := agentByID(t, st, "claude-code:s").CurrentTurn.Outcome; got != state.OutcomeCompleted {
		t.Fatalf("valid Stop did not retain normal completion semantics: %s", got)
	}
}

func createStaleSocketPath(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket setup failed: info=%v err=%v", info, err)
	}
}

func TestPrepareSocketPathDefinitiveStaleProbeRemovesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, _ := runtimeFixture(t)
	createStaleSocketPath(t, p.Socket)
	if err := prepareSocketPathWithProbe(p.Socket, func(string) error { return syscall.ECONNREFUSED }); err != nil {
		t.Fatalf("definitive stale socket not recovered: %v", err)
	}
	if _, err := os.Lstat(p.Socket); !os.IsNotExist(err) {
		t.Fatalf("definitive stale socket still exists: %v", err)
	}
}

func TestPrepareSocketPathLiveProbeRefusesAndPreservesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, _ := runtimeFixture(t)
	createStaleSocketPath(t, p.Socket)
	if err := prepareSocketPathWithProbe(p.Socket, func(string) error { return nil }); err == nil {
		t.Fatal("live probe unexpectedly allowed takeover")
	}
	if _, err := os.Lstat(p.Socket); err != nil {
		t.Fatalf("live socket pathname was removed: %v", err)
	}
}

func TestPrepareSocketPathAmbiguousTimeoutPreservesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, _ := runtimeFixture(t)
	createStaleSocketPath(t, p.Socket)
	timeoutErr := &net.OpError{Op: "dial", Net: "unix", Err: os.ErrDeadlineExceeded}
	if err := prepareSocketPathWithProbe(p.Socket, func(string) error { return timeoutErr }); err == nil {
		t.Fatal("ambiguous timeout unexpectedly allowed takeover")
	}
	if _, err := os.Lstat(p.Socket); err != nil {
		t.Fatalf("ambiguous timeout unlinked socket: %v", err)
	}
}

func TestPrepareSocketPathUnknownProbeErrorPreservesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, _ := runtimeFixture(t)
	createStaleSocketPath(t, p.Socket)
	unknown := errors.New("PRIVATE_UNKNOWN_SOCKET_PROBE_ERROR")
	if err := prepareSocketPathWithProbe(p.Socket, func(string) error { return unknown }); err == nil {
		t.Fatal("unknown probe error unexpectedly allowed takeover")
	}
	if _, err := os.Lstat(p.Socket); err != nil {
		t.Fatalf("unknown probe error unlinked socket: %v", err)
	}
}

func TestPrepareSocketPathChangedPathIsNotUnlinked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	p, _, _ := runtimeFixture(t)
	createStaleSocketPath(t, p.Socket)
	probe := func(path string) error {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		createStaleSocketPath(t, path)
		return syscall.ECONNREFUSED
	}
	if err := prepareSocketPathWithProbe(p.Socket, probe); err == nil {
		t.Fatal("changed socket path unexpectedly unlinked")
	}
	if _, err := os.Lstat(p.Socket); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
}
