package agentquota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

func TestDisabledRuntimeIsInert(t *testing.T) {
	r := Start(context.Background(), config.AgentQuotaConfig{Provider: "glm"}, filepath.Join(t.TempDir(), "key"), nil)
	defer r.Close()
	health := r.Health()
	if health.Enabled || health.State != "disabled" {
		t.Fatalf("disabled health=%+v", health)
	}
}

func TestRuntimeCloseWaitsForInFlightCycle(t *testing.T) {
	r := &Runtime{done: make(chan struct{})}
	close(r.done)
	release := make(chan struct{})
	r.cycleWG.Add(1)
	go func() {
		defer r.cycleWG.Done()
		<-release
	}()

	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Runtime.Close returned before the in-flight cycle finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Close did not return after the in-flight cycle finished")
	}
}

func TestNextScheduledAnchorUsesExplicitDailyTimes(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	now := time.Date(2026, time.January, 2, 11, 1, 0, 0, loc)
	got := nextScheduledAnchor(now, []string{"05:00", "15:00", "10:00"})
	want := time.Date(2026, time.January, 2, 15, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("next anchor=%s want=%s", got, want)
	}
	now = time.Date(2026, time.January, 2, 23, 1, 0, 0, loc)
	got = nextScheduledAnchor(now, []string{"05:00", "15:00"})
	want = time.Date(2026, time.January, 3, 5, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("next overnight anchor=%s want=%s", got, want)
	}
}

func TestTickRecordsMissedAndBusySlotsOnce(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	var missed []Event
	missedRuntime := &Runtime{
		cfg:             config.AgentQuotaConfig{Schedules: []string{"10:00"}},
		loc:             loc,
		firedAnchors:    make(map[string]struct{}),
		reportedAnchors: make(map[string]struct{}),
		sink:            func(event Event) { missed = append(missed, event) },
	}
	now := time.Date(2026, 8, 29, 10, 2, 1, 0, loc)
	missedRuntime.tick(context.Background(), now)
	missedRuntime.tick(context.Background(), now.Add(5*time.Second))
	if len(missed) != 1 || missed[0].Code != "activation_skipped" || missed[0].Reason != "missed_trigger_grace" {
		t.Fatalf("missed events=%+v", missed)
	}

	var busy []Event
	busyRuntime := &Runtime{
		cfg:             config.AgentQuotaConfig{Schedules: []string{"10:00"}},
		loc:             loc,
		activeCycle:     true,
		activeAnchor:    "other",
		firedAnchors:    make(map[string]struct{}),
		reportedAnchors: make(map[string]struct{}),
		sink:            func(event Event) { busy = append(busy, event) },
	}
	busyRuntime.tick(context.Background(), time.Date(2026, 8, 29, 10, 1, 0, 0, loc))
	busyRuntime.tick(context.Background(), time.Date(2026, 8, 29, 10, 1, 5, 0, loc))
	if len(busy) != 1 || busy[0].Code != "activation_deferred" || busy[0].Reason != "cycle_busy" {
		t.Fatalf("busy events=%+v", busy)
	}
}

func TestActivationEndpointNormalizesSupportedBaseURLs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		anthropic bool
	}{
		{name: "anthropic base", input: "https://open.bigmodel.cn/api/anthropic", want: "https://open.bigmodel.cn/api/anthropic/v1/messages", anthropic: true},
		{name: "openai coding base", input: "https://open.bigmodel.cn/api/coding/paas/v4", want: "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"},
		{name: "complete endpoint", input: "https://open.bigmodel.cn/api/paas/v4/chat/completions", want: "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, anthropic, err := activationEndpoint(test.input)
			if err != nil || got != test.want || anthropic != test.anthropic {
				t.Fatalf("activationEndpoint(%q)=(%q,%v,%v), want (%q,%v,nil)", test.input, got, anthropic, err, test.want, test.anthropic)
			}
		})
	}
}

func TestSaveAPIKeyIsPrivateAndReadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota.key")
	if err := SaveAPIKey(path, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%o want 600", info.Mode().Perm())
	}
	got, err := readAPIKey(path)
	if err != nil || got != "glm-test-key" {
		t.Fatalf("read key=%q err=%v", got, err)
	}
}

func TestActivateVerifiesGLMResponseWithoutLoggingSecret(t *testing.T) {
	var gotAuthorization string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	dir := filepath.Dir(keyPath)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-secret"); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{cfg: config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"}}, key: keyPath, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r.activate(context.Background(), time.Now())
	health := r.Health()
	if health.State != "healthy" || health.LastSuccessAt == nil || !strings.Contains(health.Message, "response received") {
		t.Fatalf("activation health=%+v", health)
	}
	if gotAuthorization != "Bearer glm-secret" {
		t.Fatalf("authorization=%q", gotAuthorization)
	}
	if gotBody["model"] != "glm-test" || gotBody["max_tokens"] != float64(activationMaxTokens) {
		t.Fatalf("request body=%v", gotBody)
	}
	messages, ok := gotBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%v", gotBody["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" || message["content"] != "Reply exactly with OK" {
		t.Fatalf("minimal message=%v", messages[0])
	}
	if strings.Contains(health.Message, "glm-secret") {
		t.Fatal("health message leaked API key")
	}
}

func TestActivateRejectsHTTP200WithoutModelContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A gateway can report usage even when it did not return model text.
		// That is reachable, but it is not a successful dialogue response.
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":""}}],"usage":{"total_tokens":8}}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	var events []Event
	r := &Runtime{
		cfg:  config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"}},
		key:  keyPath,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		sink: func(event Event) { events = append(events, event) },
	}
	r.activate(context.Background(), time.Now())
	if r.Health().State != "error" || len(events) != 2 || events[1].Reason != "response_unverified" {
		t.Fatalf("health=%+v events=%+v", r.Health(), events)
	}
}

func TestActivateClassifiesHTTP200ProviderQuotaError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":1316,"message":"5-hour quota exhausted"}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	var events []Event
	r := &Runtime{
		cfg:  config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"}},
		key:  keyPath,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		sink: func(event Event) { events = append(events, event) },
	}
	r.activate(context.Background(), time.Now())
	if r.Health().State != "waiting" || len(events) != 2 || events[1].Reason != "provider_error" {
		t.Fatalf("health=%+v events=%+v", r.Health(), events)
	}
}

func TestActivateEmitsAttemptAndFailureReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	events := make([]Event, 0, 2)
	r := &Runtime{
		cfg:  config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"}},
		key:  keyPath,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		sink: func(event Event) { events = append(events, event) },
	}
	r.activate(context.Background(), time.Now())
	if len(events) != 2 || events[0].Code != "activation_attempt" || events[1].Code != "activation_failed" {
		t.Fatalf("events=%+v", events)
	}
	if events[1].Reason != "http_status" || events[1].HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("failure event=%+v", events[1])
	}
}

func TestAnthropicBaseURLUsesMessagesPathAndHeaders(t *testing.T) {
	var gotPath, gotKey, gotAuth, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"OK"}]}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-anthropic-key"); err != nil {
		t.Fatal(err)
	}
	endpoint := server.URL + "/api/anthropic"
	r := TestActivation(context.Background(), config.AgentQuotaConfig{
		Enabled: true, Provider: "glm", Endpoint: endpoint, Model: "glm-5.2", Schedules: []string{"05:00"},
	}, keyPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if r.State != "healthy" {
		t.Fatalf("health=%+v", r)
	}
	if gotPath != "/api/anthropic/v1/messages" || gotKey != "glm-anthropic-key" || gotAuth != "" || gotVersion != "2023-06-01" {
		t.Fatalf("anthropic request path=%q key=%q auth=%q version=%q", gotPath, gotKey, gotAuth, gotVersion)
	}
}

func TestAnthropicResponseAcceptsStringOrBlockContent(t *testing.T) {
	tests := []string{
		`{"content":"OK"}`,
		`{"content":[{"type":"text","text":"OK"}]}`,
		`{"content":[{"type":"text","text":{"content":"OK"}}]}`,
		`{"content":[{"type":"thinking","text":"hidden reasoning"}]}`,
		`{"choices":[{"message":{"content":"OK"}}]}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			ok, reason := verifyModelResponse([]byte(body), true)
			if strings.Contains(body, `"thinking"`) {
				if ok || reason != "response_unverified" {
					t.Fatalf("verifyModelResponse(%s)=(%v,%q), hidden thinking must not verify", body, ok, reason)
				}
				return
			}
			if !ok || reason != "" {
				t.Fatalf("verifyModelResponse(%s)=(%v,%q)", body, ok, reason)
			}
		})
	}
}

func TestAgentQuotaManualTestEmitsManualTrigger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	var events []Event
	TestActivation(context.Background(), config.AgentQuotaConfig{
		Enabled: true, Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"},
	}, keyPath, slog.New(slog.NewTextHandler(io.Discard, nil)), func(event Event) { events = append(events, event) })
	if len(events) != 2 || events[0].Reason != "manual_test" || events[0].Trigger != "manual" || events[1].Reason != "manual_test_verified" {
		t.Fatalf("events=%+v", events)
	}
}

func TestScheduledCycleStopsAfterFirstVerifiedResponse(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{
		cfg: config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00", "10:00"}},
		key: keyPath,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loc: time.Local,
	}
	anchor := time.Date(2026, time.January, 2, 10, 0, 0, 0, time.Local)
	r.runScheduledCycle(context.Background(), anchor, "boundary-test")
	if requests != 1 {
		t.Fatalf("verified scheduled activation requests=%d want 1", requests)
	}
}

func TestScheduledCycleRetriesQuotaResetThenVerifiesModel(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			reset := time.Now().Add(100 * time.Millisecond).UnixMilli()
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintf(w, `{"code":1316,"message":"5-hour quota exhausted","next_flush_time":%d}`, reset)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}],"usage":{"total_tokens":5}}`)
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.Chmod(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey(keyPath, "glm-test-key"); err != nil {
		t.Fatal(err)
	}
	var events []Event
	r := &Runtime{
		cfg:          config.AgentQuotaConfig{Provider: "glm", Endpoint: server.URL, Model: "glm-test", Schedules: []string{"05:00"}},
		key:          keyPath,
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		sink:         func(event Event) { events = append(events, event) },
		loc:          time.Local,
		firedAnchors: make(map[string]struct{}),
	}
	r.runScheduledCycle(context.Background(), time.Now(), "test-anchor")
	if requests != 2 {
		t.Fatalf("requests=%d want 2 events=%+v", requests, events)
	}
	if len(events) != 5 || events[1].Reason != "provider_error" || events[1].ProviderCode != "1316" || events[2].Code != "activation_retry_scheduled" || events[4].Reason != "response_verified" {
		t.Fatalf("events=%+v", events)
	}
}
