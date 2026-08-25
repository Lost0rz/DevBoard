package web

import (
	"encoding/json"
	"github.com/Lost0rz/DevBoard/internal/agent"
	"github.com/Lost0rz/DevBoard/internal/state"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProviderSensitiveDataNeverReachesStateOrRendering(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "h", DisplayName: "H"}))
	r := agent.NewReducer(st, agent.ReducerConfig{CompleteRetention: 30 * time.Minute})
	begin, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"UserPromptSubmit","prompt":"PRIVATE_PROMPT_SENTINEL","transcript_path":"PRIVATE_TRANSCRIPT_SENTINEL"}`), now, "evt1")
	if err != nil || !ok {
		t.Fatalf("begin normalize %v %v", ok, err)
	}
	_ = r.Submit(begin)
	stop, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"Stop","background_tasks":[{"description":"PRIVATE_BACKGROUND_DESCRIPTION_SENTINEL","command":"PRIVATE_BACKGROUND_COMMAND_SENTINEL"}],"session_crons":[{"prompt":"PRIVATE_CRON_PROMPT_SENTINEL"}],"last_assistant_message":"PRIVATE_STOP_ASSISTANT_SENTINEL"}`), now.Add(500*time.Millisecond), "evt-stop")
	if err != nil || !ok {
		t.Fatalf("stop normalize %v %v", ok, err)
	}
	_ = r.Submit(stop)
	fail, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"StopFailure","error":"rate_limit","last_assistant_message":"PRIVATE_ASSISTANT_SENTINEL","tool_input":{"command":"PRIVATE_COMMAND_SENTINEL","x":"PRIVATE_TOOL_INPUT_SENTINEL"},"tool_response":"PRIVATE_TOOL_RESPONSE_SENTINEL","message":"PRIVATE_NOTIFICATION_SENTINEL","error_details":"PRIVATE_ERROR_DETAILS_SENTINEL"}`), now.Add(time.Second), "evt2")
	if err != nil || !ok {
		t.Fatalf("fail normalize %v %v", ok, err)
	}
	_ = r.Submit(fail)
	rawState, _ := json.Marshal(st.Snapshot())
	for _, s := range []string{"PRIVATE_PROMPT_SENTINEL", "PRIVATE_TRANSCRIPT_SENTINEL", "PRIVATE_ASSISTANT_SENTINEL", "PRIVATE_COMMAND_SENTINEL", "PRIVATE_TOOL_INPUT_SENTINEL", "PRIVATE_TOOL_RESPONSE_SENTINEL", "PRIVATE_NOTIFICATION_SENTINEL", "PRIVATE_ERROR_DETAILS_SENTINEL", "PRIVATE_BACKGROUND_DESCRIPTION_SENTINEL", "PRIVATE_BACKGROUND_COMMAND_SENTINEL", "PRIVATE_CRON_PROMPT_SENTINEL", "PRIVATE_STOP_ASSISTANT_SENTINEL"} {
		if strings.Contains(string(rawState), s) {
			t.Fatalf("internal state leaked %s", s)
		}
	}
	srv, err := NewServer(st, state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.now = func() time.Time { return now.Add(2 * time.Second) }
	for _, path := range []string{"/api/state", "/display", "/kindle/R"} {
		w := request(t, srv, http.MethodGet, path)
		body := w.Body.String()
		for _, s := range []string{"PRIVATE_PROMPT_SENTINEL", "PRIVATE_TRANSCRIPT_SENTINEL", "PRIVATE_ASSISTANT_SENTINEL", "PRIVATE_COMMAND_SENTINEL", "PRIVATE_TOOL_INPUT_SENTINEL", "PRIVATE_TOOL_RESPONSE_SENTINEL", "PRIVATE_NOTIFICATION_SENTINEL", "PRIVATE_ERROR_DETAILS_SENTINEL", "PRIVATE_BACKGROUND_DESCRIPTION_SENTINEL", "PRIVATE_BACKGROUND_COMMAND_SENTINEL", "PRIVATE_CRON_PROMPT_SENTINEL", "PRIVATE_STOP_ASSISTANT_SENTINEL"} {
			if strings.Contains(body, s) {
				t.Fatalf("%s leaked %s", path, s)
			}
		}
	}
}
