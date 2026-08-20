package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agent"
	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestM21UnsafeMetadataNeverReachesStateOrRendering(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 30, 0, 0, time.UTC)
	st := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "h", DisplayName: "H"}))
	r := agent.NewReducer(st, agent.ReducerConfig{CompleteRetention: 30 * time.Minute})

	begin, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"UserPromptSubmit"}`), now, "evt-begin")
	if err != nil || !ok {
		t.Fatalf("begin normalization: ok=%v err=%v", ok, err)
	}
	if err := r.Submit(begin); err != nil {
		t.Fatal(err)
	}

	unsafeFail, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"StopFailure","error":"PRIVATE_ERROR_TYPE_SENTINEL"}`), now.Add(time.Second), "evt-unsafe-error")
	if err != nil || !ok || unsafeFail.Metadata.ErrorType == nil || *unsafeFail.Metadata.ErrorType != "unknown" {
		t.Fatalf("unsafe error normalization: event=%+v ok=%v err=%v", unsafeFail, ok, err)
	}
	if err := r.Submit(unsafeFail); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := agent.Normalize(agent.ProviderClaude, []byte(`{"session_id":"s","prompt_id":"p","hook_event_name":"Notification","notification_type":"PRIVATE_NOTIFICATION_TYPE_SENTINEL"}`), now.Add(2*time.Second), "evt-unsafe-notification"); err != nil || ok {
		t.Fatalf("unsafe notification should be ignored: ok=%v err=%v", ok, err)
	}

	sentinels := []string{"PRIVATE_ERROR_TYPE_SENTINEL", "PRIVATE_NOTIFICATION_TYPE_SENTINEL"}
	raw, _ := json.Marshal(st.Snapshot())
	for _, sentinel := range sentinels {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("internal state leaked %s", sentinel)
		}
	}

	srv, err := NewServer(st, state.ProjectionConfig{KindleRefreshSeconds: 20, CompleteHighVisibilitySeconds: 600, CompleteRetentionSeconds: 1800}, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.now = func() time.Time { return now.Add(3 * time.Second) }
	for _, path := range []string{"/api/state", "/display", "/display/kindle"} {
		w := request(t, srv, http.MethodGet, path)
		for _, sentinel := range sentinels {
			if strings.Contains(w.Body.String(), sentinel) {
				t.Fatalf("%s leaked %s", path, sentinel)
			}
		}
	}
}
