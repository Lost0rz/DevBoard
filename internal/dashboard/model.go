// Package dashboard holds the neutral dashboard read model shared by the
// push-native hub runtime and the legacy pull multihost model. It owns only
// data types over the frozen state.PublicState contract; it knows nothing
// about how snapshots are transported or collected.
package dashboard

import (
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// HostStatus is the frozen dashboard host status enum. The constants below
// cover the legacy pull classifications; push-native node wrappers carry the
// hub-clock statuses (online/stale/offline) through the same field.
type HostStatus string

const (
	HostUnknown     HostStatus = "unknown"
	HostAvailable   HostStatus = "available"
	HostDegraded    HostStatus = "degraded"
	HostUnavailable HostStatus = "unavailable"
)

// SnapshotFreshness classifies the retained nested state of a host wrapper.
type SnapshotFreshness string

const (
	SnapshotFresh SnapshotFreshness = "fresh"
	SnapshotStale SnapshotFreshness = "stale"
)

// HostSourceKind marks how a dashboard host wrapper was produced.
type HostSourceKind string

const (
	// HostSourceLocal marks the combined local host wrapper.
	HostSourceLocal HostSourceKind = "local"
	// HostSourcePeer marks a legacy pull peer wrapper.
	HostSourcePeer HostSourceKind = "peer"
	// HostSourceNode marks a push-native registered node wrapper.
	HostSourceNode HostSourceKind = "node"
)

// State is the aggregate dashboard read model.
type State struct {
	SchemaVersion int            `json:"schemaVersion"`
	StateKind     string         `json:"stateKind"`
	GeneratedAt   time.Time      `json:"generatedAt"`
	Hosts         []HostSnapshot `json:"hosts"`
}

// HostSnapshot is one configured host wrapper inside the aggregate dashboard.
type HostSnapshot struct {
	ConfiguredHostID string `json:"configuredHostId"`
	// DisplayName is the trusted registry label authority for push-native
	// node wrappers; historical peer wrappers leave it empty.
	DisplayName       string             `json:"displayName,omitempty"`
	Source            HostSource         `json:"source"`
	SnapshotFreshness *SnapshotFreshness `json:"snapshotFreshness,omitempty"`
	State             *state.PublicState `json:"state,omitempty"`
}

// HostSource describes the transport-level health of one host wrapper.
type HostSource struct {
	Kind          HostSourceKind `json:"kind"`
	Status        HostStatus     `json:"status"`
	LastAttemptAt *time.Time     `json:"lastAttemptAt,omitempty"`
	LastSuccessAt *time.Time     `json:"lastSuccessAt,omitempty"`
	Message       string         `json:"message"`
}
