package uplink

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

var m54Base = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

func m54Store(t *testing.T) *state.Store {
	t.Helper()
	return state.NewStore(state.LiveInitialState(m54Base, state.HostState{ID: "mac-a", DisplayName: "Mac A"}))
}

func TestM54BuildProducesPublicOnlyEnvelope(t *testing.T) {
	store := m54Store(t)
	changed := store.Snapshot()
	changed.Tasks = append(changed.Tasks, state.TaskState{ID: "task-1", Provider: "codex", Title: "Ship M5.4", StartedAt: m54Base, UpdatedAt: m54Base})
	store.Replace(changed)

	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base.Add(5 * time.Second) })
	snap, err := builder.Build("aabbccddeeff00112233445566778899", 7)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if snap.SchemaVersion != 1 || snap.StateKind != "nodeSnapshot" {
		t.Fatalf("unexpected envelope header: %+v", snap)
	}
	if snap.NodeID != "mac-a" || snap.SessionID != "aabbccddeeff00112233445566778899" || snap.Sequence != 7 {
		t.Fatalf("unexpected envelope identity: %+v", snap)
	}
	// M5.2 §5.5: sentAt == nested generatedAt, same UTC instant.
	if !snap.SentAt.Equal(snap.State.GeneratedAt) {
		t.Fatalf("sentAt %v must equal generatedAt %v", snap.SentAt, snap.State.GeneratedAt)
	}
	// The nested payload must be exactly the PublicState projection.
	want := state.ProjectPublic(store.Snapshot(), state.RuntimeCapabilities{}, state.ProjectionConfig{}, m54Base.Add(5*time.Second))
	if !equalPublicJSON(t, snap.State, want) {
		t.Fatalf("nested state is not the PublicState projection")
	}
	// Public projection contract: schema/kind/host binding.
	if snap.State.SchemaVersion != 1 || snap.State.StateKind != "public" || snap.State.Host.ID != "mac-a" {
		t.Fatalf("unexpected nested public state: %+v", snap.State)
	}
	if len(snap.State.Tasks) != 1 || snap.State.Tasks[0].ID != "task-1" {
		t.Fatalf("expected projected task, got %+v", snap.State.Tasks)
	}
}

// TestM54EnvelopeCarriesNoPrivateFields freezes the snapshot boundary: the
// serialized envelope may contain only the frozen envelope keys and the
// sanitized public payload — no internal root state, no provider raw events,
// no file paths, no transcripts.
func TestM54EnvelopeCarriesNoPrivateFields(t *testing.T) {
	store := m54Store(t)
	changed := store.Snapshot()
	changed.Sources["codex"] = state.SourceHealth{Status: state.SourceUnavailable, Message: "socket: /Users/someone/private/path.sock", LastAttemptAt: &m54Base}
	store.Replace(changed)

	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base })
	snap, err := builder.Build("aabbccddeeff00112233445566778899", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	envelopeKeys := make([]string, 0, len(raw))
	for k := range raw {
		envelopeKeys = append(envelopeKeys, k)
	}
	wantKeys := []string{"schemaVersion", "stateKind", "nodeId", "sessionId", "sequence", "sentAt", "state"}
	if len(raw) != len(wantKeys) {
		t.Fatalf("envelope keys %v must be exactly %v", envelopeKeys, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Fatalf("envelope is missing frozen key %q (got %v)", k, envelopeKeys)
		}
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw["state"], &inner); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	// The nested payload carries only the frozen PublicState key set.
	wantStateKeys := []string{"schemaVersion", "stateKind", "generatedAt", "host", "agents", "tasks", "alerts", "system", "network", "projects", "quota", "sources", "navigationTargets", "meta"}
	if len(inner) != len(wantStateKeys) {
		t.Fatalf("nested state has %d keys, want exactly the frozen public set %v", len(inner), wantStateKeys)
	}
	for _, k := range wantStateKeys {
		if _, ok := inner[k]; !ok {
			t.Fatalf("nested state missing public key %q", k)
		}
	}
	// Internal-only spellings must never appear; PublicState key set is the
	// frozen public projection.
	for _, forbidden := range []string{"rawEvent", "prompt", "transcript", "cwd", "internalState", "stateKind\\\":\\\"internal"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("envelope leaked private content marker %q", forbidden)
		}
	}
	// The private socket path planted in the internal SourceHealth.Message
	// must not survive the projection into the serialized envelope.
	if strings.Contains(string(body), "/Users/someone/private/path.sock") {
		t.Fatalf("envelope leaked the private source socket path")
	}
	if msg := snap.State.Sources["codex"].Message; msg != "Source unavailable." {
		t.Fatalf("envelope source message must be the bounded public text, got %q", msg)
	}
}

// TestM54EnvelopeDoesNotExposePrivateSourceMessage freezes the M0/M5.2 §34
// source-message privacy boundary on the wire: an internal SourceHealth.Message
// carrying a private socket path and a credential sentinel must be reduced to
// the status-derived public text inside the NodeSnapshot JSON.
func TestM54EnvelopeDoesNotExposePrivateSourceMessage(t *testing.T) {
	store := m54Store(t)
	changed := store.Snapshot()
	changed.Sources["codex"] = state.SourceHealth{
		Status:        state.SourceDegraded,
		Message:       "dial /Users/someone/private/path.sock rejected token SUPER_SECRET_SOURCE_TOKEN",
		LastAttemptAt: &m54Base,
	}
	store.Replace(changed)

	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base })
	snap, err := builder.Build("aabbccddeeff00112233445566778899", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"/Users/someone/private/path.sock", "SUPER_SECRET_SOURCE_TOKEN"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("serialized NodeSnapshot leaked private source material %q", forbidden)
		}
	}
	source, ok := snap.State.Sources["codex"]
	if !ok {
		t.Fatal("source missing from envelope state")
	}
	if source.Message != "Source degraded." {
		t.Fatalf("envelope source message must be the bounded allow-listed text, got %q", source.Message)
	}
	if source.Status != state.SourceDegraded {
		t.Fatalf("source status must be preserved, got %q", source.Status)
	}
}

func TestM54BuildRejectsHostIdentityMismatch(t *testing.T) {
	store := m54Store(t)
	builder := NewSnapshotBuilder(store, "mac-b", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base })
	if _, err := builder.Build("aabbccddeeff00112233445566778899", 1); err == nil {
		t.Fatalf("expected identity mismatch refusal")
	}
}

func TestM54DigestIgnoresProjectionTime(t *testing.T) {
	store := m54Store(t)
	at := func(offset time.Duration) state.PublicState {
		return NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base.Add(offset) }).Public()
	}
	if Digest(at(0)) != Digest(at(90*time.Minute)) {
		t.Fatalf("unchanged state must digest identically across projection instants")
	}
}

func TestM54DigestDetectsPublicChange(t *testing.T) {
	store := m54Store(t)
	builder := NewSnapshotBuilder(store, "mac-a", state.RuntimeCapabilities{}, state.ProjectionConfig{}, func() time.Time { return m54Base })
	before := Digest(builder.Public())

	changed := store.Snapshot()
	changed.Tasks = append(changed.Tasks, state.TaskState{ID: "task-2", Provider: "claude-code", Title: "New checkpoint", StartedAt: m54Base, UpdatedAt: m54Base})
	store.Replace(changed)

	if Digest(builder.Public()) == before {
		t.Fatalf("changed PublicState must change the digest")
	}
}

func TestM54NewSessionIDFormat(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 64; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("session id: %v", err)
		}
		if len(id) != 32 || strings.ToLower(id) != id {
			t.Fatalf("session id %q must be 32 lowercase hex characters", id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("session id %q is not hex: %v", id, err)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 64 {
		t.Fatalf("session ids must be unique per process session, saw %d unique in 64 draws", len(seen))
	}
}

func equalPublicJSON(t *testing.T, a, b state.PublicState) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	return string(ja) == string(jb)
}
