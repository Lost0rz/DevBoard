package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestM53ParseNodesRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.yaml")
	yaml := `runtime:
  role: hub
server:
  host: "0.0.0.0"
  port: 8787
nodes:
  registered: "mac-a=Mac A=token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa=amber,mac-b==token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  disabled: "mac-b"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load hub config: %v", err)
	}
	if len(cfg.Nodes.Registered) != 2 {
		t.Fatalf("registered nodes=%d", len(cfg.Nodes.Registered))
	}
	first := cfg.Nodes.Registered[0]
	if first.NodeID != "mac-a" || first.DisplayName != "Mac A" || first.Token != "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa" || first.Accent != "amber" {
		t.Fatalf("unexpected first node: %#v", first)
	}
	second := cfg.Nodes.Registered[1]
	if second.NodeID != "mac-b" || second.DisplayName != "" || second.Token != "token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected second node: %#v", second)
	}
	if len(cfg.Nodes.Disabled) != 1 || cfg.Nodes.Disabled[0] != "mac-b" {
		t.Fatalf("unexpected disabled list: %v", cfg.Nodes.Disabled)
	}
	if len(cfg.MultiHost.Peers) != 0 {
		t.Fatalf("peers unexpectedly configured: %v", cfg.MultiHost.Peers)
	}
}

func TestM53NodesRegistryValidation(t *testing.T) {
	valid := func() Config {
		cfg := Defaults()
		cfg.Runtime.Role = RuntimeRoleHub
		cfg.Nodes.Registered = []NodeConfig{
			{NodeID: "mac-a", DisplayName: "Mac A", Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		}
		return cfg
	}
	if err := Validate(valid()); err != nil {
		t.Fatalf("valid hub registry rejected: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"node role with registry", func(c *Config) {
			c.Runtime.Role = RuntimeRoleNode
		}},
		{"node role with disabled list", func(c *Config) {
			c.Runtime.Role = RuntimeRoleNode
			c.Nodes.Disabled = []string{"mac-a"}
		}},
		{"invalid node id", func(c *Config) {
			c.Nodes.Registered[0].NodeID = "mac a"
		}},
		{"duplicate node id", func(c *Config) {
			c.Nodes.Registered = append(c.Nodes.Registered, NodeConfig{NodeID: "mac-a", Token: "token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
		}},
		{"duplicate token", func(c *Config) {
			c.Nodes.Registered = append(c.Nodes.Registered, NodeConfig{NodeID: "mac-b", Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
		}},
		{"token too short", func(c *Config) {
			c.Nodes.Registered[0].Token = "short-token"
		}},
		{"token bad charset", func(c *Config) {
			c.Nodes.Registered[0].Token = "token with spaces and commas!,=aaaaaaaaa"
		}},
		{"display name control char", func(c *Config) {
			c.Nodes.Registered[0].DisplayName = "Mac\nA"
		}},
		{"display name too long", func(c *Config) {
			c.Nodes.Registered[0].DisplayName = strings.Repeat("M", 65)
		}},
		{"invalid accent", func(c *Config) {
			c.Nodes.Registered[0].Accent = "magenta"
		}},
		{"unknown disabled id", func(c *Config) {
			c.Nodes.Disabled = []string{"mac-zzz"}
		}},
		{"duplicate disabled id", func(c *Config) {
			c.Nodes.Registered = append(c.Nodes.Registered, NodeConfig{NodeID: "mac-b", Token: "token-bbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
			c.Nodes.Disabled = []string{"mac-a", "mac-a"}
		}},
	}
	for _, tc := range cases {
		cfg := valid()
		tc.mut(&cfg)
		if err := Validate(cfg); err == nil {
			t.Fatalf("%s: unexpectedly valid", tc.name)
		}
	}
}

func TestM53ParseNodesRejectsMalformedEntries(t *testing.T) {
	for _, value := range []string{"mac-a", "mac-a=token-only", "=Mac A=token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa", "mac-a=Mac A="} {
		if _, err := parseNodes(value); err == nil {
			t.Fatalf("expected malformed nodes.registered %q rejected", value)
		}
	}
	if nodes, err := parseNodes(""); err != nil || len(nodes) != 0 {
		t.Fatalf("empty registered must be valid and empty: %v %v", nodes, err)
	}
	if _, err := parseIDList("a,,b"); err == nil {
		t.Fatal("expected malformed disabled list rejected")
	}
}
