package multihost

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	FreshWindow       = 15 * time.Second
	RemoteFreshWindow = 30 * time.Second
	RetentionWindow   = 30 * time.Minute
	FutureTolerance   = 2 * time.Minute
)

type PeerStatus string

const (
	PeerUnknown     PeerStatus = "unknown"
	PeerAvailable   PeerStatus = "available"
	PeerDegraded    PeerStatus = "degraded"
	PeerUnavailable PeerStatus = "unavailable"
)

type SnapshotFreshness string

const (
	SnapshotFresh SnapshotFreshness = "fresh"
	SnapshotStale SnapshotFreshness = "stale"
)

type SourceKind string

const (
	SourceLocal SourceKind = "local"
	SourcePeer  SourceKind = "peer"
	// SourceNode marks an M5.3 push-native registered node wrapper.
	SourceNode SourceKind = "node"
)

type DashboardState struct {
	SchemaVersion int                     `json:"schemaVersion"`
	StateKind     string                  `json:"stateKind"`
	GeneratedAt   time.Time               `json:"generatedAt"`
	Hosts         []DashboardHostSnapshot `json:"hosts"`
}

type DashboardHostSnapshot struct {
	ConfiguredHostID string `json:"configuredHostId"`
	// DisplayName is the trusted registry label authority for push-native
	// node wrappers; historical peer wrappers leave it empty.
	DisplayName       string              `json:"displayName,omitempty"`
	Source            DashboardHostSource `json:"source"`
	SnapshotFreshness *SnapshotFreshness  `json:"snapshotFreshness,omitempty"`
	State             *state.PublicState  `json:"state,omitempty"`
}

type DashboardHostSource struct {
	Kind          SourceKind `json:"kind"`
	Status        PeerStatus `json:"status"`
	LastAttemptAt *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	Message       string     `json:"message"`
}

type peerRecord struct {
	configuredHostID string
	endpoint         string
	status           PeerStatus
	lastAttemptAt    *time.Time
	lastSuccessAt    *time.Time
	message          string
	lastGood         *state.PublicState
	forceStale       bool
}

type PeerSnapshotStore struct {
	mu      sync.RWMutex
	order   []string
	records map[string]peerRecord
}

func NewPeerSnapshotStore(peers []config.PeerConfig) *PeerSnapshotStore {
	s := &PeerSnapshotStore{order: make([]string, 0, len(peers)), records: make(map[string]peerRecord, len(peers))}
	for _, peer := range peers {
		s.order = append(s.order, peer.ExpectedHostID)
		s.records[peer.ExpectedHostID] = peerRecord{configuredHostID: peer.ExpectedHostID, endpoint: peer.Endpoint, status: PeerUnknown}
	}
	return s
}

func (s *PeerSnapshotStore) MarkSuccess(expectedHostID string, pub state.PublicState, at time.Time, status PeerStatus, message string) error {
	copyState, err := clonePublicState(pub)
	if err != nil {
		return err
	}
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[expectedHostID]
	if !ok {
		return fmt.Errorf("unknown peer %q", expectedHostID)
	}
	record.status = status
	record.lastAttemptAt = timePtr(at)
	record.lastSuccessAt = timePtr(at)
	record.message = message
	record.lastGood = &copyState
	record.forceStale = status != PeerAvailable
	s.records[expectedHostID] = record
	return nil
}

func (s *PeerSnapshotStore) MarkFailure(expectedHostID string, at time.Time, status PeerStatus, message string) error {
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[expectedHostID]
	if !ok {
		return fmt.Errorf("unknown peer %q", expectedHostID)
	}
	record.status = status
	record.lastAttemptAt = timePtr(at)
	record.message = message
	record.forceStale = record.lastGood != nil
	s.records[expectedHostID] = record
	return nil
}

func (s *PeerSnapshotStore) HasAcceptedHostID(exceptExpectedID, hostID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, record := range s.records {
		if id == exceptExpectedID || record.lastGood == nil {
			continue
		}
		if record.lastGood.Host.ID == hostID {
			return true
		}
	}
	return false
}

func (s *PeerSnapshotStore) PeerEndpoints() []config.PeerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]config.PeerConfig, 0, len(s.order))
	for _, id := range s.order {
		record := s.records[id]
		out = append(out, config.PeerConfig{ExpectedHostID: id, Endpoint: record.endpoint})
	}
	return out
}

// Dashboard preserves the historical M5 combined local+peer assembly for
// regression tests. M5.1 production NODE/HUB routing does not use this path.
func (s *PeerSnapshotStore) Dashboard(local state.PublicState, now time.Time) DashboardState {
	now = now.UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	localCopy, _ := clonePublicState(local)
	fresh := SnapshotFresh
	hosts := make([]DashboardHostSnapshot, 0, len(s.order)+1)
	hosts = append(hosts, DashboardHostSnapshot{
		ConfiguredHostID:  local.Host.ID,
		Source:            DashboardHostSource{Kind: SourceLocal, Status: PeerAvailable, Message: "Local state."},
		SnapshotFreshness: &fresh,
		State:             &localCopy,
	})
	hosts = append(hosts, s.peerHostsLocked(now)...)
	return DashboardState{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: hosts}
}

// DashboardPeers is the M5.1 HUB authority path. It never fabricates or
// prepends local NAS state; hosts are exactly configured peer wrappers.
func (s *PeerSnapshotStore) DashboardPeers(now time.Time) DashboardState {
	now = now.UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return DashboardState{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: s.peerHostsLocked(now)}
}

func (s *PeerSnapshotStore) peerHostsLocked(now time.Time) []DashboardHostSnapshot {
	hosts := make([]DashboardHostSnapshot, 0, len(s.order))
	for _, id := range s.order {
		record := s.records[id]
		host := DashboardHostSnapshot{
			ConfiguredHostID: id,
			Source: DashboardHostSource{
				Kind: SourcePeer, Status: record.status, LastAttemptAt: cloneTime(record.lastAttemptAt), LastSuccessAt: cloneTime(record.lastSuccessAt), Message: record.message,
			},
		}
		if record.lastGood != nil && record.lastSuccessAt != nil && now.Sub(*record.lastSuccessAt) <= RetentionWindow {
			copyState, err := clonePublicState(*record.lastGood)
			if err == nil {
				age := now.Sub(copyState.GeneratedAt)
				if age < 0 {
					age = 0
				}
				freshness := SnapshotStale
				if !record.forceStale && now.Sub(*record.lastSuccessAt) <= FreshWindow && age <= RemoteFreshWindow {
					freshness = SnapshotFresh
				}
				host.SnapshotFreshness = &freshness
				host.State = &copyState
			}
		}
		hosts = append(hosts, host)
	}
	return hosts
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

func timePtr(v time.Time) *time.Time {
	out := v
	return &out
}

func cloneTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
