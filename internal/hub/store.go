package hub

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/state"
)

// ConnectionStatus is the frozen M5.2 hub-clock classification.
type ConnectionStatus string

const (
	ConnectionOnline  ConnectionStatus = "online"
	ConnectionStale   ConnectionStatus = "stale"
	ConnectionOffline ConnectionStatus = "offline"
)

// nodeIngest is the private per-node ordering authority. The retained digest
// is ingestion metadata only and is never exposed through read APIs.
type nodeIngest struct {
	sessionID   string
	sequence    uint64
	digest      [sha256.Size]byte
	generatedAt time.Time
}

type nodeRecord struct {
	state          *state.PublicState
	lastReceivedAt *time.Time
	ingest         *nodeIngest
}

// NodeStateStore is the push-native in-memory hub node state store. It holds
// no database, filesystem or queue state; a hub restart intentionally starts
// with no accepted snapshots.
type NodeStateStore struct {
	registry *Registry
	mu       sync.RWMutex
	records  map[string]*nodeRecord
}

func NewNodeStateStore(registry *Registry) *NodeStateStore {
	records := make(map[string]*nodeRecord, len(registry.order))
	for _, id := range registry.order {
		records[id] = &nodeRecord{}
	}
	return &NodeStateStore{registry: registry, records: records}
}

// ApplyOutcome describes how a fully validated snapshot was admitted.
type ApplyOutcome struct {
	Accepted  bool
	Duplicate bool
}

// Apply evaluates the frozen M5.2 ordering/idempotency rules and commits the
// accepted state atomically. It must be called only after the receiver has
// completed authentication, identity binding, schema and future-timestamp
// validation. Rejected input never mutates accepted state, lastReceivedAt or
// ordering metadata.
func (s *NodeStateStore) Apply(node *Node, snap NodeSnapshot, digest [sha256.Size]byte, receivedAt time.Time) (ApplyOutcome, *rejection) {
	receivedAt = receivedAt.UTC()
	generatedAt := snap.State.GeneratedAt

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.records[node.ID]
	if !ok {
		return ApplyOutcome{}, &rejection{http.StatusInternalServerError, "store_node_missing"}
	}
	ing := rec.ingest
	if ing != nil && snap.SessionID == ing.sessionID && snap.Sequence == ing.sequence {
		if digest == ing.digest {
			// Exact retry of the accepted tuple: idempotent success. The
			// receive refreshes liveness only while the retried snapshot is
			// still inside its admission window, so an ancient retry can
			// never present retained data as live.
			if receivedAt.Sub(generatedAt) <= AdmissionWindow {
				rec.lastReceivedAt = &receivedAt
			}
			return ApplyOutcome{Duplicate: true}, nil
		}
		return ApplyOutcome{}, &rejection{http.StatusConflict, "tuple_mismatch"}
	}
	if ing != nil {
		if snap.SessionID == ing.sessionID {
			if snap.Sequence < ing.sequence {
				return ApplyOutcome{}, &rejection{http.StatusConflict, "lower_sequence"}
			}
		} else if generatedAt.Before(ing.generatedAt) {
			// A different session may become active only without regressing
			// the accepted generation instant, so delayed packets from an
			// older process session cannot rewind newer node state.
			return ApplyOutcome{}, &rejection{http.StatusConflict, "session_regression"}
		}
	}
	if receivedAt.Sub(generatedAt) > AdmissionWindow {
		return ApplyOutcome{}, &rejection{http.StatusConflict, "stale_admission"}
	}

	copyState, err := clonePublicState(snap.State)
	if err != nil {
		return ApplyOutcome{}, &rejection{http.StatusInternalServerError, "state_copy"}
	}
	rec.state = &copyState
	rec.lastReceivedAt = &receivedAt
	rec.ingest = &nodeIngest{sessionID: snap.SessionID, sequence: snap.Sequence, digest: digest, generatedAt: generatedAt}
	return ApplyOutcome{Accepted: true}, nil
}

// Dashboard assembles the aggregate read model from registry order. Status,
// freshness and retention are all derived from the hub clock at read time; no
// background goroutine flips them.
func (s *NodeStateStore) Dashboard(now time.Time) multihost.DashboardState {
	now = now.UTC()

	s.mu.RLock()
	defer s.mu.RUnlock()

	hosts := make([]multihost.DashboardHostSnapshot, 0, len(s.registry.order))
	for _, id := range s.registry.order {
		node := s.registry.nodes[id]
		rec := s.records[id]
		status := connectionStatus(rec.lastReceivedAt, now)
		retained := rec.state != nil && rec.lastReceivedAt != nil && now.Sub(*rec.lastReceivedAt) <= RetentionWindow
		source := multihost.DashboardHostSource{
			Kind:          multihost.SourceNode,
			Status:        multihost.PeerStatus(status),
			LastSuccessAt: cloneTime(rec.lastReceivedAt),
			Message:       nodeMessage(node, rec, status, retained),
		}
		host := multihost.DashboardHostSnapshot{ConfiguredHostID: id, DisplayName: node.DisplayName, Source: source}
		if retained {
			copyState, err := clonePublicState(*rec.state)
			if err == nil {
				freshness := multihost.SnapshotStale
				if status == ConnectionOnline {
					freshness = multihost.SnapshotFresh
				}
				host.SnapshotFreshness = &freshness
				host.State = &copyState
			}
		}
		hosts = append(hosts, host)
	}
	return multihost.DashboardState{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: hosts}
}

func connectionStatus(lastReceivedAt *time.Time, now time.Time) ConnectionStatus {
	if lastReceivedAt == nil {
		return ConnectionOffline
	}
	age := now.Sub(*lastReceivedAt)
	switch {
	case age <= OnlineWindow:
		return ConnectionOnline
	case age <= StaleWindow:
		return ConnectionStale
	default:
		return ConnectionOffline
	}
}

func nodeMessage(node *Node, rec *nodeRecord, status ConnectionStatus, retained bool) string {
	if !node.Enabled {
		if retained {
			return "Node disabled; retained snapshot shown."
		}
		return "Node disabled."
	}
	if rec.lastReceivedAt == nil {
		return "Registered node awaiting first snapshot."
	}
	if retained {
		if status == ConnectionOnline {
			return "Receiving node snapshots."
		}
		return "Node is not sending snapshots; retained state shown."
	}
	return "Node offline."
}

func clonePublicState(in state.PublicState) (state.PublicState, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return state.PublicState{}, err
	}
	var out state.PublicState
	if err := json.Unmarshal(body, &out); err != nil {
		return state.PublicState{}, err
	}
	return out, nil
}

func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
