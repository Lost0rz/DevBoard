package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductServiceActionGrammarIsPortable(t *testing.T) {
	for _, action := range []string{"install", "status", "restart", "uninstall"} {
		if !validProductServiceAction(action) {
			t.Errorf("valid service action %q was rejected", action)
		}
	}
	for _, action := range []string{"", "invalid", "start", "Install", "install-now"} {
		if validProductServiceAction(action) {
			t.Errorf("invalid service action %q was accepted", action)
		}
	}
}

func TestProductCommandRejectsInvalidServiceActionsBeforePlatformDispatch(t *testing.T) {
	for _, action := range []string{"", "invalid", "start", "Install", "install-now"} {
		result, code := runProductCommand([]string{"service", action})
		if code != 1 || result.OK || result.SchemaVersion != 1 || result.Status != "invalid_command" {
			t.Fatalf("action=%q result=%+v code=%d", action, result, code)
		}
	}
}

func TestProductIntegrationCLIRejectsProviderSpecificStatus(t *testing.T) {
	for _, provider := range []string{"codex", "claude-code"} {
		result, code := runProductCommand([]string{"integrations", "status", provider})
		if code != 1 || result.OK || result.Status != "invalid_command" {
			t.Fatalf("provider=%s result=%+v code=%d", provider, result, code)
		}
	}
}

func TestProductSetupRejectsExtraArguments(t *testing.T) {
	result, code := runProductCommand([]string{"setup", "extra"})
	if code == 0 || result.Status != "invalid_command" {
		t.Fatalf("result=%+v code=%d", result, code)
	}
}

func TestProductOnboardUsageDocumentsQuotaSecurityFlags(t *testing.T) {
	result, code := runProductCommand([]string{"node", "not-onboard"})
	if code != 1 || result.Status != "invalid_command" {
		t.Fatalf("result=%+v code=%d", result, code)
	}
	for _, flag := range []string{"--quota-identity-key-file", "--quota-alias-file"} {
		if !strings.Contains(result.Message, flag) {
			t.Fatalf("usage missing %s: %q", flag, result.Message)
		}
	}
}

func TestProductMacStatusAlignsExitCodeWithResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	if err := os.WriteFile(path, []byte("not valid config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, code := runProductCommand([]string{"mac", "status", "--config", path})
	if code != 1 || result.OK || result.Status != "setup_config_unreadable" {
		t.Fatalf("result=%+v code=%d", result, code)
	}
}

func TestProductMacCommandDocumentsProtectedStdinBoundary(t *testing.T) {
	// The main command owns os.Stdin; this grammar test still pins that the
	// public command is distinct from the legacy browser/onboarding paths.
	result, code := runProductCommand([]string{"mac", "invalid"})
	if code != 1 || result.OK || result.Status != "invalid_command" || !strings.Contains(result.Message, "configure reads protected JSON from stdin") {
		t.Fatalf("result=%+v code=%d", result, code)
	}
}
