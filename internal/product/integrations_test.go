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
	removed := runIntegrationAt(paths, integrationClaude, "remove")
	if !removed.OK || strings.Contains(string(mustRead(t, paths.ClaudeSettings)), paths.Binary) {
		t.Fatalf("remove result=%+v", removed)
	}
	if !strings.Contains(string(mustRead(t, paths.ClaudeSettings)), `"command": "other"`) {
		t.Fatal("unrelated Claude handler was removed")
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
