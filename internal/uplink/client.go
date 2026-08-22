package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SnapshotPath is the single frozen M5.2 machine write route.
const SnapshotPath = "/api/node/v1/snapshot"

// MaxSnapshotBodyBytes mirrors the frozen M5.2 receiver bound of 256 KiB. The
// client refuses to send anything larger so a runaway projection can never be
// transported.
const MaxSnapshotBodyBytes = 256 * 1024

// maxResponseBody bounds how much of an error response is read before it is
// discarded. Response bodies are never logged and never surfaced.
const maxResponseBody = 4 * 1024

// DefaultRequestTimeout is the frozen M5.2 §26 per-request timeout: 5 seconds
// maximum.
const DefaultRequestTimeout = 5 * time.Second

// ErrorKind classifies a failed send for the scheduler's retry policy:
// transient failures are retried with backoff, auth failures re-attempt
// slowly, payload failures wait for a fresh snapshot, and conflicts
// resynchronize the session.
type ErrorKind string

const (
	ErrTransient ErrorKind = "transient"
	ErrAuth      ErrorKind = "auth"
	ErrPayload   ErrorKind = "payload"
	ErrConflict  ErrorKind = "conflict"
)

// SendError is a bounded, generic failure classification. It deliberately
// carries no URL, no token, no request body and no hub response text.
type SendError struct {
	Kind   ErrorKind
	Status int
}

func (e *SendError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("uplink send failed: %s (http %d)", e.Kind, e.Status)
	}
	return "uplink send failed: " + string(e.Kind)
}

// Client is the node-side machine HTTP client for the frozen snapshot route.
// It is transport only: scheduling, sessions and sequencing live in the
// scheduler.
type Client struct {
	url        string
	token      string
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient builds a snapshot client for a configured hub base endpoint. The
// endpoint must already be validated (http/https, bare host address); the
// frozen route is appended here so callers can never target another path.
func NewClient(endpoint string, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return &Client{
		url:   strings.TrimSuffix(strings.TrimSpace(endpoint), "/") + SnapshotPath,
		token: token,
		httpClient: &http.Client{
			Timeout: timeout,
			// The machine endpoint is exact; a redirect is a configuration
			// defect, not something to follow with the bearer credential.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		timeout: timeout,
	}
}

// Send posts one NodeSnapshotV1 envelope and classifies the outcome. A nil
// return means HTTP 200 from the hub. The request runs under the per-request
// M5.2 §26 timeout (5 seconds maximum); parent cancellation still aborts
// promptly.
func (c *Client) Send(ctx context.Context, snap NodeSnapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return &SendError{Kind: ErrPayload}
	}
	if len(body) > MaxSnapshotBodyBytes {
		return &SendError{Kind: ErrPayload, Status: http.StatusRequestEntityTooLarge}
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return &SendError{Kind: ErrPayload}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendError{Kind: ErrTransient}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
	return classifyStatus(resp.StatusCode)
}

func classifyStatus(status int) error {
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusBadRequest, status == http.StatusRequestEntityTooLarge, status == http.StatusUnsupportedMediaType:
		return &SendError{Kind: ErrPayload, Status: status}
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return &SendError{Kind: ErrAuth, Status: status}
	case status == http.StatusConflict:
		return &SendError{Kind: ErrConflict, Status: status}
	case status >= 300 && status < 500:
		return &SendError{Kind: ErrPayload, Status: status}
	default:
		// 5xx and anything unexpected: transient transport/server failure.
		return &SendError{Kind: ErrTransient, Status: status}
	}
}
