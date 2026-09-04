// Package agentquota owns the optional Hub-side activation loop. It sends
// only the provider request configured by the operator; credentials never
// enter the Hub YAML or the public dashboard state.
package agentquota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

const (
	requestTimeout = 30 * time.Second
	// Keep the request's output ceiling aligned with the proven desktop
	// helper. This is a ceiling rather than a quota-consumption guarantee;
	// success is still established only from a verified model response.
	activationMaxTokens = 8
	maxKeyBytes         = 512
	maxBodyBytes        = 512 << 10
	heartbeatInterval   = 5 * time.Second
	missedTriggerGrace  = 2 * time.Minute
	confirmationWindow  = 60 * time.Second
	retryInterval       = 30 * time.Second
)

// Health is a credential-free snapshot suitable for the Admin UI.
type Health struct {
	Enabled       bool
	Provider      string
	State         string
	Message       string
	NextRunAt     *time.Time
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	// The response fields are bounded, credential-free details for the Admin
	// test panel. ResponsePreview contains only visible model text; the raw
	// provider body is never retained.
	LastHTTPStatus      int
	LastResponsePreview string
	LastUsageSummary    string
	LastResponseAt      *time.Time
}

// Event is a safe scheduler event. Reason is a bounded category, never raw
// provider output; HTTPStatus is kept separate so the Admin log can explain
// common failures such as 401 or 429 without storing response bodies.
type Event struct {
	At           time.Time
	ScheduledAt  time.Time
	Code         string
	Reason       string
	HTTPStatus   int
	Trigger      string
	Attempt      int
	ProviderCode string
	ResetAt      time.Time
	ResetText    string
	UsageSummary string
}

type EventSink func(Event)

// Runtime runs explicit daily local-time activation slots. A request failure
// never moves the next configured slot.
type Runtime struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.RWMutex
	health Health
	cfg    config.AgentQuotaConfig
	key    string
	log    *slog.Logger
	sink   EventSink
	loc    *time.Location

	cycleMu         sync.Mutex
	cycleWG         sync.WaitGroup
	activeCycle     bool
	activeAnchor    string
	firedAnchors    map[string]struct{}
	reportedAnchors map[string]struct{}
}

type activationOutcome string

const (
	outcomeVerified         activationOutcome = "verified"
	outcomeFiveHourQuota    activationOutcome = "five_hour_quota_exhausted"
	outcomeOtherQuota       activationOutcome = "other_quota_exhausted"
	outcomePlanExpired      activationOutcome = "plan_expired"
	outcomeTransientLimited activationOutcome = "transient_limited"
	outcomeProviderError    activationOutcome = "provider_error"
	outcomeUnverified       activationOutcome = "unverified"
	outcomeTransportError   activationOutcome = "transport_error"
)

type activationResult struct {
	Success         bool
	Outcome         activationOutcome
	HTTPStatus      int
	ProviderCode    string
	ResetAt         *time.Time
	ResetText       string
	ResponsePreview string
	UsageSummary    string
}

type providerFailure struct {
	Code      string
	Message   string
	ResetAt   *time.Time
	ResetText string
}

// KeyFile returns the private credential path beside the Hub config.
func KeyFile(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "agent-quota.key")
}

// SaveAPIKey writes a provider key atomically with private permissions.
func SaveAPIKey(path, value string) error {
	value = strings.TrimSpace(value)
	if path == "" || value == "" || len(value) > maxKeyBytes || strings.ContainsAny(value, "\r\n") {
		return errors.New("agent quota API key is invalid")
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return errors.New("agent quota key directory must be private")
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return errors.New("agent quota key directory must be private")
		}
	}
	tmp, err := os.CreateTemp(dir, ".agent-quota-key-*")
	if err != nil {
		return fmt.Errorf("create agent quota key: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect agent quota key: %w", err)
	}
	if _, err := tmp.WriteString(value + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write agent quota key: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync agent quota key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close agent quota key: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit agent quota key: %w", err)
	}
	return nil
}

// Start starts the optional scheduler. A disabled config returns an inert
// runtime so callers can always defer Close safely.
func Start(ctx context.Context, cfg config.AgentQuotaConfig, keyPath string, logger *slog.Logger, sinks ...EventSink) *Runtime {
	return start(ctx, cfg, keyPath, logger, time.Local, nil, sinks...)
}

// StartWithTimezone starts the scheduler using the application's configured
// IANA timezone instead of the container's operating-system timezone. This is
// important on NAS containers, which commonly run with UTC even when the Hub
// is configured for Asia/Shanghai or another operator timezone.
func StartWithTimezone(ctx context.Context, cfg config.AgentQuotaConfig, keyPath, timezone string, logger *slog.Logger, sinks ...EventSink) *Runtime {
	return StartWithTimezoneAndFired(ctx, cfg, keyPath, timezone, logger, nil, sinks...)
}

// StartWithTimezoneAndFired restores recently completed local schedule keys
// from the activator's private status file. It prevents a short worker or
// configuration reload from repeating a slot still inside its grace window.
func StartWithTimezoneAndFired(ctx context.Context, cfg config.AgentQuotaConfig, keyPath, timezone string, logger *slog.Logger, fired []string, sinks ...EventSink) *Runtime {
	loc := time.Local
	if name := strings.TrimSpace(timezone); name != "" {
		if loaded, err := time.LoadLocation(name); err == nil {
			loc = loaded
		}
	}
	return start(ctx, cfg, keyPath, logger, loc, fired, sinks...)
}

func start(ctx context.Context, cfg config.AgentQuotaConfig, keyPath string, logger *slog.Logger, loc *time.Location, fired []string, sinks ...EventSink) *Runtime {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var sink EventSink
	if len(sinks) > 0 {
		sink = sinks[0]
	}
	if loc == nil {
		loc = time.Local
	}
	firedAnchors := make(map[string]struct{}, len(fired))
	for _, key := range fired {
		if len(key) <= 96 {
			firedAnchors[key] = struct{}{}
		}
	}
	r := &Runtime{done: make(chan struct{}), cfg: cfg, key: keyPath, log: logger, sink: sink, loc: loc, firedAnchors: firedAnchors, reportedAnchors: make(map[string]struct{})}
	r.health = Health{Enabled: cfg.Enabled, Provider: cfg.Provider, State: "disabled", Message: "Agent quota activation is disabled."}
	if !cfg.Enabled {
		close(r.done)
		return r
	}
	r.health.State = "waiting"
	r.health.Message = "Waiting for the next custom activation time."
	if _, err := readAPIKey(keyPath); err != nil {
		r.health.State = "configuration_required"
		r.health.Message = "GLM API key is not configured."
	}
	if len(cfg.Schedules) == 0 {
		r.health.State = "configuration_required"
		r.health.Message = "At least one GLM activation time is required."
		close(r.done)
		return r
	}
	child, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	go r.loop(child)
	return r
}

func (r *Runtime) loop(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	r.tick(ctx, time.Now().In(r.loc))
	for {
		select {
		case now := <-ticker.C:
			r.tick(ctx, now.In(r.loc))
		case <-ctx.Done():
			return
		}
	}
}

func (r *Runtime) tick(ctx context.Context, now time.Time) {
	next := nextScheduledAnchor(now, r.cfg.Schedules)
	r.setNext(next)
	for _, schedule := range r.cfg.Schedules {
		anchor, ok := latestDueAnchor(now, schedule)
		if !ok {
			continue
		}
		key := logicalAnchorKey(anchor, schedule)
		claim := r.claimCycle(key)
		if claim == cycleAlreadyFired {
			continue
		}
		if claim == cycleBusy {
			r.reportOnce(key, Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_deferred", Reason: "cycle_busy", Trigger: "scheduled", Attempt: 1})
			continue
		}
		// Claim before checking the grace window. If a cycle is already in
		// flight, it owns this logical anchor even when the provider request
		// crosses the two-minute boundary. Reporting activation_skipped first
		// used to create a false "missed" record after a successful cycle.
		if now.Sub(anchor) > missedTriggerGrace {
			r.releaseCycle(key)
			r.reportOnce(key, Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_skipped", Reason: "missed_trigger_grace", Trigger: "scheduled", Attempt: 1})
			continue
		}
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_due", Reason: "scheduled_time", Trigger: "scheduled", Attempt: 1})
		r.cycleWG.Add(1)
		go func(anchor time.Time, key string) {
			defer r.cycleWG.Done()
			r.runScheduledCycle(ctx, anchor, key)
			r.finishCycle(key)
		}(anchor, key)
		break
	}
}

func (r *Runtime) activate(parent context.Context, anchor time.Time) {
	_ = r.activateWithTrigger(parent, anchor, "scheduled", 1)
}

type cycleClaim uint8

const (
	cycleClaimed cycleClaim = iota
	cycleBusy
	cycleAlreadyFired
)

func (r *Runtime) claimCycle(key string) cycleClaim {
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	if r.activeCycle {
		return cycleBusy
	}
	if _, fired := r.firedAnchors[key]; fired {
		return cycleAlreadyFired
	}
	r.activeCycle = true
	r.activeAnchor = key
	return cycleClaimed
}

func (r *Runtime) finishCycle(key string) {
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	if r.activeAnchor == key {
		r.firedAnchors[key] = struct{}{}
		r.activeCycle = false
		r.activeAnchor = ""
	}
}

// releaseCycle abandons a claim that was never eligible to run. It must not
// add the anchor to firedAnchors: a missed slot is a diagnostic outcome, not a
// completed activation cycle.
func (r *Runtime) releaseCycle(key string) {
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	if r.activeAnchor == key {
		r.activeCycle = false
		r.activeAnchor = ""
	}
}

// FiredAnchorKeys returns the bounded, credential-free schedule ledger used
// by the independent worker across its own reloads and restarts.
func (r *Runtime) FiredAnchorKeys() []string {
	r.cycleMu.Lock()
	defer r.cycleMu.Unlock()
	keys := make([]string, 0, len(r.firedAnchors))
	for key := range r.firedAnchors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 32 {
		keys = keys[len(keys)-32:]
	}
	return keys
}

// reportOnce keeps skipped/deferred schedule diagnostics useful without
// emitting the same notice on every five-second scheduler heartbeat.
func (r *Runtime) reportOnce(key string, event Event) {
	r.cycleMu.Lock()
	if r.reportedAnchors == nil {
		r.reportedAnchors = make(map[string]struct{})
	}
	if _, reported := r.reportedAnchors[key]; reported {
		r.cycleMu.Unlock()
		return
	}
	r.reportedAnchors[key] = struct{}{}
	r.cycleMu.Unlock()
	r.emit(event)
}

func (r *Runtime) runScheduledCycle(ctx context.Context, anchor time.Time, key string) {
	deadline := anchor.Add(confirmationWindow)
	sawFiveHourQuota := false
	attempt := 1

	for {
		result := r.activateWithTrigger(ctx, anchor, "scheduled", attempt)
		if result.Outcome == outcomeFiveHourQuota {
			sawFiveHourQuota = true
		}
		now := time.Now().In(r.loc)
		if result.Success {
			// One verified model response completes this scheduled activation.
			// In particular, do not repeat a successful request merely because
			// the slot happens to be near a five-hour quota boundary.
			return
		} else if !retryableActivation(result, deadline, now) {
			return
		}
		attempt++
		next := nextRetryTime(result, anchor, now, attempt, r.loc)
		if next.After(deadline) && !sawFiveHourQuota {
			return
		}
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_retry_scheduled", Reason: "retryable_provider_result", Trigger: "scheduled", Attempt: attempt, ProviderCode: result.ProviderCode, ResetAt: timeOrZero(result.ResetAt), ResetText: result.ResetText})
		if !waitUntil(ctx, next) {
			return
		}
		_ = key
	}
}

func (r *Runtime) activateWithTrigger(parent context.Context, anchor time.Time, trigger string, attemptNumber int) activationResult {
	attempt := time.Now()
	r.setAttempt(attempt)
	key, err := readAPIKey(r.key)
	if err != nil {
		r.setFailure("configuration_required", "GLM API key is not configured.")
		r.emit(Event{At: attempt.UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "api_key_unavailable", Trigger: trigger, Attempt: attemptNumber})
		r.log.Warn("agent quota activation unavailable", "error", "key_unavailable")
		return activationResult{Outcome: outcomeProviderError}
	}

	body, err := json.Marshal(map[string]any{
		"model":    r.cfg.Model,
		"messages": []map[string]string{{"role": "user", "content": "Reply exactly with OK"}},
		// A short response ceiling mirrors the desktop helper's observed
		// request shape. It does not prove or force any particular provider
		// accounting result.
		"max_tokens": activationMaxTokens,
	})
	if err != nil {
		r.setFailure("error", "GLM activation request could not be built.")
		r.emit(Event{At: attempt.UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "request_build", Trigger: trigger, Attempt: attemptNumber})
		return activationResult{Outcome: outcomeProviderError}
	}
	endpoint, anthropic, err := activationEndpoint(r.cfg.Endpoint)
	if err != nil {
		r.setFailure("error", "GLM activation endpoint is invalid.")
		r.emit(Event{At: attempt.UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "endpoint_invalid", Trigger: trigger, Attempt: attemptNumber})
		return activationResult{Outcome: outcomeProviderError}
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		r.setFailure("error", "GLM activation endpoint is invalid.")
		r.emit(Event{At: attempt.UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "endpoint_invalid", Trigger: trigger, Attempt: attemptNumber})
		return activationResult{Outcome: outcomeProviderError}
	}
	req.Header.Set("Content-Type", "application/json")
	if anthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("User-Agent", "DevBoard-AgentQuota/1.0")
	attemptReason := "http_request"
	if trigger == "manual" {
		attemptReason = "manual_test"
	}
	r.emit(Event{At: attempt.UTC(), ScheduledAt: anchor.UTC(), Code: "activation_attempt", Reason: attemptReason, Trigger: trigger, Attempt: attemptNumber})
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		r.setFailure("error", "GLM activation request failed.")
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "transport", Trigger: trigger, Attempt: attemptNumber})
		r.log.Warn("agent quota activation request failed", "error", "transport")
		return activationResult{Outcome: outcomeTransientLimited}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if readErr != nil {
		r.setFailure("error", "GLM activation response could not be read.")
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "response_read", HTTPStatus: resp.StatusCode, Trigger: trigger, Attempt: attemptNumber})
		return activationResult{HTTPStatus: resp.StatusCode, Outcome: outcomeProviderError}
	}
	r.setResponseMeta(time.Now(), resp.StatusCode)
	if failure := parseProviderFailure(responseBody, resp.StatusCode, r.loc); failure != nil {
		result := classifyProviderFailure(failure, resp.StatusCode)
		message := "GLM provider returned an error response."
		if result.Outcome == outcomeFiveHourQuota {
			message = "GLM reports that the 5-hour quota is exhausted."
		} else if result.Outcome == outcomeOtherQuota {
			message = "GLM reports that the quota is exhausted."
		}
		r.setFailure("waiting", message)
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "provider_error", HTTPStatus: resp.StatusCode, Trigger: trigger, Attempt: attemptNumber, ProviderCode: result.ProviderCode, ResetAt: timeOrZero(result.ResetAt), ResetText: result.ResetText})
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		outcome := outcomeProviderError
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			outcome = outcomeTransientLimited
		}
		r.setFailure("error", fmt.Sprintf("GLM activation returned HTTP %d.", resp.StatusCode))
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: "http_status", HTTPStatus: resp.StatusCode, Trigger: trigger, Attempt: attemptNumber})
		r.log.Warn("agent quota activation rejected", "status", resp.StatusCode)
		return activationResult{HTTPStatus: resp.StatusCode, Outcome: outcome}
	}
	verified, reason := verifyModelResponse(responseBody, anthropic)
	if !verified {
		message := "GLM response did not contain visible model text."
		if reason == "provider_error" {
			message = "GLM returned a provider error response."
		}
		r.setFailure("error", message)
		r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_failed", Reason: reason, HTTPStatus: resp.StatusCode, Trigger: trigger, Attempt: attemptNumber})
		return activationResult{HTTPStatus: resp.StatusCode, Outcome: outcomeUnverified}
	}
	responsePreview, _ := modelResponseText(responseBody, anthropic)
	usageSummary := responseUsageSummary(responseBody, anthropic)
	r.setSuccess(attempt, anchor, responsePreview, usageSummary)
	successReason := "response_verified"
	if trigger == "manual" {
		successReason = "manual_test_verified"
	}
	r.emit(Event{At: time.Now().UTC(), ScheduledAt: anchor.UTC(), Code: "activation_succeeded", Reason: successReason, HTTPStatus: resp.StatusCode, Trigger: trigger, Attempt: attemptNumber, UsageSummary: usageSummary})
	r.log.Info("agent quota activation verified", "provider", r.cfg.Provider)
	return activationResult{Success: true, HTTPStatus: resp.StatusCode, Outcome: outcomeVerified, ResponsePreview: responsePreview, UsageSummary: usageSummary}
}

// TestActivation performs one immediate activation using the saved provider
// configuration and key. It never changes the daily schedule or config file.
// The returned health is safe to show in the Admin page.
func TestActivation(ctx context.Context, cfg config.AgentQuotaConfig, keyPath string, logger *slog.Logger, sinks ...EventSink) Health {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var sink EventSink
	if len(sinks) > 0 {
		sink = sinks[0]
	}
	r := &Runtime{
		done: make(chan struct{}), cfg: cfg, key: keyPath, log: logger, sink: sink, loc: time.Local,
		health: Health{Enabled: cfg.Enabled, Provider: cfg.Provider, State: "waiting", Message: "Manual GLM activation test is running."},
	}
	r.activateWithTrigger(ctx, time.Now(), "manual", 1)
	close(r.done)
	return r.Health()
}

func activationEndpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false, errors.New("endpoint must be an http or https URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	lowerPath := strings.ToLower(path)
	anthropic := strings.Contains(lowerPath, "/anthropic")
	if anthropic && !strings.HasSuffix(lowerPath, "/v1/messages") {
		parsed.Path = path + "/v1/messages"
	} else if !anthropic && strings.HasSuffix(lowerPath, "/paas/v4") {
		parsed.Path = path + "/chat/completions"
	}
	return parsed.String(), anthropic, nil
}

// activationEnvelope is intentionally limited to the response fields needed
// to prove that the request reached a model. It does not retain or log the
// provider response body.
type activationEnvelope struct {
	Choices   []activationChoice `json:"choices"`
	Content   json.RawMessage    `json:"content"`
	Usage     activationUsage    `json:"usage"`
	Error     json.RawMessage    `json:"error"`
	Success   *bool              `json:"success"`
	Code      json.RawMessage    `json:"code"`
	ErrorCode json.RawMessage    `json:"error_code"`
}

type activationUsage struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
	TotalTokens      *int `json:"total_tokens"`
	InputTokens      *int `json:"input_tokens"`
	OutputTokens     *int `json:"output_tokens"`
}

type activationChoice struct {
	Message activationMessage `json:"message"`
	Text    string            `json:"text"`
}

type activationMessage struct {
	Content json.RawMessage `json:"content"`
}

// verifyModelResponse is the connection-test gate: a successful HTTP status
// or token usage is not enough. The body must contain visible text produced by
// the model. This also rejects provider error wrappers that incorrectly use
// HTTP 200. The strict text requirement is intentional: the Admin test must
// prove that a minimal dialogue completed and returned a usable answer, not
// merely prove that the endpoint accepted a request.
func verifyModelResponse(body []byte, anthropic bool) (bool, string) {
	text, reason := modelResponseText(body, anthropic)
	return text != "", reason
}

// modelResponseText extracts only a bounded, visible model reply for the
// authenticated Admin result. It is deliberately not written to diagnostics
// or logs, and never includes the raw provider body.
func modelResponseText(body []byte, anthropic bool) (string, string) {
	var envelope activationEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", "response_unverified"
	}
	if hasJSONValue(envelope.Error) || (envelope.Success != nil && !*envelope.Success) {
		return "", "provider_error"
	}
	if providerCodeFailed(envelope.Code) || providerCodeFailed(envelope.ErrorCode) {
		return "", "provider_error"
	}
	if anthropic {
		if text := visibleJSONTextValue(envelope.Content); text != "" {
			return boundedResponseText(text), ""
		}
	}
	// Most Anthropic responses use top-level content blocks. A few
	// compatibility gateways still return an OpenAI-style choices envelope
	// even when the request was sent to their /anthropic endpoint, so accept
	// that standard shape as well.
	for _, choice := range envelope.Choices {
		if strings.TrimSpace(choice.Text) != "" {
			return boundedResponseText(choice.Text), ""
		}
		if text := visibleJSONTextValue(choice.Message.Content); text != "" {
			return boundedResponseText(text), ""
		}
	}
	return "", "response_unverified"
}

// responseUsageSummary extracts only provider-reported token counters. Some
// compatible endpoints omit usage, so its absence is displayed explicitly by
// the Admin panel rather than being mistaken for a successful quota signal.
func responseUsageSummary(body []byte, anthropic bool) string {
	var envelope activationEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if anthropic {
		if envelope.Usage.InputTokens != nil {
			parts = append(parts, fmt.Sprintf("input_tokens=%d", *envelope.Usage.InputTokens))
		}
		if envelope.Usage.OutputTokens != nil {
			parts = append(parts, fmt.Sprintf("output_tokens=%d", *envelope.Usage.OutputTokens))
		}
	} else {
		if envelope.Usage.PromptTokens != nil {
			parts = append(parts, fmt.Sprintf("prompt_tokens=%d", *envelope.Usage.PromptTokens))
		}
		if envelope.Usage.CompletionTokens != nil {
			parts = append(parts, fmt.Sprintf("completion_tokens=%d", *envelope.Usage.CompletionTokens))
		}
		if envelope.Usage.TotalTokens != nil {
			parts = append(parts, fmt.Sprintf("total_tokens=%d", *envelope.Usage.TotalTokens))
		}
	}
	return strings.Join(parts, ", ")
}

func visibleJSONText(raw json.RawMessage) bool {
	return visibleJSONTextValue(raw) != ""
}

func visibleJSONTextValue(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		for _, part := range parts {
			if text := visibleJSONTextValue(part); text != "" {
				return text
			}
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		if kind, ok := object["type"]; ok {
			var blockType string
			if json.Unmarshal(kind, &blockType) == nil && strings.TrimSpace(blockType) != "" && !strings.EqualFold(strings.TrimSpace(blockType), "text") {
				return ""
			}
		}
		// Anthropic-compatible APIs normally return content blocks with a
		// `text` field. Some gateways normalize that block into a string,
		// while OpenAI-compatible gateways may wrap it under `content`.
		// Follow only these response fields; never treat provider metadata or
		// hidden reasoning as the visible model answer.
		if value, ok := object["text"]; ok {
			if text := visibleJSONTextValue(value); text != "" {
				return text
			}
		}
		if value, ok := object["content"]; ok {
			if text := visibleJSONTextValue(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func boundedResponseText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return text
}

func hasJSONValue(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}

func providerCodeFailed(raw json.RawMessage) bool {
	if !hasJSONValue(raw) {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.ToLower(strings.TrimSpace(text))
		return text != "" && text != "0" && text != "200" && text != "ok" && text != "success" && text != "succeeded"
	}
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number < 200 || number >= 300
	}
	return true
}

func readAPIKey(path string) (string, error) {
	if path == "" {
		return "", errors.New("key path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("key file is unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > maxKeyBytes {
		return "", errors.New("key file is empty or too large")
	}
	return value, nil
}

func nextScheduledAnchor(now time.Time, schedules []string) time.Time {
	var next time.Time
	for _, schedule := range schedules {
		parsed, err := time.Parse("15:04", strings.TrimSpace(schedule))
		if err != nil {
			continue
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		if !candidate.After(now) {
			continue
		}
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	if !next.IsZero() {
		return next
	}
	for _, schedule := range schedules {
		parsed, err := time.Parse("15:04", strings.TrimSpace(schedule))
		if err != nil {
			continue
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day()+1, parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	if next.IsZero() {
		return now.Add(24 * time.Hour)
	}
	return next
}

func latestDueAnchor(now time.Time, schedule string) (time.Time, bool) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(schedule))
	if err != nil {
		return time.Time{}, false
	}
	for dayOffset := 0; dayOffset >= -1; dayOffset-- {
		candidate := time.Date(now.Year(), now.Month(), now.Day()+dayOffset, parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		if !candidate.After(now) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

func logicalAnchorKey(anchor time.Time, schedule string) string {
	return anchor.Format("2006-01-02") + "|" + strings.TrimSpace(schedule)
}

func retryableActivation(result activationResult, deadline, now time.Time) bool {
	if now.Before(deadline) {
		switch result.Outcome {
		case outcomeFiveHourQuota, outcomeTransientLimited:
			return true
		case outcomeOtherQuota:
			return result.ProviderCode == "1308" && result.ResetAt != nil && !result.ResetAt.After(deadline)
		}
	}
	return false
}

func nextRetryTime(result activationResult, anchor, now time.Time, attempt int, loc *time.Location) time.Time {
	if result.ResetAt != nil && result.ResetAt.After(now) {
		return result.ResetAt.Add(500 * time.Millisecond)
	}
	target := anchor.Add(time.Duration(maxInt(1, attempt-1)) * retryInterval)
	if !target.After(now) {
		return now.Add(200 * time.Millisecond).In(loc)
	}
	return target
}

func waitUntil(ctx context.Context, target time.Time) bool {
	delay := time.Until(target)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func parseProviderFailure(body []byte, status int, loc *time.Location) *providerFailure {
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return nil
	}
	nested, _ := object["error"].(map[string]any)
	rawError, hasError := object["error"]
	if !hasError || rawError == nil {
		hasError = false
	}
	successFalse, _ := object["success"].(bool)
	code := firstScalarString(nested["code"], object["code"], findNestedScalar(object, "error_code"), findNestedScalar(object, "business_code"))
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	codeFailed := normalizedCode != "" && normalizedCode != "0" && normalizedCode != "200" && normalizedCode != "ok" && normalizedCode != "success" && normalizedCode != "succeeded"
	if numeric, err := strconv.Atoi(normalizedCode); err == nil {
		codeFailed = numeric < 200 || numeric >= 300
	}
	httpFailed := status < 200 || status >= 300
	if !hasError && !successFalse && !codeFailed && !httpFailed {
		return nil
	}
	message := firstScalarString(nested["message"], nested["msg"], rawError, object["message"], object["msg"], findNestedScalar(object, "error_message"))
	resetValue := firstValue(nested["next_flush_time"], object["next_flush_time"], findNestedValue(object, "next_flush_time"))
	resetText := scalarString(resetValue)
	if resetText == "" {
		resetText = extractResetText(message)
	}
	resetAt := parseResetTime(resetValue, resetText, loc)
	return &providerFailure{Code: code, Message: message, ResetAt: resetAt, ResetText: resetText}
}

func classifyProviderFailure(failure *providerFailure, status int) activationResult {
	code := strings.TrimSpace(failure.Code)
	message := strings.ToLower(failure.Message)
	mentionsFiveHours := strings.Contains(message, "5小时") || strings.Contains(message, "5 小时") || strings.Contains(message, "5-hour") || strings.Contains(message, "5 hour") || strings.Contains(message, "5h")
	outcome := outcomeProviderError
	switch {
	case code == "1316" || code == "1318" || code == "1320" || (code == "1308" && mentionsFiveHours):
		outcome = outcomeFiveHourQuota
	case code == "1310" || code == "1317" || code == "1319" || code == "1321" || code == "1308":
		outcome = outcomeOtherQuota
	case code == "1302" || code == "1305" || status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500:
		outcome = outcomeTransientLimited
	case code == "1309":
		outcome = outcomePlanExpired
	}
	return activationResult{HTTPStatus: status, Outcome: outcome, ProviderCode: code, ResetAt: failure.ResetAt, ResetText: failure.ResetText}
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstScalarString(values ...any) string {
	for _, value := range values {
		if text := scalarString(value); text != "" {
			return text
		}
	}
	return ""
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func findNestedScalar(value any, key string) any {
	if object, ok := value.(map[string]any); ok {
		if found, exists := object[key]; exists {
			return found
		}
		for _, child := range object {
			if found := findNestedScalar(child, key); found != nil {
				return found
			}
		}
	}
	if list, ok := value.([]any); ok {
		for _, child := range list {
			if found := findNestedScalar(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func findNestedValue(value any, key string) any {
	return findNestedScalar(value, key)
}

func extractResetText(message string) string {
	patterns := []string{
		`\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}`,
		`\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}`,
		`\b\d{13}\b`,
		`\b\d{10}\b`,
	}
	for _, pattern := range patterns {
		if match := regexp.MustCompile(pattern).FindString(message); match != "" {
			return match
		}
	}
	return ""
}

func parseResetTime(value any, text string, loc *time.Location) *time.Time {
	if loc == nil {
		loc = time.Local
	}
	parsedText := scalarString(value)
	if parsedText == "" {
		parsedText = strings.TrimSpace(text)
	}
	if parsedText != "" {
		if raw, err := strconv.ParseFloat(parsedText, 64); err == nil {
			if raw > 10_000_000_000 {
				raw /= 1000
			}
			parsed := time.Unix(int64(raw), 0).In(loc)
			return &parsed
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02 15:04"} {
			var parsed time.Time
			var err error
			if strings.Contains(layout, "Z07") {
				parsed, err = time.Parse(layout, parsedText)
			} else {
				parsed, err = time.ParseInLocation(layout, parsedText, loc)
			}
			if err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func (r *Runtime) emit(event Event) {
	if r.sink != nil {
		r.sink(event)
	}
}

func (r *Runtime) setNext(value time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := value
	r.health.NextRunAt = &copy
	if r.health.State == "disabled" {
		r.health.State = "waiting"
	}
}

func (r *Runtime) setAttempt(value time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.health.LastAttemptAt = cloneTime(&value)
	r.health.State = "sending"
	r.health.Message = "Sending the scheduled GLM activation request."
	r.health.LastHTTPStatus = 0
	r.health.LastResponsePreview = ""
	r.health.LastUsageSummary = ""
	r.health.LastResponseAt = nil
}

func (r *Runtime) setResponseMeta(receivedAt time.Time, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.health.LastHTTPStatus = status
	r.health.LastResponseAt = cloneTime(&receivedAt)
}

func (r *Runtime) setFailure(state, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.health.State = state
	r.health.Message = message
}

func (r *Runtime) setSuccess(attempt time.Time, _ time.Time, responsePreview, usageSummary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.health.LastSuccessAt = cloneTime(&attempt)
	r.health.State = "healthy"
	r.health.Message = "Model response received; last GLM activation was verified."
	r.health.LastResponsePreview = responsePreview
	r.health.LastUsageSummary = usageSummary
	if responsePreview != "" {
		r.health.Message += " Response: " + responsePreview
	}
}

// Health returns a credential-free copy of the current scheduler state.
func (r *Runtime) Health() Health {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Health{
		Enabled: r.health.Enabled, Provider: r.health.Provider, State: r.health.State, Message: r.health.Message,
		NextRunAt: cloneTime(r.health.NextRunAt), LastAttemptAt: cloneTime(r.health.LastAttemptAt), LastSuccessAt: cloneTime(r.health.LastSuccessAt),
		LastHTTPStatus: r.health.LastHTTPStatus, LastResponsePreview: r.health.LastResponsePreview, LastUsageSummary: r.health.LastUsageSummary, LastResponseAt: cloneTime(r.health.LastResponseAt),
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Close stops the scheduler and waits for both its heartbeat loop and every
// in-flight scheduled request to finish. The second wait is essential during
// config reload: otherwise a replacement runtime could claim the same local
// slot while the cancelled request from the old runtime is still completing.
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
	r.cycleWG.Wait()
}
