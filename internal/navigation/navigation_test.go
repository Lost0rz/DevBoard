package navigation

import (
	"context"
	"testing"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestResolveRequiresPrivateTargetAllowList(t *testing.T) {
	root := state.InternalRootState{
		Host: state.HostState{ID: "mac-a"},
		NavigationTargets: []state.NavigationTarget{{
			TargetID: "opaque-agent", Kind: state.NavigationAgent, HostID: "mac-a",
			AllowedActions: []state.NavigationAction{state.ActionFocusAgent},
			Detail:         state.NavigationTargetDetail{AgentID: "codex:s", Provider: "codex", SessionID: "s", PreferredApp: "Codex"},
		}},
	}
	if _, err := Resolve(root, Action{TargetID: "opaque-agent", Action: state.ActionFocusAgent}); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	if _, err := Resolve(root, Action{TargetID: "opaque-agent", Action: state.ActionOpenProject}); err == nil {
		t.Fatal("disallowed action accepted")
	}
	if result := Execute(context.Background(), root, Action{TargetID: "missing", Action: state.ActionFocusAgent}); result.OK {
		t.Fatal("missing target executed")
	}
}

func TestCodexConversationURLValidatesConversationID(t *testing.T) {
	valid := "019c3526-0054-7052-a09b-281d913fd8f7"
	if got, ok := codexConversationURL(valid); !ok || got != "codex://threads/"+valid {
		t.Fatalf("codexConversationURL(%q)=(%q, %v)", valid, got, ok)
	}
	if got, ok := codexConversationURL("  " + valid + "  "); !ok || got != "codex://threads/"+valid {
		t.Fatalf("trimmed codexConversationURL(%q)=(%q, %v)", valid, got, ok)
	}
	for _, invalid := range []string{
		"session-mock-001",
		"codex://evil",
		"019c3526-0054-7052-a09b-281d913fd8fZ",
		"019c35260054-7052-a09b-281d913fd8f7",
	} {
		if got, ok := codexConversationURL(invalid); ok || got != "" {
			t.Fatalf("codexConversationURL(%q)=(%q, %v), want rejected", invalid, got, ok)
		}
	}
}
