package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestM4CompletionBoundsSafetyAndNilAllowed(t *testing.T) {
	raw := "Implemented M4 task state successfully.\nTests pass: state, agent, and web.\nLimitation: real Mac validation remains pending.\nExtra line must not be selected.\nCommit abcdef1234567890"
	summary, id := deriveCompletion(raw)
	if summary == nil || id == nil || *id != "abcdef1234567890" {
		t.Fatalf("summary=%v id=%v", summary, id)
	}
	if len(*summary) > maxCompletionBytes || len(strings.Split(*summary, "\n")) > 3 {
		t.Fatalf("completion bounds violated: %q", *summary)
	}
	for _, line := range strings.Split(*summary, "\n") {
		if len(line) > maxCompletionLineBytes || !utf8.ValidString(line) {
			t.Fatalf("line bounds violated: %q", line)
		}
	}
	for _, unsafe := range []string{
		"Implemented change in /Users/name/private/repo/file.go",
		"go test ./... passed",
		"package main; tests pass",
		"api_key=SUPER_PRIVATE_VALUE tests pass",
		"-----BEGIN PRIVATE KEY-----\nabc\nTests pass",
	} {
		s, ident := deriveCompletion(unsafe)
		if s != nil || ident != nil {
			t.Fatalf("unsafe completion accepted %q => %v %v", unsafe, s, ident)
		}
	}
	if s, id := deriveCompletion("Thanks for the context. I reviewed everything carefully."); s != nil || id != nil {
		t.Fatalf("ordinary prose should not become completion: %v %v", s, id)
	}
}

func TestM4CompletionInputInspectionIsBounded(t *testing.T) {
	prefix := "Implemented safe result.\n" + strings.Repeat("x", maxCompletionInputBytes)
	secretAfterBound := "\napi_key=OUTSIDE_INSPECTION_BOUND"
	summary, _ := deriveCompletion(prefix + secretAfterBound)
	if summary == nil || !strings.Contains(*summary, "Implemented safe result") {
		t.Fatalf("bounded inspection lost safe prefix: %v", summary)
	}
}

func TestM4TopLevelStopRetainsOnlyDerivedCompletion(t *testing.T) {
	e, ok := normalizeRaw(t, ProviderCodex, `{"session_id":"s","turn_id":"t","hook_event_name":"Stop","last_assistant_message":"Implemented task board.\nTests pass.\nPRIVATE_ORDINARY_RESPONSE_BODY"}`)
	if !ok || e.Metadata.CompletionSummary == nil {
		t.Fatalf("event=%+v ok=%v", e, ok)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "PRIVATE_ORDINARY_RESPONSE_BODY") {
		t.Fatalf("raw final response leaked: %s", b)
	}
}

func TestM4CompletionAndBackgroundStopSemantics(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderClaude, "s", "p", EventUserPromptSubmit, now))
	bg := m4Event(ProviderClaude, "s", "p", EventStop, now.Add(time.Minute))
	bg.Metadata.BackgroundTaskCount = ptrInt(1)
	bg.Metadata.SessionCronCount = ptrInt(0)
	bg.Metadata.CompletionSummary = ptrString("Implemented something that must be discarded")
	submitOK(t, r, bg)
	got := m4Task(t, st, ProviderClaude, "s", "p")
	if got.Lifecycle != state.TaskWorking || got.Checkpoint.Kind != state.CheckpointBackgroundWait || got.Completion != nil {
		t.Fatalf("background Stop falsely completed: %+v", got)
	}

	stop := m4Event(ProviderClaude, "s", "p", EventStop, now.Add(2*time.Minute))
	stop.EventID = "terminal-stop"
	stop.Metadata.BackgroundTaskCount = ptrInt(0)
	stop.Metadata.SessionCronCount = ptrInt(0)
	stop.Metadata.CompletionSummary = ptrString("Implemented M4; tests pass.")
	stop.Metadata.ResultIdentifier = ptrString("abcdef1234567")
	submitOK(t, r, stop)
	got = m4Task(t, st, ProviderClaude, "s", "p")
	if got.Lifecycle != state.TaskComplete || got.Completion == nil || got.Completion.Summary == nil || *got.Completion.ResultIdentifier != "abcdef1234567" || got.Checkpoint.Kind != state.CheckpointBackgroundWait {
		t.Fatalf("terminal completion=%+v", got)
	}
	// Lifecycle COMPLETE outranks a retained checkpoint; the checkpoint itself is
	// intentionally not fabricated into a finalizing/validation result.
	if err := r.Maintenance(now.Add(33 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(st.Snapshot().Tasks) != 0 {
		t.Fatalf("completed task not pruned after retention: %+v", st.Snapshot().Tasks)
	}
}

func TestM4CompleteWithoutSafeSummaryIsValid(t *testing.T) {
	st, r, now := m4Fixture(t)
	submitOK(t, r, m4Event(ProviderCodex, "s", "t", EventUserPromptSubmit, now))
	stop := m4Event(ProviderCodex, "s", "t", EventStop, now.Add(time.Minute))
	submitOK(t, r, stop)
	got := m4Task(t, st, ProviderCodex, "s", "t")
	if got.Lifecycle != state.TaskComplete || got.Completion != nil {
		t.Fatalf("nil-summary completion invalid: %+v", got)
	}
}
