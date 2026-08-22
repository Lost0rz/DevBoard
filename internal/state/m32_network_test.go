package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNetworkPublicProjectionAllowListAndPrivacy(t *testing.T) {
	reachable := true
	latency := 42.5
	failure := 8.3
	recv := 1234.0
	send := 567.0
	now := time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)
	internal := LiveInitialState(now, HostState{ID: "host"})
	internal.Network = NetworkState{
		Quality:               NetworkGood,
		Reachable:             &reachable,
		ConnectLatencyMs:      &latency,
		ProbeFailurePercent:   &failure,
		ReceiveBytesPerSecond: &recv,
		SendBytesPerSecond:    &send,
	}
	internal.InternalMeta.PrivateNote = "1.1.1.1:443 en0 192.0.2.10 raw dial error secret"
	internal.Sources["network"] = SourceHealth{Status: SourceAvailable, Message: "Network health collector is available."}

	pub := ProjectPublic(internal, RuntimeCapabilities{}, ProjectionConfig{}, now)
	if pub.SchemaVersion != 1 || pub.Network.Quality != NetworkGood || pub.Network.Reachable == nil || !*pub.Network.Reachable {
		t.Fatalf("unexpected public network: %+v", pub.Network)
	}
	body, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	network, ok := root["network"].(map[string]any)
	if !ok {
		t.Fatalf("network object missing: %s", body)
	}
	wantKeys := map[string]bool{
		"quality": true, "reachable": true, "connectLatencyMs": true,
		"probeFailurePercent": true, "receiveBytesPerSecond": true, "sendBytesPerSecond": true,
	}
	if len(network) != len(wantKeys) {
		t.Fatalf("unexpected network keys: %#v", network)
	}
	for key := range network {
		if !wantKeys[key] {
			t.Fatalf("unexpected public network field %q", key)
		}
	}
	text := string(body)
	for _, forbidden := range []string{"1.1.1.1:443", "en0", "192.0.2.10", "raw dial error", "privateNote", "probe_address", "localIP", "interfaceName", "MAC"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public projection leaked %q: %s", forbidden, text)
		}
	}
}

func TestNetworkPointersAreDeepCloned(t *testing.T) {
	reachable := true
	latency := 10.0
	root := InternalRootState{Network: NetworkState{Quality: NetworkGood, Reachable: &reachable, ConnectLatencyMs: &latency}, Sources: map[string]SourceHealth{}}
	clone := CloneInternalRootState(root)
	*clone.Network.Reachable = false
	*clone.Network.ConnectLatencyMs = 99
	if !*root.Network.Reachable || *root.Network.ConnectLatencyMs != 10 {
		t.Fatal("network pointers alias across store clone")
	}
}

func TestLiveNetworkStartsUnknownAndNull(t *testing.T) {
	root := LiveInitialState(time.Now(), HostState{ID: "h"})
	if root.Network.Quality != NetworkUnknown || root.Network.Reachable != nil || root.Network.ConnectLatencyMs != nil || root.Network.ProbeFailurePercent != nil || root.Network.ReceiveBytesPerSecond != nil || root.Network.SendBytesPerSecond != nil {
		t.Fatalf("unexpected initial network state: %+v", root.Network)
	}
	if root.Sources["network"].Status != SourceUnavailable {
		t.Fatalf("unexpected network source: %+v", root.Sources["network"])
	}
}

func TestMockNetworkIsDeterministicSyntheticState(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC)
	root := MockInternalState(now, HostState{ID: "h"})
	if root.Network.Quality != NetworkGood || root.Network.Reachable == nil || !*root.Network.Reachable {
		t.Fatalf("unexpected mock network: %+v", root.Network)
	}
	if root.Network.ConnectLatencyMs == nil || *root.Network.ConnectLatencyMs != 43 || root.Network.ProbeFailurePercent == nil || *root.Network.ProbeFailurePercent != 0 {
		t.Fatalf("unexpected mock network metrics: %+v", root.Network)
	}
	if root.Sources["network"].Status != SourceAvailable {
		t.Fatalf("unexpected mock source: %+v", root.Sources["network"])
	}
}
