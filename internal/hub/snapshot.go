package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// NodeSnapshot is the frozen M5.2 NodeSnapshotV1 machine envelope. The nested
// payload is always an already-sanitized state.PublicState; InternalState is
// never a valid node snapshot.
type NodeSnapshot struct {
	SchemaVersion int               `json:"schemaVersion"`
	StateKind     string            `json:"stateKind"`
	NodeID        string            `json:"nodeId"`
	SessionID     string            `json:"sessionId"`
	Sequence      uint64            `json:"sequence"`
	SentAt        time.Time         `json:"sentAt"`
	State         state.PublicState `json:"state"`
}

var errTrailingData = errors.New("snapshot body has trailing data")

// decodeNodeSnapshot decodes exactly one NodeSnapshotV1 JSON value inside the
// already-bounded request body. Unknown fields and trailing content keep the
// machine contract strict.
func decodeNodeSnapshot(body []byte) (NodeSnapshot, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var snap NodeSnapshot
	if err := dec.Decode(&snap); err != nil {
		return NodeSnapshot{}, err
	}
	// Exactly one top-level value: a second Decode must hit io.EOF. More()
	// cannot validate the top level — it reports false for trailing `]`, `}`
	// or bare scalars — so this is the only strict EOF check.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errTrailingData
		}
		return NodeSnapshot{}, err
	}
	return snap, nil
}

// payloadDigest digests the exact bounded request bytes so an exact retry of
// an accepted tuple is recognizable while any different body conflicts.
func payloadDigest(body []byte) [sha256.Size]byte {
	return sha256.Sum256(body)
}
