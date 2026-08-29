// Package navigation contains the private, host-local side of safe display
// navigation. Public displays carry only opaque target IDs; this package
// resolves those IDs against the Mac's internal state before doing anything.
package navigation

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type Action struct {
	ID       string                 `json:"id"`
	TargetID string                 `json:"targetId"`
	TaskID   string                 `json:"taskId,omitempty"`
	Action   state.NavigationAction `json:"action"`
	IssuedAt time.Time              `json:"issuedAt"`
}

type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Resolve checks the private target registry and the target's allow-list. A
// public target ID alone is never sufficient to invoke an OS action.
func Resolve(root state.InternalRootState, action Action) (state.NavigationTarget, error) {
	if action.TargetID == "" {
		return state.NavigationTarget{}, fmt.Errorf("navigation target is missing")
	}
	for _, target := range root.NavigationTargets {
		if target.TargetID != action.TargetID {
			continue
		}
		if !contains(target.AllowedActions, action.Action) {
			return state.NavigationTarget{}, fmt.Errorf("navigation action is not allowed")
		}
		if target.Kind == state.NavigationAgent && !knownProviderApp(target.Detail.Provider, target.Detail.PreferredApp) {
			return state.NavigationTarget{}, fmt.Errorf("provider application is not allow-listed")
		}
		return target, nil
	}
	return state.NavigationTarget{}, fmt.Errorf("navigation target is unavailable")
}

// Execute performs the smallest safe host-local operation: open a validated
// provider-native conversation deep link when one is available, then fall
// back to activating the provider application. Session/window locators remain
// private and are never interpreted from public input.
func Execute(ctx context.Context, root state.InternalRootState, action Action) Result {
	target, err := Resolve(root, action)
	if err != nil {
		return Result{Message: err.Error()}
	}
	if action.Action != state.ActionFocusAgent {
		return Result{Message: "navigation action is not implemented on this host"}
	}
	if runtime.GOOS != "darwin" {
		return Result{Message: "provider application activation requires macOS"}
	}
	app := strings.TrimSpace(target.Detail.PreferredApp)
	if app == "" {
		return Result{Message: "provider application is unavailable"}
	}
	if strings.EqualFold(strings.TrimSpace(target.Detail.Provider), "codex") {
		if deepLink, ok := codexConversationURL(target.Detail.SessionID); ok {
			if err := exec.CommandContext(ctx, "open", deepLink).Run(); err == nil {
				return Result{OK: true, Message: "Codex conversation activated"}
			}
		}
	}
	if err := exec.CommandContext(ctx, "open", "-a", app).Run(); err != nil {
		return Result{Message: "provider application could not be activated"}
	}
	return Result{OK: true, Message: app + " activated"}
}

// codexConversationURL returns the local Codex Desktop deep link for a
// conversation UUID. The ID comes from the private target registry, but it is
// still validated before becoming an OS-level URL argument.
func codexConversationURL(sessionID string) (string, bool) {
	id := strings.TrimSpace(sessionID)
	if len(id) != 36 {
		return "", false
	}
	for index, character := range id {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return "", false
			}
		default:
			if !isHex(character) {
				return "", false
			}
		}
	}
	return "codex://threads/" + id, true
}

func isHex(character rune) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F')
}

func contains(actions []state.NavigationAction, want state.NavigationAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func knownProviderApp(provider, app string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return strings.EqualFold(strings.TrimSpace(app), "Codex")
	case "claude-code", "claude":
		return strings.EqualFold(strings.TrimSpace(app), "Claude")
	default:
		return false
	}
}
