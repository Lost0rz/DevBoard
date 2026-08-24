package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperatorConfigRoundTripsAndValidatesBounds(t *testing.T) {
	cfg := validHubConfig()
	cfg.Operator = OperatorConfig{ConsoleRefreshSeconds: 30, DiagnosticsMinLevel: "warn", DiagnosticsCapacity: 350}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Operator != cfg.Operator {
		t.Fatalf("operator config=%+v want %+v", loaded.Operator, cfg.Operator)
	}
	for name, mutate := range map[string]func(*Config){
		"refresh-low":   func(c *Config) { c.Operator.ConsoleRefreshSeconds = 4 },
		"refresh-high":  func(c *Config) { c.Operator.ConsoleRefreshSeconds = 61 },
		"level":         func(c *Config) { c.Operator.DiagnosticsMinLevel = "debug" },
		"capacity-low":  func(c *Config) { c.Operator.DiagnosticsCapacity = 49 },
		"capacity-high": func(c *Config) { c.Operator.DiagnosticsCapacity = 501 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cfg
			mutate(&bad)
			if err := Validate(bad); err == nil {
				t.Fatal("invalid operator config was accepted")
			}
		})
	}
}

func TestOperatorConfigRejectsUnknownAndDuplicateKeys(t *testing.T) {
	base := strings.TrimSpace(`runtime:
  role: "hub"
operator:
  console_refresh_seconds: 10
  diagnostics_min_level: "info"
  diagnostics_capacity: 200
`)
	for name, body := range map[string]string{
		"unknown":   base + "\n  not_allowed: 1\n",
		"duplicate": base + "\n  diagnostics_capacity: 201\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("malformed operator schema was accepted")
			}
		})
	}
}

func TestHubPrivatePathChecksRejectSymlinks(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.yaml")
	if err := os.WriteFile(regular, []byte("runtime:\n  role: hub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil {
		t.Fatal("config loader followed a symlink")
	}
	if err := RequirePrivateFile(link); err == nil {
		t.Fatal("private path check accepted a symlink")
	}
	if err := RequirePrivateFile(regular); err != nil {
		t.Fatalf("regular private file rejected: %v", err)
	}
}

func TestSaveAtomicRejectsSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := SaveAtomic(link, validHubConfig()); err == nil {
		t.Fatal("SaveAtomic followed or replaced a symlink destination")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "sentinel" {
		t.Fatalf("symlink target changed: %q err=%v", content, err)
	}
}
