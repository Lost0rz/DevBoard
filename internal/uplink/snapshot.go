// Package uplink implements the M5.4 node-side push runtime: it projects the
// local state store to already-sanitized PublicState, wraps it in the frozen
// M5.2 NodeSnapshotV1 envelope and pushes it to the hub receiver. It never
// reads collectors directly, never mutates state and never learns another
// node's identity.
package uplink

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	envelopeSchemaVersion = 1
	envelopeStateKind     = "nodeSnapshot"
)

// NodeSnapshot is the frozen M5.2 NodeSnapshotV1 machine envelope. The nested
// payload is always an already-sanitized state.PublicState produced by the
// local projector; InternalState is never a valid node snapshot payload.
type NodeSnapshot struct {
	SchemaVersion int               `json:"schemaVersion"`
	StateKind     string            `json:"stateKind"`
	NodeID        string            `json:"nodeId"`
	SessionID     string            `json:"sessionId"`
	Sequence      uint64            `json:"sequence"`
	SentAt        time.Time         `json:"sentAt"`
	State         state.PublicState `json:"state"`
}

// NewSessionID returns a fresh V1 session identifier: 32 lowercase hex
// characters representing 16 cryptographically random bytes, generated once
// per uplink runtime start. It identifies the session; it is not a
// credential.
func NewSessionID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("session id entropy unavailable: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// StateSource is the read-only state dependency of the uplink runtime. It
// exists so the uplink can never depend on collectors or mutate state: a
// *state.Store satisfies it, and nothing else is needed.
type StateSource interface {
	Snapshot() state.InternalRootState
	Changes() <-chan struct{}
}

// SnapshotBuilder is the only place snapshots are constructed. It projects
// the store's InternalRootState through ProjectPublic at construction time
// and stamps envelope sentAt and nested generatedAt with the same UTC
// instant, as M5.2 §5.5 freezes.
type SnapshotBuilder struct {
	source  StateSource
	nodeID  string
	caps    state.RuntimeCapabilities
	project state.ProjectionConfig
	now     func() time.Time
}

func NewSnapshotBuilder(source StateSource, nodeID string, caps state.RuntimeCapabilities, project state.ProjectionConfig, now func() time.Time) *SnapshotBuilder {
	if now == nil {
		now = time.Now
	}
	return &SnapshotBuilder{source: source, nodeID: nodeID, caps: caps, project: project, now: now}
}

// Public projects the current store state to PublicState without envelope
// metadata. The digest of this value drives change detection.
func (b *SnapshotBuilder) Public() state.PublicState {
	return state.ProjectPublic(b.source.Snapshot(), b.caps, b.project, b.now().UTC())
}

// Build constructs one NodeSnapshotV1 envelope from the latest store state.
// It refuses to build a snapshot whose projected host identity does not match
// the configured node id, because the hub rejects any envelope where
// envelope.nodeId != state.host.id.
func (b *SnapshotBuilder) Build(sessionID string, sequence uint64) (NodeSnapshot, error) {
	now := b.now().UTC()
	pub := state.ProjectPublic(b.source.Snapshot(), b.caps, b.project, now)
	if pub.Host.ID != b.nodeID {
		return NodeSnapshot{}, fmt.Errorf("uplink node id %q does not match local host id %q", b.nodeID, pub.Host.ID)
	}
	return NodeSnapshot{
		SchemaVersion: envelopeSchemaVersion,
		StateKind:     envelopeStateKind,
		NodeID:        b.nodeID,
		SessionID:     sessionID,
		Sequence:      sequence,
		SentAt:        now,
		State:         pub,
	}, nil
}

// Digest hashes the canonical PublicState content. The projection timestamp
// generatedAt is normalized out so two projections of unchanged state share a
// digest: change detection must react to public content, not to projection
// time. encoding/json output is deterministic for this value type (maps are
// written in sorted key order), so equal content yields equal bytes. Public
// state contains only json-marshalable value types; the fallback tag is
// unreachable and exists to keep the function total.
func Digest(pub state.PublicState) [sha256.Size]byte {
	pub.GeneratedAt = time.Time{}
	body, err := json.Marshal(pub)
	if err != nil {
		return sha256.Sum256([]byte("uplink-digest-marshal-error"))
	}
	return sha256.Sum256(body)
}
