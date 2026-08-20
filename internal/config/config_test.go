package config

import (
	"os"
	"path/filepath"
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
	if cfg.Agent.StaleAfterSeconds != 300 {
		t.Fatalf("unexpected agent config: %+v", cfg.Agent)
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
