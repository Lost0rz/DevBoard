package product

import "testing"

func TestSetupStopsBeforeIntegrationsWhenServiceFails(t *testing.T) {
	integrationCalls := 0
	result := runSetup(
		func(action string) operationResult {
			if action != "install" {
				t.Fatalf("service action=%q", action)
			}
			return errorResult("install_failed", "failed", nil)
		},
		func(provider, action string) operationResult {
			integrationCalls++
			return okResult("configured", "configured", nil)
		},
	)
	if result.OK || result.Status != "service_setup_failed" || integrationCalls != 0 {
		t.Fatalf("result=%+v integrationCalls=%d", result, integrationCalls)
	}
}

func TestSetupConfiguresBothProvidersAndReportsTrustBoundary(t *testing.T) {
	providers := []string{}
	result := runSetup(
		func(action string) operationResult {
			return okResult("installed", "installed", nil)
		},
		func(provider, action string) operationResult {
			if action != "install" {
				t.Fatalf("integration action=%q", action)
			}
			providers = append(providers, provider)
			if provider == integrationCodex {
				return okResult("configured_requires_trust", "trust", nil)
			}
			return okResult("configured", "configured", nil)
		},
	)
	if !result.OK || result.Status != "setup_complete_requires_trust" {
		t.Fatalf("result=%+v", result)
	}
	if len(providers) != 2 || providers[0] != integrationCodex || providers[1] != integrationClaude {
		t.Fatalf("providers=%v", providers)
	}
	if result.Data["nextAction"] != "configure_codex_surface" {
		t.Fatalf("nextAction=%v", result.Data["nextAction"])
	}
}

func TestSetupStillAttemptsClaudeWhenCodexNeedsManualConfiguration(t *testing.T) {
	claudeCalled := false
	result := runSetup(
		func(action string) operationResult {
			return okResult("installed", "installed", nil)
		},
		func(provider, action string) operationResult {
			if provider == integrationCodex {
				return errorResult("manual_configuration_required", "manual", nil)
			}
			claudeCalled = true
			return okResult("configured", "configured", nil)
		},
	)
	if result.OK || result.Status != "manual_configuration_required" || !claudeCalled {
		t.Fatalf("result=%+v claudeCalled=%v", result, claudeCalled)
	}
}
