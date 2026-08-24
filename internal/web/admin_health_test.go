package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

func TestAdminOverviewHealthMatrix(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "hub.yaml")
	tokenPath := filepath.Join(dir, "admin.token")
	cfg := config.Defaults()
	cfg.Runtime.Role = config.RuntimeRoleHub
	if err := config.SaveAtomic(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(adminTestSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	h := &AdminHandler{opts: AdminOptions{ConfigPath: cfgPath, TokenFile: tokenPath, RuntimeReady: true, Now: func() time.Time { return now }, StartedAt: now}}
	check := func(name, wantHealth, wantClass string) {
		t.Helper()
		view := h.overview(cfg)
		if view.Health != wantHealth || view.HealthClass != wantClass {
			t.Fatalf("%s: health=%q class=%q want %q/%q", name, view.Health, view.HealthClass, wantHealth, wantClass)
		}
	}

	check("all available", "healthy", "is-online")
	h.opts.RuntimeReady = false
	check("runtime unavailable", "degraded", "is-stale")
	h.opts.RuntimeReady = true
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatal(err)
	}
	check("config unavailable", "unavailable", "is-offline")
	if err := os.Chmod(cfgPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tokenPath, 0o644); err != nil {
		t.Fatal(err)
	}
	check("credential unavailable", "unavailable", "is-offline")
	if err := os.Chmod(tokenPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	check("persistent directory unavailable", "unavailable", "is-offline")
}
