package config

import "testing"

func TestM5PeerEndpointValidation(t *testing.T) {
	valid := []string{
		"10.0.0.1:8787",
		"172.16.1.2:8787",
		"192.168.1.50:8787",
		"100.64.0.12:8787",
		"[fc00::1234]:8787",
		"[fd12:3456::1]:8787",
	}
	for _, endpoint := range valid {
		if _, err := ParsePeerEndpoint(endpoint); err != nil {
			t.Fatalf("expected %q valid: %v", endpoint, err)
		}
	}

	invalid := []string{
		"8.8.8.8:8787",
		"127.0.0.1:8787",
		"0.0.0.0:8787",
		"169.254.1.2:8787",
		"224.0.0.1:8787",
		"[fe80::1]:8787",
		"example.com:8787",
		"192.168.1.2",
		"http://192.168.1.2:8787",
		"192.168.1.2:8787/api/state",
		"192.168.1.2:8787?x=1",
		"user@192.168.1.2:8787",
	}
	for _, endpoint := range invalid {
		if _, err := ParsePeerEndpoint(endpoint); err == nil {
			t.Fatalf("expected %q invalid", endpoint)
		}
	}
}

func TestM5ConfigValidationAndOrdering(t *testing.T) {
	cfg := Defaults()
	if cfg.MultiHost.Enabled {
		t.Fatal("multi-host must default disabled")
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("disabled zero peers should be valid: %v", err)
	}

	cfg.MultiHost.Enabled = true
	cfg.MultiHost.Peers = []PeerConfig{
		{ExpectedHostID: "macbook", Endpoint: "192.168.1.50:8787"},
		{ExpectedHostID: "mac-mini-2", Endpoint: "100.64.0.12:8787"},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid peers rejected: %v", err)
	}
	if cfg.MultiHost.Peers[0].ExpectedHostID != "macbook" || cfg.MultiHost.Peers[1].ExpectedHostID != "mac-mini-2" {
		t.Fatal("peer order changed")
	}
}

func TestM5ConfigRejectsDuplicatesAndLocalCollision(t *testing.T) {
	cases := []struct {
		name  string
		peers []PeerConfig
	}{
		{"duplicate host", []PeerConfig{{ExpectedHostID: "peer", Endpoint: "192.168.1.2:8787"}, {ExpectedHostID: "peer", Endpoint: "192.168.1.3:8787"}}},
		{"duplicate endpoint", []PeerConfig{{ExpectedHostID: "peer-a", Endpoint: "192.168.1.2:8787"}, {ExpectedHostID: "peer-b", Endpoint: "192.168.1.2:8787"}}},
		{"local collision", []PeerConfig{{ExpectedHostID: "local", Endpoint: "192.168.1.2:8787"}}},
		{"bad host id", []PeerConfig{{ExpectedHostID: "bad host", Endpoint: "192.168.1.2:8787"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			cfg.MultiHost.Enabled = true
			cfg.MultiHost.Peers = tc.peers
			if err := Validate(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParsePeers(t *testing.T) {
	peers, err := parsePeers(" macbook=192.168.1.50:8787, mac-mini-2=100.64.0.12:8787 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[0].ExpectedHostID != "macbook" || peers[1].Endpoint != "100.64.0.12:8787" {
		t.Fatalf("unexpected peers: %#v", peers)
	}
	for _, malformed := range []string{"peer", "=192.168.1.2:8787", "peer="} {
		if _, err := parsePeers(malformed); err == nil {
			t.Fatalf("expected malformed %q rejected", malformed)
		}
	}
}
