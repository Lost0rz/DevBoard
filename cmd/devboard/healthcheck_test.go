package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func healthTestServer(t *testing.T, status int, body string, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("healthcheck method=%s, want GET", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHealthcheckHealthyRoleAccepted(t *testing.T) {
	srv := healthTestServer(t, http.StatusOK, `{"status":"ok","role":"hub","schemaVersion":1}`, 0)
	for _, role := range []string{"hub", ""} {
		args := []string{"--url", srv.URL}
		if role != "" {
			args = append(args, "--expect-role", role)
		}
		if err := runHealthcheck(args); err != nil {
			t.Fatalf("role=%q: %v", role, err)
		}
	}
}

func TestHealthcheckWrongRoleRejected(t *testing.T) {
	srv := healthTestServer(t, http.StatusOK, `{"status":"ok","role":"node","schemaVersion":1}`, 0)
	if err := runHealthcheck([]string{"--url", srv.URL, "--expect-role", "hub"}); err == nil {
		t.Fatal("wrong role must fail the healthcheck")
	}
}

func TestHealthcheckNon200Rejected(t *testing.T) {
	srv := healthTestServer(t, http.StatusServiceUnavailable, `{"status":"ok","role":"hub"}`, 0)
	if err := runHealthcheck([]string{"--url", srv.URL, "--expect-role", "hub"}); err == nil {
		t.Fatal("non-200 must fail the healthcheck")
	}
}

func TestHealthcheckMalformedJSONRejected(t *testing.T) {
	srv := healthTestServer(t, http.StatusOK, `{"status":`, 0)
	if err := runHealthcheck([]string{"--url", srv.URL}); err == nil {
		t.Fatal("malformed JSON must fail the healthcheck")
	}
	srv2 := healthTestServer(t, http.StatusOK, `{"status":"degraded","role":"hub"}`, 0)
	if err := runHealthcheck([]string{"--url", srv2.URL}); err == nil {
		t.Fatal("status!=ok must fail the healthcheck")
	}
	srv3 := healthTestServer(t, http.StatusOK, `{"status":"ok","role":"hub"} trailing`, 0)
	if err := runHealthcheck([]string{"--url", srv3.URL}); err == nil {
		t.Fatal("trailing non-JSON content must fail the healthcheck")
	}
}

func TestHealthcheckTimeoutRejected(t *testing.T) {
	// The handler stalls past the bounded 2s client timeout.
	srv := healthTestServer(t, http.StatusOK, `{"status":"ok","role":"hub"}`, 3*time.Second)
	start := time.Now()
	if err := runHealthcheck([]string{"--url", srv.URL}); err == nil {
		t.Fatal("timeout must fail the healthcheck")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("healthcheck exceeded its bounded timeout: %v", elapsed)
	}
}

// TestRestartSignalRequestsGracefulShutdown proves the M5.5A restart model:
// a managed restart request drains the server (Shutdown invoked, listen
// returns ErrServerClosed) and the serve cycle exits nil — the supervisor,
// not the handler, owns process lifetime.
func TestRestartSignalRequestsGracefulShutdown(t *testing.T) {
	restart := newRestartSignal()
	listenStarted := make(chan struct{})
	shutdownCalled := make(chan struct{})

	listen := func() error {
		close(listenStarted)
		<-shutdownCalled // emulate ListenAndServe: returns once Shutdown drains
		return http.ErrServerClosed
	}

	done := make(chan error, 1)
	go func() {
		done <- serveWithShutdown(
			listen,
			func(ctx context.Context) error { close(shutdownCalled); return nil },
			make(chan struct{}), // signals never fire
			restart.C(),
		)
	}()

	<-listenStarted
	restart.Request()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("graceful restart cycle returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restart request did not shut down the serve cycle")
	}
	select {
	case <-shutdownCalled:
	default:
		t.Fatal("shutdown was never invoked")
	}
}
