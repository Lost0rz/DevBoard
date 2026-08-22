package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNetworkDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Network.ProbeAddress != "1.1.1.1:443" || cfg.Network.ProbeTimeoutMilliseconds != 1500 {
		t.Fatalf("unexpected network defaults: %+v", cfg.Network)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestLoadCustomNetworkConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("network:\n  probe_address: \"example.com:8443\"\n  probe_timeout_milliseconds: 2500\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network.ProbeAddress != "example.com:8443" || cfg.Network.ProbeTimeoutMilliseconds != 2500 {
		t.Fatalf("unexpected network config: %+v", cfg.Network)
	}
}

func TestValidateRejectsInvalidProbeAddress(t *testing.T) {
	for _, value := range []string{"", "https://example.com", "example.com", "example.com:notaport", ":443", "user@example.com:443", "example.com/path:443", "example.com;rm:443", "[fe80::1%en0]:443"} {
		cfg := Defaults()
		cfg.Network.ProbeAddress = value
		if err := Validate(cfg); err == nil {
			t.Fatalf("accepted invalid probe address %q", value)
		}
	}
}

func TestValidateRejectsInvalidProbeTimeout(t *testing.T) {
	for _, value := range []int{0, -1, maxProbeTimeoutMilliseconds + 1} {
		cfg := Defaults()
		cfg.Network.ProbeTimeoutMilliseconds = value
		if err := Validate(cfg); err == nil {
			t.Fatalf("accepted invalid probe timeout %d", value)
		}
	}
}
