package web

import (
	"os"
	"strings"
	"testing"
	"text/template"
)

func TestM4DesktopTemplateIsTaskOrientedAndHidesOpaqueLegacyIDs(t *testing.T) {
	b, err := os.ReadFile("templates/display.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if _, err := template.New("display").Parse(text); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"AI TASKS", "ProviderProject", "task-title", "CHECKPOINT", "ACTION REQUIRED", "COMPLETION"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"{{.ID}}", "{{.AgentID}}", "{{.TurnID}}"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("opaque legacy id rendering remains: %s", forbidden)
		}
	}
}
