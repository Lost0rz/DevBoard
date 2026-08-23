package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestM4TaskTitleBoundsAndRejectsUnsafeInput(t *testing.T) {
	natural := "实现 M4 的任务观察能力。后面这一句不应该进入标题。"
	title := deriveTaskTitle(natural)
	if title == nil || *title != "实现 M4 的任务观察能力。" || len(*title) > maxTaskTitleBytes || !utf8.ValidString(*title) {
		t.Fatalf("title=%v", title)
	}
	multi := deriveTaskTitle("\n\n# Audit the task reducer\nmore")
	if multi == nil || !strings.Contains(*multi, "Audit the task reducer") {
		t.Fatalf("multiline title=%v", multi)
	}
	long := deriveTaskTitle(strings.Repeat("任务", 100))
	if long == nil || len(*long) > maxTaskTitleBytes || !utf8.ValidString(*long) {
		t.Fatalf("long title invalid: %v", long)
	}

	unsafe := []string{
		"package main\nfunc main() {}",
		"go test ./...",
		"Please inspect /tmp/private/repo/file.go",
		`{"token":"value","items":[1,2,3]}`,
		"ERROR one\nWARN two\nINFO three",
		"api_key=SUPER_PRIVATE_VALUE",
		"https://user:password@example.com/path",
		"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc",
		// Identifier/token-shaped prompts are not natural language and must
		// never be republished as the derived title (privacy sentinel case).
		"PRIVATE_PROMPT_SENTINEL",
		"snake_case_identifier_prompt",
	}
	for _, raw := range unsafe {
		if got := deriveTaskTitle(raw); got != nil {
			t.Fatalf("unsafe title accepted %q => %q", raw, *got)
		}
	}
}

func TestM4RawPromptNeverEntersNormalizedEvent(t *testing.T) {
	rawSentinel := "RAW_PROMPT_SENTINEL_812739"
	raw := `{"session_id":"s","turn_id":"t","hook_event_name":"UserPromptSubmit","cwd":"","prompt":"Please implement the task board\n` + rawSentinel + `"}`
	e, ok, err := Normalize(ProviderCodex, []byte(raw), time.Unix(10, 0).UTC(), "event-1")
	if err != nil || !ok {
		t.Fatalf("normalize ok=%v err=%v", ok, err)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), rawSentinel) {
		t.Fatalf("raw prompt leaked into normalized event: %s", b)
	}
}
