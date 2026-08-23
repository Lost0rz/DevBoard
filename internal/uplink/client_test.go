package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const m54Token = "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func m54Envelope() NodeSnapshot {
	pub := state.PublicState{SchemaVersion: 1, StateKind: "public", Host: state.PublicHost{ID: "mac-a", DisplayName: "Mac A"}, GeneratedAt: m54Base, Sources: map[string]state.PublicSourceHealth{}}
	return NodeSnapshot{SchemaVersion: 1, StateKind: "nodeSnapshot", NodeID: "mac-a", SessionID: "aabbccddeeff00112233445566778899", Sequence: 1, SentAt: m54Base, State: pub}
}

type recordingHandler struct {
	mu       sync.Mutex
	statuses []int
	requests []recordedRequest
	block    chan struct{} // non-nil: hold each request until closed
}

type recordedRequest struct {
	at          time.Time
	path        string
	rawQuery    string
	method      string
	auth        string
	contentType string
	body        []byte
}

func (h *recordingHandler) setStatus(statuses ...int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses = statuses
}

func (h *recordingHandler) recorded() []recordedRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]recordedRequest(nil), h.requests...)
}

func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxSnapshotBodyBytes+1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	idx := len(h.requests)
	h.requests = append(h.requests, recordedRequest{at: time.Now(), path: r.URL.Path, rawQuery: r.URL.RawQuery, method: r.Method, auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: body})
	statuses := h.statuses
	block := h.block
	h.mu.Unlock()
	if block != nil {
		<-block
	}
	status := http.StatusOK
	if idx < len(statuses) {
		status = statuses[idx]
	}
	w.WriteHeader(status)
}

// httptestServer starts a server for the handler and closes it with the test.
func httptestServer(t *testing.T, h http.Handler) string {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return server.URL
}

// TestM54ClientClassifiesHubResponses freezes the HTTP behavior matrix.
func TestM54ClientClassifiesHubResponses(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{http.StatusOK, ""},
		{http.StatusBadRequest, ErrPayload},
		{http.StatusUnauthorized, ErrAuth},
		{http.StatusForbidden, ErrAuth},
		{http.StatusConflict, ErrConflict},
		{http.StatusRequestEntityTooLarge, ErrPayload},
		{http.StatusUnsupportedMediaType, ErrPayload},
		{http.StatusInternalServerError, ErrTransient},
		{http.StatusBadGateway, ErrTransient},
		{http.StatusServiceUnavailable, ErrTransient},
		{http.StatusNotFound, ErrPayload},
		{http.StatusMovedPermanently, ErrPayload},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			handler := &recordingHandler{}
			handler.setStatus(tc.status)
			server := httptest.NewServer(handler)
			defer server.Close()
			client := NewClient(server.URL, m54Token, 500*time.Millisecond)
			err := client.Send(context.Background(), m54Envelope())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("200 must succeed, got %v", err)
				}
				return
			}
			sendErr, ok := err.(*SendError)
			if !ok {
				t.Fatalf("expected *SendError, got %T %v", err, err)
			}
			if sendErr.Kind != tc.want {
				t.Fatalf("status %d classified as %s, want %s", tc.status, sendErr.Kind, tc.want)
			}
			if sendErr.Status != tc.status {
				t.Fatalf("classification lost status %d", sendErr.Status)
			}
		})
	}
}

func TestM54ClientSendsFrozenRequestShape(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(server.URL+"/", m54Token, 500*time.Millisecond)
	if err := client.Send(context.Background(), m54Envelope()); err != nil {
		t.Fatalf("send: %v", err)
	}
	reqs := handler.recorded()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.method != http.MethodPost || req.path != SnapshotPath || req.rawQuery != "" {
		t.Fatalf("request must be POST %s without query, got %s %s?q=%s", SnapshotPath, req.method, req.path, req.rawQuery)
	}
	if req.auth != "Bearer "+m54Token {
		t.Fatalf("authorization header must be the bearer token")
	}
	if req.contentType != "application/json" {
		t.Fatalf("content type must be application/json, got %q", req.contentType)
	}
	var decoded NodeSnapshot
	if err := json.Unmarshal(req.body, &decoded); err != nil {
		t.Fatalf("body must be one NodeSnapshotV1 JSON value: %v", err)
	}
	if decoded.NodeID != "mac-a" || decoded.Sequence != 1 || decoded.State.StateKind != "public" {
		t.Fatalf("unexpected decoded envelope: %+v", decoded)
	}
}

func TestM54ClientTimeoutIsTransient(t *testing.T) {
	handler := &recordingHandler{}
	handler.mu.Lock()
	handler.block = make(chan struct{})
	handler.mu.Unlock()
	server := httptest.NewServer(handler)
	defer server.Close()
	defer close(handler.block)

	client := NewClient(server.URL, m54Token, 40*time.Millisecond)
	start := time.Now()
	err := client.Send(context.Background(), m54Envelope())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout did not bound the request (%v)", elapsed)
	}
	sendErr, ok := err.(*SendError)
	if !ok || sendErr.Kind != ErrTransient {
		t.Fatalf("timeout must classify as transient, got %v", err)
	}
}

func TestM54ClientNetworkFailureIsTransient(t *testing.T) {
	server := httptest.NewServer(&recordingHandler{})
	url := server.URL
	server.Close() // unreachable from here on

	client := NewClient(url, m54Token, 200*time.Millisecond)
	err := client.Send(context.Background(), m54Envelope())
	sendErr, ok := err.(*SendError)
	if !ok || sendErr.Kind != ErrTransient {
		t.Fatalf("connection refused must classify as transient, got %v", err)
	}
}

func TestM54ClientRefusesOversizedEnvelope(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	snap := m54Envelope()
	snap.State.Tasks = make([]state.PublicTask, 0, 20000)
	for i := 0; i < 20000; i++ {
		snap.State.Tasks = append(snap.State.Tasks, state.PublicTask{ID: fmt.Sprintf("task-%d", i), Title: strings.Repeat("x", 128), StartedAt: m54Base, UpdatedAt: m54Base})
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(body) <= MaxSnapshotBodyBytes {
		t.Fatalf("test envelope must exceed the 256 KiB bound, got %d bytes", len(body))
	}
	client := NewClient(server.URL, m54Token, 500*time.Millisecond)
	sendErr, errT := func() (*SendError, error) {
		err := client.Send(context.Background(), snap)
		if err == nil {
			return nil, nil
		}
		se, ok := err.(*SendError)
		if !ok {
			return nil, err
		}
		return se, nil
	}()
	if errT != nil {
		t.Fatalf("send: %v", errT)
	}
	if sendErr == nil || sendErr.Kind != ErrPayload {
		t.Fatalf("oversized envelope must be refused as payload failure, got %v", sendErr)
	}
	if handler.count() != 0 {
		t.Fatalf("oversized envelope must not be transported")
	}
}

// TestM54ClientClampsRequestTimeoutToFrozenMaximum freezes M5.2 §26: 5
// seconds is the per-request maximum, so any non-positive or larger timeout is
// clamped down; only values inside (0, 5s] are preserved verbatim. Both the
// scheduler-facing field and the underlying http.Client must agree.
func TestM54ClientClampsRequestTimeoutToFrozenMaximum(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
	}{
		{"negative", -time.Second},
		{"zero", 0},
		{"250ms", 250 * time.Millisecond},
		{"5s", 5 * time.Second},
		{"6s", 6 * time.Second},
		{"30s", 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.in
			if want <= 0 || want > DefaultRequestTimeout {
				want = DefaultRequestTimeout
			}
			c := NewClient("http://hub.invalid", m54Token, tc.in)
			if c.timeout != want || c.httpClient.Timeout != want {
				t.Fatalf("NewClient(timeout=%v): timeout=%v httpClient.Timeout=%v, both must be %v", tc.in, c.timeout, c.httpClient.Timeout, want)
			}
		})
	}
}

// TestM54ClientErrorsNeverLeakToken freezes the security requirement: error
// strings are bounded classifications and never contain the bearer token.
func TestM54ClientErrorsNeverLeakToken(t *testing.T) {
	handler := &recordingHandler{}
	handler.setStatus(http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(server.URL, m54Token, 100*time.Millisecond)
	for i := 0; i < 4; i++ {
		err := client.Send(context.Background(), m54Envelope())
		if err == nil {
			t.Fatalf("expected classified failure")
		}
		if strings.Contains(err.Error(), m54Token) {
			t.Fatalf("error message leaks the bearer token: %q", err.Error())
		}
	}
	dead := httptest.NewServer(&recordingHandler{})
	deadURL := dead.URL
	dead.Close()
	err := NewClient(deadURL, m54Token, 100*time.Millisecond).Send(context.Background(), m54Envelope())
	if err == nil || strings.Contains(err.Error(), m54Token) {
		t.Fatalf("transport error must not leak the bearer token: %v", err)
	}
}
