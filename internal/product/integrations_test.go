package product

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testProductPaths(t *testing.T) Paths {
	t.Helper()
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	return paths
}

func readTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCodexInstallIsExactAndIdempotent(t *testing.T) {
	paths := testProductPaths(t)
	first := runIntegrationAt(paths, integrationCodex, "install")
	if !first.OK || first.Status != "configured_requires_trust" {
		t.Fatalf("install result=%+v", first)
	}
	if mode := mustMode(t, paths.CodexHooks); mode != 0o600 {
		t.Fatalf("new Codex hooks mode=%o", mode)
	}
	before, err := os.ReadFile(paths.CodexHooks)
	if err != nil {
		t.Fatal(err)
	}
	root := readTestJSON(t, paths.CodexHooks)
	hooks := root["hooks"].(map[string]any)
	if len(hooks) != len(codexEvents) {
		t.Fatalf("events=%d want %d", len(hooks), len(codexEvents))
	}
	for _, event := range codexEvents {
		groups := hooks[event].([]any)
		if len(groups) != 1 {
			t.Fatalf("%s groups=%d", event, len(groups))
		}
		handler := groups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		if handler["command"] != shellQuote(paths.Binary)+" agent-hook codex" {
			t.Fatalf("%s command=%v", event, handler["command"])
		}
	}
	second := runIntegrationAt(paths, integrationCodex, "install")
	if !second.OK {
		t.Fatalf("idempotent install result=%+v", second)
	}
	after, err := os.ReadFile(paths.CodexHooks)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("idempotent reinstall rewrote the Codex file")
	}
	removed := runIntegrationAt(paths, integrationCodex, "remove")
	if !removed.OK || countOwnedHandlers(integrationDefinition{provider: integrationCodex, path: paths.CodexHooks, command: shellQuote(paths.Binary) + " agent-hook codex"}, readTestJSON(t, paths.CodexHooks)) != 0 {
		t.Fatalf("remove result=%+v", removed)
	}
}

func TestCodexConflictAndMalformedJSONDoNotMutate(t *testing.T) {
	paths := testProductPaths(t)
	if err := os.MkdirAll(paths.CodexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CodexConfig, []byte("[hooks]\nUserPromptSubmit = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conflict := runIntegrationAt(paths, integrationCodex, "install")
	if conflict.Status != "manual_configuration_required" {
		t.Fatalf("conflict result=%+v", conflict)
	}
	if _, err := os.Stat(paths.CodexHooks); !os.IsNotExist(err) {
		t.Fatal("Codex conflict created hooks.json")
	}
	if err := os.WriteFile(paths.CodexHooks, []byte(`{"hooks":`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(paths.CodexHooks)
	// Remove the inline conflict so this assertion exercises malformed JSON.
	_ = os.Remove(paths.CodexConfig)
	malformed := runIntegrationAt(paths, integrationCodex, "install")
	if malformed.Status != "invalid_configuration" {
		t.Fatalf("malformed result=%+v", malformed)
	}
	after, _ := os.ReadFile(paths.CodexHooks)
	if string(before) != string(after) {
		t.Fatal("malformed Codex JSON was mutated")
	}
}

func TestCodexUnreadableUserConfigBlocksInstallWithoutWrite(t *testing.T) {
	paths := testProductPaths(t)
	if err := os.MkdirAll(paths.CodexConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	result := runIntegrationAt(paths, integrationCodex, "install")
	if result.OK || result.Status != "integration_unavailable" {
		t.Fatalf("uninspectable config result=%+v", result)
	}
	if _, err := os.Stat(paths.CodexHooks); !os.IsNotExist(err) {
		t.Fatal("uninspectable Codex config allowed hooks.json mutation")
	}
}

func TestCodexInlineHooksBlockInstallButAllowExactRemove(t *testing.T) {
	paths := testProductPaths(t)
	installed := runIntegrationAt(paths, integrationCodex, "install")
	if !installed.OK {
		t.Fatalf("seed install result=%+v", installed)
	}
	if err := os.WriteFile(paths.CodexConfig, []byte("[hooks]\nUserPromptSubmit = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := runIntegrationAt(paths, integrationCodex, "install")
	if blocked.Status != "manual_configuration_required" {
		t.Fatalf("inline install result=%+v", blocked)
	}
	status := runIntegrationAt(paths, integrationCodex, "status")
	if status.Status != "manual_configuration_required" {
		t.Fatalf("inline status result=%+v", status)
	}
	removed := runIntegrationAt(paths, integrationCodex, "remove")
	if !removed.OK || countRequiredOwnedHandlers(integrationDefinition{provider: integrationCodex, path: paths.CodexHooks, command: shellQuote(paths.Binary) + " agent-hook codex", events: codexEvents}, readTestJSON(t, paths.CodexHooks)) != 0 {
		t.Fatalf("inline remove result=%+v", removed)
	}
}

func TestProviderRemoveMalformedJSONDoesNotWrite(t *testing.T) {
	for _, provider := range []string{integrationCodex, integrationClaude} {
		t.Run(provider, func(t *testing.T) {
			paths := testProductPaths(t)
			path := paths.CodexHooks
			if provider == integrationClaude {
				path = paths.ClaudeSettings
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			before := []byte(`{"hooks":{"Stop":[`)
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			result := runIntegrationAt(paths, provider, "remove")
			if result.OK || result.Status != "invalid_configuration" {
				t.Fatalf("remove result=%+v", result)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("malformed provider JSON was rewritten during removal")
			}
		})
	}
}

func TestProviderRemovePreservesNearMatchHandlers(t *testing.T) {
	for _, provider := range []string{integrationCodex, integrationClaude} {
		t.Run(provider, func(t *testing.T) {
			paths := testProductPaths(t)
			spec, ok := integrationSpec(provider, paths)
			if !ok {
				t.Fatal("provider spec unavailable")
			}
			nearMatch := ownedHandler(spec)
			nearMatch["timeout"] = float64(15)
			root := map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{nearMatch}}}}}
			if err := writeProviderJSON(spec.path, root, false); err != nil {
				t.Fatal(err)
			}
			before := string(mustRead(t, spec.path))
			result := runIntegrationAt(paths, provider, "remove")
			if !result.OK {
				t.Fatalf("remove result=%+v", result)
			}
			if after := string(mustRead(t, spec.path)); after != before {
				t.Fatal("near-match user handler was removed or rewritten")
			}
		})
	}
}

func TestCodexMergePreservesUnrelatedConfigurationAndMode(t *testing.T) {
	paths := testProductPaths(t)
	if err := os.MkdirAll(paths.CodexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := `{"theme":"keep","hooks":{"UserPromptSubmit":[{"matcher":"other","hooks":[{"type":"command","command":"other"}]}]}}`
	if err := os.WriteFile(paths.CodexHooks, []byte(seed), 0o640); err != nil {
		t.Fatal(err)
	}
	installed := runIntegrationAt(paths, integrationCodex, "install")
	if !installed.OK {
		t.Fatalf("install result=%+v", installed)
	}
	root := readTestJSON(t, paths.CodexHooks)
	if root["theme"] != "keep" || !strings.Contains(string(mustRead(t, paths.CodexHooks)), `"command": "other"`) {
		t.Fatal("unrelated Codex configuration was not preserved")
	}
	if mode := mustMode(t, paths.CodexHooks); mode != 0o640 {
		t.Fatalf("existing Codex hooks mode=%o", mode)
	}
	removed := runIntegrationAt(paths, integrationCodex, "remove")
	if !removed.OK || !strings.Contains(string(mustRead(t, paths.CodexHooks)), `"command": "other"`) {
		t.Fatalf("exact removal changed an unrelated Codex handler: %+v", removed)
	}
}

func TestIntegrationStatusRequiresEveryOwnedEvent(t *testing.T) {
	paths := testProductPaths(t)
	notConfigured := runIntegrationAt(paths, integrationCodex, "status")
	if notConfigured.Status != "not_configured" {
		t.Fatalf("empty status=%+v", notConfigured)
	}
	installed := runIntegrationAt(paths, integrationCodex, "install")
	if !installed.OK || installed.Status != "configured_requires_trust" {
		t.Fatalf("complete install=%+v", installed)
	}
	root := readTestJSON(t, paths.CodexHooks)
	delete(root["hooks"].(map[string]any), codexEvents[0])
	if err := writeProviderJSON(paths.CodexHooks, root, true); err != nil {
		t.Fatal(err)
	}
	partial := runIntegrationAt(paths, integrationCodex, "status")
	if partial.Status != "repair_required" {
		t.Fatalf("partial status=%+v", partial)
	}
}

func TestCombinedIntegrationStatusReportsAnActualAttentionState(t *testing.T) {
	configuredCodex := okResult("configured_requires_trust", "trust", nil)
	configuredClaude := okResult("configured", "configured", nil)
	notConfiguredCodex := errorResult("not_configured", "missing", nil)
	notConfiguredClaude := errorResult("repair_required", "partial", nil)

	tests := []struct {
		name   string
		codex  operationResult
		claude operationResult
		want   string
	}{
		{name: "Claude needs attention", codex: configuredCodex, claude: notConfiguredClaude, want: "repair_required"},
		{name: "Codex needs attention", codex: notConfiguredCodex, claude: configuredClaude, want: "not_configured"},
		{name: "both configured", codex: configuredCodex, claude: configuredClaude, want: "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := combinedIntegrationsStatus(tt.codex, tt.claude)
			if result.Status != tt.want {
				t.Fatalf("combined result=%+v", result)
			}
		})
	}
}

func TestClaudeExecFormAndDisabledStateArePreserved(t *testing.T) {
	paths := testProductPaths(t)
	if err := os.MkdirAll(paths.ClaudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := `{"disableAllHooks":true,"theme":"keep","hooks":{"Notification":[{"matcher":"*","hooks":[{"type":"command","command":"other"}]}]}}`
	if err := os.WriteFile(paths.ClaudeSettings, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	installed := runIntegrationAt(paths, integrationClaude, "install")
	if !installed.OK || installed.Status != "configured_but_disabled" {
		t.Fatalf("install result=%+v", installed)
	}
	root := readTestJSON(t, paths.ClaudeSettings)
	if root["theme"] != "keep" || !boolValue(root["disableAllHooks"]) {
		t.Fatal("unrelated Claude settings were not preserved")
	}
	groups := root["hooks"].(map[string]any)["UserPromptSubmit"].([]any)
	handler := groups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if handler["command"] != paths.Binary {
		t.Fatalf("Claude command=%v", handler["command"])
	}
	if got := handler["args"].([]any); len(got) != 2 || got[0] != "agent-hook" || got[1] != "claude-code" {
		t.Fatalf("Claude args=%v", got)
	}
	beforeRepair := string(mustRead(t, paths.ClaudeSettings))
	repaired := runIntegrationAt(paths, integrationClaude, "install")
	if !repaired.OK || string(mustRead(t, paths.ClaudeSettings)) != beforeRepair {
		t.Fatalf("idempotent Claude repair result=%+v", repaired)
	}
	configuredStatus := runIntegrationAt(paths, integrationClaude, "status")
	if configuredStatus.OK || configuredStatus.Status != "configured_but_disabled" {
		t.Fatalf("disabled Claude status=%+v", configuredStatus)
	}
	removed := runIntegrationAt(paths, integrationClaude, "remove")
	if !removed.OK || strings.Contains(string(mustRead(t, paths.ClaudeSettings)), paths.Binary) {
		t.Fatalf("remove result=%+v", removed)
	}
	if !strings.Contains(string(mustRead(t, paths.ClaudeSettings)), `"command": "other"`) {
		t.Fatal("unrelated Claude handler was removed")
	}
	status := runIntegrationAt(paths, integrationClaude, "status")
	if status.Status != "not_configured" {
		t.Fatalf("removed Claude status=%+v", status)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
