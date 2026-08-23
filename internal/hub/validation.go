package hub

import (
	"net/http"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	// MaxBodyBytes is the frozen M5.2 request body bound (256 KiB).
	MaxBodyBytes = 256 * 1024
	// FutureTolerance bounds how far a snapshot generation instant may sit in
	// the future relative to the hub clock.
	FutureTolerance = 2 * time.Minute
	// AdmissionWindow is the maximum age of a newly arriving non-duplicate
	// snapshot relative to the hub clock.
	AdmissionWindow = 30 * time.Second
	// OnlineWindow / StaleWindow freeze the connection status boundaries.
	OnlineWindow = 5 * time.Second
	StaleWindow  = 30 * time.Second
	// RetentionWindow is the frozen last-good retention duration.
	RetentionWindow = 30 * time.Minute

	EnvelopeSchemaVersion = 1
	EnvelopeStateKind     = "nodeSnapshot"
	PublicStateKind       = "public"
)

// rejection is a bounded, generic receiver outcome. class is a short
// log-only classification; client bodies carry only fixed generic text.
type rejection struct {
	status int
	class  string
}

func rejectionText(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid snapshot request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusMethodNotAllowed:
		return "method not allowed"
	case http.StatusConflict:
		return "snapshot conflict"
	case http.StatusRequestEntityTooLarge:
		return "request body too large"
	case http.StatusUnsupportedMediaType:
		return "unsupported content type"
	default:
		return "internal server error"
	}
}

// ValidNodeID enforces the frozen node id grammar: 1-64 bytes of ASCII
// letters, digits, '.', '_' or '-', case-sensitive, no surrounding space.
func ValidNodeID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// ValidSessionID enforces the frozen V1 session format: 32 lowercase hex
// characters representing one Node Uplink process session.
func ValidSessionID(sessionID string) bool {
	if len(sessionID) != 32 {
		return false
	}
	for i := 0; i < len(sessionID); i++ {
		c := sessionID[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validateEnvelope(snap *NodeSnapshot) *rejection {
	if snap.SchemaVersion != EnvelopeSchemaVersion {
		return &rejection{http.StatusBadRequest, "envelope_schema_version"}
	}
	if snap.StateKind != EnvelopeStateKind {
		return &rejection{http.StatusBadRequest, "envelope_state_kind"}
	}
	if !ValidNodeID(snap.NodeID) {
		return &rejection{http.StatusBadRequest, "envelope_node_id"}
	}
	if !ValidSessionID(snap.SessionID) {
		return &rejection{http.StatusBadRequest, "envelope_session_id"}
	}
	if snap.Sequence < 1 {
		return &rejection{http.StatusBadRequest, "envelope_sequence"}
	}
	if snap.SentAt.IsZero() {
		return &rejection{http.StatusBadRequest, "envelope_sent_at"}
	}
	return nil
}

// validateNestedState validates the nested PublicState V1 contract and the
// timestamp rules that depend only on the hub clock. Node identity binding
// against the registry happens in the receiver before this step.
func validateNestedState(snap *NodeSnapshot, now time.Time) *rejection {
	pub := &snap.State
	if pub.SchemaVersion != 1 {
		return &rejection{http.StatusBadRequest, "state_schema_version"}
	}
	if pub.StateKind != PublicStateKind {
		return &rejection{http.StatusBadRequest, "state_state_kind"}
	}
	if !ValidNodeID(pub.Host.ID) {
		return &rejection{http.StatusBadRequest, "state_host_id"}
	}
	if pub.GeneratedAt.IsZero() {
		return &rejection{http.StatusBadRequest, "state_generated_at"}
	}
	if duplicateOrEmptyTaskIDs(pub.Tasks) {
		return &rejection{http.StatusBadRequest, "state_task_ids"}
	}
	if duplicateOrEmptyAgentIDs(pub.Agents) {
		return &rejection{http.StatusBadRequest, "state_agent_ids"}
	}
	if pub.Host.ID != snap.NodeID {
		return &rejection{http.StatusForbidden, "identity_binding"}
	}
	if !snap.SentAt.Equal(pub.GeneratedAt) {
		return &rejection{http.StatusBadRequest, "sent_at_mismatch"}
	}
	if pub.GeneratedAt.After(now.Add(FutureTolerance)) {
		return &rejection{http.StatusBadRequest, "state_future_timestamp"}
	}
	return nil
}

func duplicateOrEmptyTaskIDs(tasks []state.PublicTask) bool {
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			return true
		}
		if _, ok := seen[task.ID]; ok {
			return true
		}
		seen[task.ID] = struct{}{}
	}
	return false
}

func duplicateOrEmptyAgentIDs(agents []state.PublicAgent) bool {
	seen := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		if agent.ID == "" {
			return true
		}
		if _, ok := seen[agent.ID]; ok {
			return true
		}
		seen[agent.ID] = struct{}{}
	}
	return false
}
