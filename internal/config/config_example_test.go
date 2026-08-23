package config

import (
	"path/filepath"
	"testing"
)

// TestConfigExampleLoads loads the repository's real config.example.yaml with
// the production loader. The example is copy-paste documentation: if an edit
// ever makes it unparsable again (for example by reintroducing inline
// comments on scalar lines, which this loader does not strip), this test
// fails before the broken example ships.
func TestConfigExampleLoads(t *testing.T) {
	path := filepath.Join("..", "..", "config.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("config.example.yaml must load with config.Load: %v", err)
	}
	if cfg.Runtime.Role != RuntimeRoleNode {
		t.Fatalf("runtime.role=%q, want %q", cfg.Runtime.Role, RuntimeRoleNode)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("server.host=%q, want the node loopback default", cfg.Server.Host)
	}
	if cfg.Server.Port != 8787 {
		t.Fatalf("server.port=%d, want 8787", cfg.Server.Port)
	}
	if cfg.Host.ID != "mac-a" {
		t.Fatalf("host.id=%q, want %q", cfg.Host.ID, "mac-a")
	}
	if cfg.Uplink.Enabled {
		t.Fatal("uplink must stay disabled in the copyable example")
	}
	if cfg.MultiHost.Enabled || len(cfg.MultiHost.Peers) != 0 {
		t.Fatalf("example must not configure production multihost peers: %+v", cfg.MultiHost)
	}
}
