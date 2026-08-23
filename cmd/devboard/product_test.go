package main

import "testing"

func TestProductCommandUsesBoundedResultAndFailureCode(t *testing.T) {
	result, code := runProductCommand([]string{"service", "invalid"})
	if code != 1 || result.OK || result.SchemaVersion != 1 || result.Status != "invalid_command" {
		t.Fatalf("result=%+v code=%d", result, code)
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
