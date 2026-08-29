package hub

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
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
	// retired holds the session IDs that were once ACCEPTED and active in
	// this hub process but were superseded by a newer session. A retired
	// session can never become active again (M5.2 §14.6), no matter how late
	// or fresh-looking its packets are. Sessions that were merely attempted
	// but never accepted are never added. The history lives only in memory:
	// a hub restart intentionally forgets it (M5.2 §14.7).
	retired map[string]struct{}
}

// NodeStateStore is the push-native in-memory hub node state store. It holds
// no database, filesystem or queue state; a hub restart intentionally starts
// with no accepted snapshots.
type NodeStateStore struct {
	registry *Registry
	mu       sync.RWMutex
	records  map[string]*nodeRecord
	actions  *navigationActionQueue
	// readAcks is Hub-local display acknowledgement state. The node remains
	// the lifecycle authority; this overlay prevents a clicked terminal card
	// from reappearing before the node's next snapshot carries ReadAt.
	readAcks map[string]map[string]time.Time
}

func NewNodeStateStore(registry *Registry) *NodeStateStore {
	records := make(map[string]*nodeRecord, len(registry.order))
	for _, id := range registry.order {
		records[id] = &nodeRecord{}
	}
	return &NodeStateStore{registry: registry, records: records, actions: newNavigationActionQueue(), readAcks: make(map[string]map[string]time.Time)}
}

// MarkTaskRead records a successful display navigation for one terminal task.
// It is scoped to the task's opaque navigation reference and never changes
// lifecycle or provider state.
func (s *NodeStateStore) MarkTaskRead(hostID, targetID, taskID string, at time.Time) bool {
	if hostID == "" || targetID == "" || taskID == "" || at.IsZero() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[hostID]
	if rec == nil || rec.state == nil {
		return false
	}
	for _, task := range rec.state.Tasks {
		if task.ID != taskID || (task.Lifecycle != state.TaskComplete && task.Lifecycle != state.TaskError) || task.Navigation == nil || task.Navigation.TargetID != targetID {
			continue
		}
		if s.readAcks[hostID] == nil {
			s.readAcks[hostID] = make(map[string]time.Time)
		}
		s.readAcks[hostID][taskID] = at.UTC()
		return true
	}
	return false
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
		} else {
			// A previously accepted session can never become active again:
			// once another session took over, every later packet from the old
			// one is a non-active-session conflict regardless of its
			// generation instant or sequence (M5.2 §14.6).
			if rec.retired != nil {
				if _, isRetired := rec.retired[snap.SessionID]; isRetired {
					return ApplyOutcome{}, &rejection{http.StatusConflict, "retired_session"}
				}
			}
			if generatedAt.Before(ing.generatedAt) {
				// A different session may become active only without
				// regressing the accepted generation instant, so delayed
				// packets from an older process session cannot rewind newer
				// node state.
				return ApplyOutcome{}, &rejection{http.StatusConflict, "session_regression"}
			}
		}
	}
	if receivedAt.Sub(generatedAt) > AdmissionWindow {
		return ApplyOutcome{}, &rejection{http.StatusConflict, "stale_admission"}
	}

	copyState, err := clonePublicState(snap.State)
	if err != nil {
		return ApplyOutcome{}, &rejection{http.StatusInternalServerError, "state_copy"}
	}
	if ing != nil && snap.SessionID != ing.sessionID {
		// A never-seen session is taking over: the previously active session
		// becomes retired for the lifetime of this hub process.
		if rec.retired == nil {
			rec.retired = make(map[string]struct{})
		}
		rec.retired[ing.sessionID] = struct{}{}
	}
	rec.state = &copyState
	s.pruneReadAcksLocked(node.ID, &copyState)
	rec.lastReceivedAt = &receivedAt
	rec.ingest = &nodeIngest{sessionID: snap.SessionID, sequence: snap.Sequence, digest: digest, generatedAt: generatedAt}
	return ApplyOutcome{Accepted: true}, nil
}

func (s *NodeStateStore) pruneReadAcksLocked(hostID string, pub *state.PublicState) {
	acks := s.readAcks[hostID]
	if len(acks) == 0 || pub == nil {
		return
	}
	present := make(map[string]struct{}, len(pub.Tasks))
	for _, task := range pub.Tasks {
		present[task.ID] = struct{}{}
	}
	for taskID := range acks {
		if _, ok := present[taskID]; !ok {
			delete(acks, taskID)
		}
	}
}

// Dashboard assembles the aggregate read model from registry order. Status,
// freshness and retention are all derived from the hub clock at read time; no
// background goroutine flips them.
func (s *NodeStateStore) Dashboard(now time.Time) dashboard.State {
	now = now.UTC()

	s.mu.RLock()
	defer s.mu.RUnlock()

	hosts := make([]dashboard.HostSnapshot, 0, len(s.registry.order))
	for _, id := range s.registry.order {
		node := s.registry.nodes[id]
		rec := s.records[id]
		status := connectionStatus(rec.lastReceivedAt, now)
		retained := rec.state != nil && rec.lastReceivedAt != nil && now.Sub(*rec.lastReceivedAt) <= RetentionWindow
		source := dashboard.HostSource{
			Kind:          dashboard.HostSourceNode,
			Status:        dashboard.HostStatus(status),
			LastSuccessAt: cloneTime(rec.lastReceivedAt),
			Message:       nodeMessage(node, rec, status, retained),
		}
		host := dashboard.HostSnapshot{ConfiguredHostID: id, DisplayName: node.DisplayName, Accent: node.Accent, Source: source}
		if retained {
			copyState, err := clonePublicState(*rec.state)
			if err == nil {
				s.applyReadAcksLocked(id, &copyState)
				freshness := dashboard.SnapshotStale
				if status == ConnectionOnline {
					freshness = dashboard.SnapshotFresh
				}
				host.SnapshotFreshness = &freshness
				host.State = &copyState
			}
		}
		hosts = append(hosts, host)
	}
	globalQuota := dedupeQuota(hosts)
	return dashboard.State{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: hosts, Quota: globalQuota}
}

func (s *NodeStateStore) applyReadAcksLocked(hostID string, pub *state.PublicState) {
	acks := s.readAcks[hostID]
	if len(acks) == 0 || pub == nil {
		return
	}
	for i := range pub.Tasks {
		if _, ok := acks[pub.Tasks[i].ID]; ok {
			pub.Tasks[i].Unread = false
		}
	}
}

func dedupeQuota(hosts []dashboard.HostSnapshot) []state.PublicQuota {
	out := []state.PublicQuota{}
	seen := make(map[string]int)
	for _, host := range hosts {
		if host.State == nil {
			continue
		}
		for index, quota := range host.State.Quota {
			// A retained state from a stale/offline host is still useful for
			// diagnostics, but it must not outrank a fresh observation or make
			// the Hub's global quota projection look healthy by itself.
			if !hostQuotaSourceHealthy(host.Source.Status) {
				quota.SourceStatus = state.SourceDegraded
			}
			key := string(quota.Provider) + "\x00" + quota.AccountKey
			if quota.AccountKey == "" {
				key += fmt.Sprintf("\x00legacy-%s-%d", host.ConfiguredHostID, index)
			}
			if existing, exists := seen[key]; exists {
				if quotaPreferred(quota, out[existing]) {
					out[existing] = quota
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, quota)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].DisplayLabel != out[j].DisplayLabel {
			return out[i].DisplayLabel < out[j].DisplayLabel
		}
		return out[i].AccountKey < out[j].AccountKey
	})
	return out
}

func hostQuotaSourceHealthy(status dashboard.HostStatus) bool {
	switch status {
	case dashboard.HostStatus("online"), dashboard.HostAvailable:
		return true
	default:
		return false
	}
}

func quotaPreferred(candidate, current state.PublicQuota) bool {
	rank := func(status state.SourceStatus) int {
		switch status {
		case state.SourceAvailable:
			return 0
		case state.SourceDegraded:
			return 1
		default:
			return 2
		}
	}
	if rank(candidate.SourceStatus) != rank(current.SourceStatus) {
		return rank(candidate.SourceStatus) < rank(current.SourceStatus)
	}
	if candidate.SampledAt != nil && current.SampledAt != nil && !candidate.SampledAt.Equal(*current.SampledAt) {
		return candidate.SampledAt.After(*current.SampledAt)
	}
	return candidate.ObservedBy < current.ObservedBy
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
