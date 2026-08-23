package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestM54UplinkDefaultsToDisabled(t *testing.T) {
	cfg := Defaults()
	if cfg.Uplink.Enabled {
		t.Fatalf("uplink must default to disabled")
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config rejected: %v", err)
	}
}

func TestM54UplinkParsesNodeSection(t *testing.T) {
	path := writeConfig(t, `runtime:
  role: node
host:
  id: "mac-a"
  display_name: "Mac A"
uplink:
  enabled: true
  endpoint: "https://hub.example.com"
  node_id: "mac-a"
  token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Uplink.Enabled || cfg.Uplink.Endpoint != "https://hub.example.com" || cfg.Uplink.NodeID != "mac-a" || cfg.Uplink.Token != "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected uplink config: %#v", cfg.Uplink)
	}
}

func TestM54UplinkRequiresNodeRole(t *testing.T) {
	path := writeConfig(t, `runtime:
  role: hub
nodes:
  registered: "mac-a=Mac A=token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
uplink:
  enabled: true
  endpoint: "https://hub.example.com"
  node_id: "mac-a"
  token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "uplink requires runtime.role node") {
		t.Fatalf("expected node-role boundary error, got %v", err)
	}
}

func TestM54UplinkDisabledSectionIsInertWithoutRoleError(t *testing.T) {
	path := writeConfig(t, `runtime:
  role: node
host:
  id: "mac-a"
uplink:
  enabled: false
  endpoint: "https://hub.example.com"
  node_id: "mac-a"
  token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Uplink.Enabled {
		t.Fatalf("uplink must stay disabled")
	}
}

func TestM54UplinkValidationRejectsInvalidSettings(t *testing.T) {
	base := func() Config {
		cfg := Defaults()
		cfg.Host.ID = "mac-a"
		cfg.Uplink = UplinkConfig{
			Enabled:  true,
			Endpoint: "https://hub.example.com",
			NodeID:   "mac-a",
			Token:    "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}
		return cfg
	}
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"ftp scheme", func(c *Config) { c.Uplink.Endpoint = "ftp://hub.example.com" }, "http or https"},
		{"missing host", func(c *Config) { c.Uplink.Endpoint = "https://" }, "bare host address"},
		{"query string", func(c *Config) { c.Uplink.Endpoint = "https://hub.example.com?x=1" }, "bare host address"},
		{"userinfo", func(c *Config) { c.Uplink.Endpoint = "https://user:pw@hub.example.com" }, "bare host address"},
		{"sub path", func(c *Config) { c.Uplink.Endpoint = "https://hub.example.com/hub" }, "must not include a path"},
		{"node id grammar", func(c *Config) { c.Uplink.NodeID = "mac a" }, "uplink.node_id is invalid"},
		{"node id host binding", func(c *Config) { c.Uplink.NodeID = "mac-b" }, "must equal host.id"},
		{"empty token", func(c *Config) { c.Uplink.Token = "" }, "32-128 characters"},
		{"1-char token", func(c *Config) { c.Uplink.Token = "a" }, "32-128 characters"},
		{"31-char token", func(c *Config) { c.Uplink.Token = strings.Repeat("a", 31) }, "32-128 characters"},
		{"129-char token", func(c *Config) { c.Uplink.Token = strings.Repeat("a", 129) }, "32-128 characters"},
		{"token charset", func(c *Config) { c.Uplink.Token = "token with spaces and others!!!!!!!!!" }, "unsupported characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestM54UplinkTokenLengthBoundariesAccepted pins the exact accepted window.
// The lower bound matches the hub registry invariant (M5.2 §9: credentials
// generated from at least 32 random bytes), so a token that validates here
// is always registry-representable.
func TestM54UplinkTokenLengthBoundariesAccepted(t *testing.T) {
	for _, length := range []int{32, 128} {
		cfg := Defaults()
		cfg.Host.ID = "mac-a"
		cfg.Uplink = UplinkConfig{
			Enabled:  true,
			Endpoint: "https://hub.example.com",
			NodeID:   "mac-a",
			Token:    strings.Repeat("a", length),
		}
		if err := Validate(cfg); err != nil {
			t.Fatalf("token of %d characters with valid charset must be accepted: %v", length, err)
		}
	}
}

func TestM54UplinkAcceptsExplicitHTTPLANEndpoint(t *testing.T) {
	cfg := Defaults()
	cfg.Host.ID = "mac-a"
	cfg.Uplink = UplinkConfig{
		Enabled:  true,
		Endpoint: "http://192.168.28.103:8787",
		NodeID:   "mac-a",
		Token:    "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("trusted-LAN explicit http endpoint rejected: %v", err)
	}
}
