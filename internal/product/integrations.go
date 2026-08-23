package product

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	integrationCodex  = "codex"
	integrationClaude = "claude-code"
)

var inlineCodexHooks = regexp.MustCompile(`(?m)^\s*\[hooks\]\s*(?:#.*)?$`)

var codexEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"Stop",
	"SessionEnd",
}

var claudeEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"PostToolUseFailure",
	"PermissionDenied",
	"Notification",
	"Stop",
	"StopFailure",
	"SessionEnd",
	"Elicitation",
	"ElicitationResult",
}

func runIntegration(provider, action string) operationResult {
	paths, err := ResolvePaths("")
	if err != nil {
		return errorResult("integration_unavailable", "provider integration paths are unavailable", nil)
	}
	return runIntegrationAt(paths, provider, action)
}

func RunIntegration(provider, action string) Result {
	return resultValue(runIntegration(provider, action))
}

// RunIntegrationsStatus returns one bounded status object for both supported
// providers. Individual provider status is kept as an internal convenience
// for the macOS shell, while the public CLI's status command remains exactly
// `devboard product integrations status`.
func RunIntegrationsStatus() Result {
	paths, err := ResolvePaths("")
	if err != nil {
		return resultValue(errorResult("integration_unavailable", "provider integration paths are unavailable", nil))
	}
	codex := runIntegrationAt(paths, integrationCodex, "status")
	claude := runIntegrationAt(paths, integrationClaude, "status")
	data := map[string]any{
		"codex":       resultValue(codex),
		"claude-code": resultValue(claude),
	}
	if !codex.OK || !claude.OK {
		status := codex.Status
		if status == "not_configured" || status == "" {
			status = claude.Status
		}
		return Result{SchemaVersion: 1, OK: false, Status: status, Message: "one or more provider integration statuses require attention", Data: data}
	}
	return Result{SchemaVersion: 1, OK: true, Status: "ok", Message: "provider integration status available", Data: data}
}

func runIntegrationAt(paths Paths, provider, action string) operationResult {
	spec, ok := integrationSpec(provider, paths)
	if !ok {
		return errorResult("invalid_command", "unsupported provider integration", nil)
	}
	if provider == integrationCodex && action == "install" && codexInlineHooks(paths.CodexConfig) {
		return errorResult("manual_configuration_required", "Codex uses inline user hooks in config.toml; review that configuration manually", nil)
	}
	if action == "install" && !stableBinaryExists(paths) {
		return errorResult("stable_binary_missing", "install the background Node before installing provider integrations", nil)
	}

	root, exists, err := loadProviderJSON(spec.path)
	if err != nil {
		return errorResult("invalid_configuration", "provider configuration is malformed or incompatible; no file was changed", nil)
	}
	if action == "status" {
		return integrationStatus(spec, root, exists)
	}
	if action == "remove" {
		removed, next, err := removeOwnedHandlers(spec, root)
		if err != nil {
			return errorResult("invalid_configuration", "provider configuration is malformed or incompatible; no file was changed", nil)
		}
		if removed {
			if err := writeProviderJSON(spec.path, next, exists); err != nil {
				return errorResult("write_failed", "provider configuration could not be updated", nil)
			}
		}
		return okResult("removed", "DevBoard provider handlers removed", map[string]any{"provider": provider, "removed": removed})
	}
	if action != "install" {
		return errorResult("invalid_command", "unsupported provider integration action", nil)
	}
	changed, next, err := installOwnedHandlers(spec, root)
	if err != nil {
		return errorResult("invalid_configuration", "provider configuration is malformed or incompatible; no file was changed", nil)
	}
	if changed || !exists {
		if err := writeProviderJSON(spec.path, next, exists); err != nil {
			return errorResult("write_failed", "provider configuration could not be updated", nil)
		}
	}
	status := "configured"
	message := "DevBoard provider handlers installed"
	if provider == integrationCodex {
		status = "configured_requires_trust"
		message = "Configuration installed. Review and trust the DevBoard hook in Codex /hooks."
	}
	if provider == integrationClaude && boolValue(next["disableAllHooks"]) {
		status = "configured_but_disabled"
		message = "Configuration installed, but Claude Code disableAllHooks=true was preserved."
	}
	return okResult(status, message, map[string]any{"provider": provider, "changed": changed})
}

type integrationDefinition struct {
	provider string
	path     string
	events   []string
	command  string
	args     []string
}

func integrationSpec(provider string, paths Paths) (integrationDefinition, bool) {
	switch provider {
	case integrationCodex:
		return integrationDefinition{provider: provider, path: paths.CodexHooks, events: codexEvents, command: shellQuote(paths.Binary) + " agent-hook codex"}, true
	case integrationClaude:
		return integrationDefinition{provider: provider, path: paths.ClaudeSettings, events: claudeEvents, command: paths.Binary, args: []string{"agent-hook", "claude-code"}}, true
	default:
		return integrationDefinition{}, false
	}
}

func codexInlineHooks(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return inlineCodexHooks.Match(body)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func loadProviderJSON(path string) (map[string]any, bool, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{"hooks": map[string]any{}}, false, nil
	}
	if err != nil || len(body) > 1<<20 {
		return nil, false, fmt.Errorf("read provider configuration")
	}
	root, err := validateJSONShape(body)
	if err != nil {
		return nil, false, err
	}
	if hooks, ok := root["hooks"]; ok {
		if _, ok := hooks.(map[string]any); !ok {
			return nil, false, fmt.Errorf("hooks is not an object")
		}
	} else {
		root["hooks"] = map[string]any{}
	}
	if disabled, ok := root["disableAllHooks"]; ok {
		if _, ok := disabled.(bool); !ok {
			return nil, false, fmt.Errorf("disableAllHooks is not boolean")
		}
	}
	if err := validateHookGroups(root); err != nil {
		return nil, false, err
	}
	return root, true, nil
}

func validateHookGroups(root map[string]any) error {
	hooks := root["hooks"].(map[string]any)
	for _, rawGroups := range hooks {
		groups, ok := rawGroups.([]any)
		if !ok {
			return fmt.Errorf("event hooks is not an array")
		}
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				return fmt.Errorf("hook group is not an object")
			}
			rawHandlers, ok := group["hooks"]
			if !ok {
				return fmt.Errorf("hook group has no handlers")
			}
			handlers, ok := rawHandlers.([]any)
			if !ok {
				return fmt.Errorf("hook handlers is not an array")
			}
			for _, rawHandler := range handlers {
				if _, ok := rawHandler.(map[string]any); !ok {
					return fmt.Errorf("hook handler is not an object")
				}
			}
		}
	}
	return nil
}

func integrationStatus(spec integrationDefinition, root map[string]any, exists bool) operationResult {
	owned := countRequiredOwnedHandlers(spec, root)
	if !exists || owned == 0 {
		return errorResult("not_configured", "DevBoard provider handlers are not configured", map[string]any{"provider": spec.provider, "configured": false})
	}
	if owned < len(spec.events) {
		return errorResult("repair_required", "DevBoard provider handlers are only partially configured", map[string]any{"provider": spec.provider, "configured": false})
	}
	if spec.provider == integrationCodex {
		return okResult("configured_requires_trust", "Configuration installed. Review and trust the DevBoard hook in Codex /hooks.", map[string]any{"provider": spec.provider, "configured": true})
	}
	if boolValue(root["disableAllHooks"]) {
		return errorResult("configured_but_disabled", "DevBoard handlers are configured but Claude Code disableAllHooks=true is preserved.", map[string]any{"provider": spec.provider, "configured": true})
	}
	return okResult("configured", "DevBoard provider handlers are configured", map[string]any{"provider": spec.provider, "configured": true})
}

func installOwnedHandlers(spec integrationDefinition, root map[string]any) (bool, map[string]any, error) {
	hooks := root["hooks"].(map[string]any)
	changed := false
	for _, event := range spec.events {
		rawGroups, exists := hooks[event]
		var groups []any
		if exists {
			var ok bool
			groups, ok = rawGroups.([]any)
			if !ok {
				return false, nil, fmt.Errorf("event groups are incompatible")
			}
		}
		if !eventHasOwnedHandler(spec, groups) {
			groups = append(groups, map[string]any{"hooks": []any{ownedHandler(spec)}})
			hooks[event] = groups
			changed = true
		}
	}
	return changed, root, nil
}

func removeOwnedHandlers(spec integrationDefinition, root map[string]any) (bool, map[string]any, error) {
	hooks := root["hooks"].(map[string]any)
	changed := false
	for event, rawGroups := range hooks {
		groups, ok := rawGroups.([]any)
		if !ok {
			return false, nil, fmt.Errorf("event groups are incompatible")
		}
		nextGroups := make([]any, 0, len(groups))
		eventChanged := false
		for _, rawGroup := range groups {
			group := rawGroup.(map[string]any)
			handlers := group["hooks"].([]any)
			nextHandlers := make([]any, 0, len(handlers))
			for _, rawHandler := range handlers {
				if isOwnedHandler(spec, rawHandler.(map[string]any)) {
					changed = true
					eventChanged = true
					continue
				}
				nextHandlers = append(nextHandlers, rawHandler)
			}
			if len(nextHandlers) == 0 && len(handlers) > 0 {
				if len(group) == 1 {
					continue
				}
				group["hooks"] = nextHandlers
			}
			group["hooks"] = nextHandlers
			nextGroups = append(nextGroups, group)
		}
		if eventChanged {
			hooks[event] = nextGroups
		}
	}
	return changed, root, nil
}

func countOwnedHandlers(spec integrationDefinition, root map[string]any) int {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	count := 0
	for _, rawGroups := range hooks {
		groups, ok := rawGroups.([]any)
		if !ok {
			continue
		}
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				continue
			}
			handlers, ok := group["hooks"].([]any)
			if !ok {
				continue
			}
			for _, rawHandler := range handlers {
				if handler, ok := rawHandler.(map[string]any); ok && isOwnedHandler(spec, handler) {
					count++
				}
			}
		}
	}
	return count
}

func countRequiredOwnedHandlers(spec integrationDefinition, root map[string]any) int {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	count := 0
	for _, event := range spec.events {
		groups, ok := hooks[event].([]any)
		if ok && eventHasOwnedHandler(spec, groups) {
			count++
		}
	}
	return count
}

func eventHasOwnedHandler(spec integrationDefinition, groups []any) bool {
	for _, rawGroup := range groups {
		group := rawGroup.(map[string]any)
		handlers := group["hooks"].([]any)
		for _, rawHandler := range handlers {
			if isOwnedHandler(spec, rawHandler.(map[string]any)) {
				return true
			}
		}
	}
	return false
}

func ownedHandler(spec integrationDefinition) map[string]any {
	handler := map[string]any{"type": "command", "command": spec.command}
	if len(spec.args) > 0 {
		args := make([]any, len(spec.args))
		for i, arg := range spec.args {
			args[i] = arg
		}
		handler["args"] = args
	}
	return handler
}

func isOwnedHandler(spec integrationDefinition, handler map[string]any) bool {
	if handler["type"] != "command" || handler["command"] != spec.command {
		return false
	}
	if len(spec.args) == 0 {
		return true
	}
	rawArgs, ok := handler["args"].([]any)
	if !ok || len(rawArgs) != len(spec.args) {
		return false
	}
	for i, arg := range spec.args {
		if rawArgs[i] != arg {
			return false
		}
	}
	return true
}

func writeProviderJSON(path string, root map[string]any, existing bool) error {
	body, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	mode := os.FileMode(0o600)
	if existing {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, body, mode)
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}
