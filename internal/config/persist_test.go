package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validNodeConfig() Config {
	cfg := Defaults()
	cfg.Host.ID = "mac-a"
	cfg.Host.DisplayName = "Mac A"
	cfg.Uplink = UplinkConfig{
		Enabled:  true,
		Endpoint: "http://192.0.2.10:8787",
		NodeID:   "mac-a",
		Token:    "node-token-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	return cfg
}

func validHubConfig() Config {
	cfg := Defaults()
	cfg.Runtime.Role = RuntimeRoleHub
	cfg.Server.Host = "0.0.0.0"
	cfg.Nodes.Registered = []NodeConfig{
		{NodeID: "mac-a", DisplayName: "Mac A", Token: "node-token-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{NodeID: "mac-b", DisplayName: "Mac B", Token: "node-token-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	cfg.Nodes.Disabled = []string{"mac-b"}
	cfg.Admin = AdminConfig{Enabled: true, TokenFile: "/var/lib/devboard/admin.token", PasswordFile: "/var/lib/devboard/admin.password"}
	return cfg
}

// SaveAtomic must roundtrip through the strict loader for every active
// section, including the token-bearing registry.
func TestSaveAtomicRoundtripsThroughLoad(t *testing.T) {
	for name, cfg := range map[string]Config{"node": validNodeConfig(), "hub": validHubConfig()} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := SaveAtomic(path, cfg); err != nil {
				t.Fatalf("save: %v", err)
			}
			loaded, err := Load(path)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if !reflect.DeepEqual(loaded, cfg) {
				t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", loaded, cfg)
			}
		})
	}
}

func TestSaveAtomicFileModeIs0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveAtomic(path, validNodeConfig()); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%v, want 0600", info.Mode().Perm())
	}
}

func TestSaveAtomicPostRenameDirectorySyncFailureKeepsCommittedSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveAtomic(path, validNodeConfig()); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	next := validNodeConfig()
	next.Host.DisplayName = "Committed Mac"
	syncAttempted := false
	err := saveAtomic(path, next, func(string) error {
		syncAttempted = true
		return errors.New("synthetic directory sync failure")
	})
	if !syncAttempted {
		t.Fatal("directory sync failure was not injected")
	}
	if err != nil {
		t.Fatalf("post-rename directory sync failure must retain committed success semantics: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload committed config: %v", err)
	}
	if !reflect.DeepEqual(loaded, next) {
		t.Fatalf("destination does not contain committed config:\n got %+v\nwant %+v", loaded, next)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("committed config mode=%v, want 0600", info.Mode().Perm())
	}
}

// An invalid config must never replace the destination: the validation gate
// runs before any temp file is written.
func TestSaveAtomicInvalidConfigDoesNotReplaceDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	good := validNodeConfig()
	if err := SaveAtomic(path, good); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	bad := good
	bad.Uplink.Token = "short"
	if err := SaveAtomic(path, bad); err == nil {
		t.Fatal("expected invalid config to be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid save mutated the destination config")
	}

	// No temp-file litter is left behind on failure or success.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "config.yaml" {
			t.Fatalf("unexpected leftover file %q", entry.Name())
		}
	}
}

func TestSaveAtomicCreateFailureDoesNotCorruptDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	good := validNodeConfig()
	if err := SaveAtomic(path, good); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	// Some privileged environments ignore directory write permissions. Probe
	// first and skip there so the real destination is never changed by a
	// platform-specific false assumption.
	probe, err := os.CreateTemp(dir, ".write-probe-*")
	if err == nil {
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
		t.Skip("filesystem permits writes to mode-0500 directory")
	}

	changed := good
	changed.Host.DisplayName = "Changed"
	if err := SaveAtomic(path, changed); err == nil {
		t.Fatal("expected temp-file creation failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("temp-file creation failure corrupted the destination")
	}
}

// The token roundtrips but nothing about it may reach log-shaped output:
// SaveAtomic's errors never embed the config body, and the rendered file is
// mode 0600 so only the owner can read the credential.
func TestSaveAtomicTokenNeverLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := validNodeConfig()
	if err := SaveAtomic(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	bad := cfg
	bad.Server.Port = 0
	err := SaveAtomic(path, bad)
	if err == nil || strings.Contains(err.Error(), cfg.Uplink.Token) {
		t.Fatalf("save error leaked token: %v", err)
	}
}

func TestAdminConfigValidation(t *testing.T) {
	t.Run("node role cannot enable admin", func(t *testing.T) {
		cfg := validNodeConfig()
		cfg.Admin = AdminConfig{Enabled: true, TokenFile: "/etc/devboard/admin.token"}
		err := Validate(cfg)
		if err == nil || !strings.Contains(err.Error(), "admin.enabled requires runtime.role hub") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("hub enabled without token file", func(t *testing.T) {
		cfg := validHubConfig()
		cfg.Admin = AdminConfig{Enabled: true}
		err := Validate(cfg)
		if err == nil || !strings.Contains(err.Error(), "admin.enabled requires admin.token_file") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("hub enabled with relative token file", func(t *testing.T) {
		cfg := validHubConfig()
		cfg.Admin = AdminConfig{Enabled: true, TokenFile: "admin.token"}
		err := Validate(cfg)
		if err == nil || !strings.Contains(err.Error(), "admin.token_file must be an absolute path") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("hub enabled with relative password file", func(t *testing.T) {
		cfg := validHubConfig()
		cfg.Admin.PasswordFile = "admin.password"
		err := Validate(cfg)
		if err == nil || !strings.Contains(err.Error(), "admin.password_file must be an absolute path") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("valid hub admin config", func(t *testing.T) {
		cfg := validHubConfig()
		if err := Validate(cfg); err != nil {
			t.Fatalf("valid admin config rejected: %v", err)
		}
	})
}

// The registry display-name grammar must reject separator characters, or a
// canonical save could produce a line the loader cannot parse back.
func TestRegistryDisplayNameRejectsSeparators(t *testing.T) {
	for _, name := range []string{"Mac=A", "Mac, B"} {
		cfg := validHubConfig()
		cfg.Nodes.Registered[0].DisplayName = name
		if err := Validate(cfg); err == nil {
			t.Fatalf("display name %q must be rejected", name)
		}
	}
}

func TestSaveAtomicPreservesMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveAtomic(path, validNodeConfig()); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.ModTime().After(time.Now().Add(time.Second)) {
		t.Fatal("suspicious mod time")
	}
}
