package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Validate(Defaults()); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`server:
  host: "0.0.0.0"
  port: 9000
host:
  id: "synthetic-host"
  display_name: "Synthetic Host"
display:
  kindle_refresh_seconds: 30
  complete_high_visibility_seconds: 120
  complete_retention_seconds: 600
agent:
  stale_after_seconds: 300
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 9000 || cfg.Host.ID != "synthetic-host" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Server.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone default=%q, want Asia/Shanghai", cfg.Server.Timezone)
	}
	if cfg.Agent.StaleAfterSeconds != 300 {
		t.Fatalf("unexpected agent config: %+v", cfg.Agent)
	}
	if cfg.Display.PadPath != "/display" || cfg.Display.KindleRightPath != "/kindle/R" || cfg.Display.KindleLeftPath != "/kindle/L" {
		t.Fatalf("unexpected display paths: %+v", cfg.Display)
	}
}

func TestServerTimezoneRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Defaults()
	cfg.Server.Timezone = "America/New_York"
	if err := SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Timezone != cfg.Server.Timezone {
		t.Fatalf("timezone did not round-trip: got=%q want=%q", loaded.Server.Timezone, cfg.Server.Timezone)
	}
	invalid := Defaults()
	invalid.Server.Timezone = "Not/A-Timezone"
	if err := Validate(invalid); err == nil {
		t.Fatal("invalid IANA timezone accepted")
	}
}

func TestDisplayPathsRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Defaults()
	cfg.Display.PadPath = "/wall"
	cfg.Display.KindleRightPath = "/paper/right"
	cfg.Display.KindleLeftPath = "/paper/left"
	if err := SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Display.PadPath != cfg.Display.PadPath || loaded.Display.KindleRightPath != cfg.Display.KindleRightPath || loaded.Display.KindleLeftPath != cfg.Display.KindleLeftPath {
		t.Fatalf("display paths did not round-trip: got=%+v want=%+v", loaded.Display, cfg.Display)
	}
	for name, mutate := range map[string]func(*Config){
		"empty":     func(c *Config) { c.Display.PadPath = "" },
		"reserved":  func(c *Config) { c.Display.PadPath = "/admin/display" },
		"duplicate": func(c *Config) { c.Display.KindleLeftPath = c.Display.KindleRightPath },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := Defaults()
			mutate(&invalid)
			if err := Validate(invalid); err == nil {
				t.Fatal("invalid display paths accepted")
			}
		})
	}
}

func TestAgentQuotaConfigRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Defaults()
	cfg.Runtime.Role = RuntimeRoleHub
	cfg.AgentQuota = AgentQuotaConfig{
		Enabled:   true,
		Provider:  "glm",
		Endpoint:  "https://example.invalid/v4/chat/completions",
		Model:     "glm-test",
		Schedules: []string{"05:00", "10:00", "15:00"},
	}
	if err := SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.AgentQuota, cfg.AgentQuota) {
		t.Fatalf("agent quota did not round-trip: got=%+v want=%+v", loaded.AgentQuota, cfg.AgentQuota)
	}
	for name, mutate := range map[string]func(*Config){
		"wrong provider": func(c *Config) { c.AgentQuota.Provider = "codex" },
		"no schedule":    func(c *Config) { c.AgentQuota.Schedules = nil },
		"bad schedule":   func(c *Config) { c.AgentQuota.Schedules = []string{"5:00"} },
		"duplicate time": func(c *Config) { c.AgentQuota.Schedules = []string{"05:00", "05:00"} },
		"missing model":  func(c *Config) { c.AgentQuota.Model = "" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cfg
			mutate(&invalid)
			if err := Validate(invalid); err == nil {
				t.Fatal("invalid agent quota config accepted")
			}
		})
	}
}

func TestValidateRejectsInvalid(t *testing.T) {
	cases := []Config{
		func() Config { c := Defaults(); c.Server.Port = 70000; return c }(),
		func() Config { c := Defaults(); c.Host.ID = ""; return c }(),
		func() Config { c := Defaults(); c.Display.KindleRefreshSeconds = 0; return c }(),
		func() Config {
			c := Defaults()
			c.Display.CompleteRetentionSeconds = 10
			c.Display.CompleteHighVisibilitySeconds = 20
			return c
		}(),
		func() Config { c := Defaults(); c.Agent.StaleAfterSeconds = 0; return c }(),
	}
	for i, cfg := range cases {
		if err := Validate(cfg); err == nil {
			t.Fatalf("case %d unexpectedly valid", i)
		}
	}
}
