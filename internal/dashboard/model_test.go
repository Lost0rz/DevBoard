package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// The dashboard read model carries the frozen M5 wire contract. The neutral
// package must encode exactly the historical JSON: field names, omitempty
// behavior and enum wire values are unchanged from the multihost era.
func TestReadModelJSONShapeIsFrozen(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	success := at.Add(-2 * time.Second)
	fresh := SnapshotFresh
	model := State{
		SchemaVersion: 1,
		StateKind:     "dashboard",
		GeneratedAt:   at,
		Hosts: []HostSnapshot{{
			ConfiguredHostID: "mac-a",
			DisplayName:      "Mac A",
			Source: HostSource{
				Kind:          HostSourceNode,
				Status:        "online",
				LastSuccessAt: &success,
				Message:       "Receiving node snapshots.",
			},
			SnapshotFreshness: &fresh,
			State: &state.PublicState{
				SchemaVersion: 1,
				StateKind:     "public",
				GeneratedAt:   at,
				Host:          state.PublicHost{ID: "mac-a"},
				Sources:       map[string]state.PublicSourceHealth{},
			},
		}},
	}
	body, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schemaVersion"] != float64(1) || decoded["stateKind"] != "dashboard" {
		t.Fatalf("envelope keys changed: %s", body)
	}
	if generated, ok := decoded["generatedAt"].(string); !ok || generated != "2026-08-22T12:00:00Z" {
		t.Fatalf("generatedAt changed: %s", body)
	}
	hosts, ok := decoded["hosts"].([]any)
	if !ok || len(hosts) != 1 {
		t.Fatalf("hosts changed: %s", body)
	}
	host, ok := hosts[0].(map[string]any)
	if !ok {
		t.Fatalf("host entry not an object: %s", body)
	}
	if host["configuredHostId"] != "mac-a" || host["displayName"] != "Mac A" {
		t.Fatalf("host identity keys changed: %s", body)
	}
	source, ok := host["source"].(map[string]any)
	if !ok {
		t.Fatalf("source not an object: %s", body)
	}
	if source["kind"] != "node" || source["status"] != "online" || source["message"] != "Receiving node snapshots." {
		t.Fatalf("source keys changed: %s", body)
	}
	if _, present := source["lastAttemptAt"]; present {
		t.Fatalf("nil lastAttemptAt must be omitted: %s", body)
	}
	if host["snapshotFreshness"] != "fresh" {
		t.Fatalf("snapshotFreshness changed: %s", body)
	}
	nested, ok := host["state"].(map[string]any)
	if !ok || nested["stateKind"] != "public" {
		t.Fatalf("nested state changed: %s", body)
	}
}

// Optional fields stay optional: an empty wrapper encodes only the required
// identity and source keys, exactly like the historical read model.
func TestEmptyWrapperOmitsOptionalFields(t *testing.T) {
	body, err := json.Marshal(HostSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"configuredHostId", "source"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("required key %q missing: %s", key, body)
		}
	}
	for _, key := range []string{"displayName", "snapshotFreshness", "state"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("optional key %q must be omitted when empty: %s", key, body)
		}
	}
	source := decoded["source"].(map[string]any)
	for _, key := range []string{"kind", "status", "message"} {
		if _, ok := source[key]; !ok {
			t.Fatalf("required source key %q missing: %s", key, body)
		}
	}
	for _, key := range []string{"lastAttemptAt", "lastSuccessAt"} {
		if _, ok := source[key]; ok {
			t.Fatalf("nil source timestamp %q must be omitted: %s", key, body)
		}
	}
}

// Enum wire values are frozen: the neutral package must not rename them.
func TestEnumWireValuesAreFrozen(t *testing.T) {
	statuses := map[HostStatus]string{
		HostUnknown:     "unknown",
		HostAvailable:   "available",
		HostDegraded:    "degraded",
		HostUnavailable: "unavailable",
	}
	for value, wire := range statuses {
		if string(value) != wire {
			t.Fatalf("status %q != wire %q", value, wire)
		}
	}
	kinds := map[HostSourceKind]string{
		HostSourceLocal: "local",
		HostSourcePeer:  "peer",
		HostSourceNode:  "node",
	}
	for value, wire := range kinds {
		if string(value) != wire {
			t.Fatalf("source kind %q != wire %q", value, wire)
		}
	}
	if string(SnapshotFresh) != "fresh" || string(SnapshotStale) != "stale" {
		t.Fatalf("freshness wire values changed: %q %q", SnapshotFresh, SnapshotStale)
	}
}
