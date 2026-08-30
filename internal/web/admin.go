package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agentquota"
	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
)

// adminSessionTTL bounds an authenticated admin session (M5.5A B7: 8–12h).
const adminSessionTTL = 12 * time.Hour

// adminMinSecretBytes is the minimum decoded machine provisioning secret
// length: bootstrap generates 32 crypto-random bytes (64 hex characters).
const adminMinSecretBytes = 32

// AdminOptions configures the M5.5A authenticated hub admin surface.
type AdminOptions struct {
	// ConfigPath is the hub's persistent config file; mutations edit it
	// atomically and then request a supervised restart.
	ConfigPath string
	// TokenFile holds the machine provisioning secret (openssl rand -hex 32),
	// mode 0600. It is never used by the human-facing login form.
	TokenFile string
	// PasswordFile holds the opaque, mode-0600 operator password record. A
	// missing file is the supported first-run setup state.
	PasswordFile string
	// Nodes exposes per-node connection status for the management page.
	Nodes *hub.NodeStateStore
	// RequestRestart is invoked after a successful atomic save.
	RequestRestart func()
	// RestartDelay gives the browser time to receive the rendered response and
	// stylesheet before the supervisor restarts the Hub. Production wiring uses
	// a short delay; tests can leave it zero for synchronous calls.
	RestartDelay time.Duration
	// SaveConfig defaults to config.SaveAtomic. Tests inject a failing saver
	// to prove that persistence failures never request a restart.
	SaveConfig func(string, config.Config) error
	// Diagnostics is the allow-listed, bounded application diagnostic store.
	// When nil, the handler creates a default in-process ring.
	Diagnostics    *DiagnosticsRing
	ProductVersion string
	GitCommit      string
	RuntimeReady   bool
	StartedAt      time.Time
	Logger         *slog.Logger
	Now            func() time.Time
	// AgentQuotaHealth is a credential-free runtime snapshot supplied by the
	// independent activator's private status file. It is optional so existing
	// handler tests remain inert.
	AgentQuotaHealth func() AgentQuotaHealth
	// AgentQuotaKeyFile is a private file beside the Hub config by default.
	AgentQuotaKeyFile string
	// AgentQuotaControlFile contains a credential-free manual-test request for
	// the independent activator. The Hub never sends provider traffic itself.
	AgentQuotaControlFile string
	// AgentQuotaAudit is a durable, credential-free scheduler audit. It is
	// intentionally separate from the short-lived general diagnostics ring.
	AgentQuotaAudit agentquota.AuditLog
	// AgentQuotaAuditTokenFile authorizes the external read-only audit API.
	// It defaults beside the Hub config and is never returned by this handler.
	AgentQuotaAuditTokenFile string
}

// AgentQuotaHealth is the safe status contract exposed to the Admin page.
// It deliberately contains no API key, raw response body, or provider
// credentials; successful tests may include only a bounded visible reply
// preview in Message.
type AgentQuotaHealth struct {
	Enabled       bool
	Provider      string
	State         string
	Message       string
	NextRunAt     *time.Time
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	ManualTest    *agentquota.Health
}

// AdminHandler serves the hub /admin surface: password login, node management
// (add / enable / disable / reset token), safe settings, and diagnostics.
// Sessions are HMAC-signed cookies derived from the machine provisioning
// secret, so they survive hub restarts; the cookie never contains either raw
// credential and node tokens are shown exactly once, in the mutation result
// that generated them.
type AdminHandler struct {
	opts             AdminOptions
	secret           []byte
	templates        *template.Template
	logger           *slog.Logger
	mutation         sync.Mutex
	agentQuotaTestMu sync.Mutex
	auditSecret      []byte
}

func NewAdminHandler(opts AdminOptions) (*AdminHandler, error) {
	if err := config.RequirePrivateFile(opts.ConfigPath); err != nil {
		return nil, fmt.Errorf("admin config unreadable")
	}
	secret, err := loadAdminSecret(opts.TokenFile)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.PasswordFile) == "" {
		// Older Hub configs did not have a password_file key. Keep their first
		// run safe and deterministic by placing the new record beside the
		// existing machine token rather than falling back to that token.
		opts.PasswordFile = filepath.Join(filepath.Dir(opts.TokenFile), "admin.password")
	}
	if strings.TrimSpace(opts.AgentQuotaKeyFile) == "" {
		opts.AgentQuotaKeyFile = agentquota.KeyFile(opts.ConfigPath)
	}
	if strings.TrimSpace(opts.AgentQuotaControlFile) == "" {
		opts.AgentQuotaControlFile = agentquota.ControlFile(opts.ConfigPath)
	}
	var auditSecret []byte
	if opts.AgentQuotaAudit != nil {
		if strings.TrimSpace(opts.AgentQuotaAuditTokenFile) == "" {
			opts.AgentQuotaAuditTokenFile = agentquota.AuditTokenFile(opts.ConfigPath)
		}
		var err error
		auditSecret, err = agentquota.EnsureAuditToken(opts.AgentQuotaAuditTokenFile)
		if err != nil {
			return nil, err
		}
	}
	if _, _, err := loadAdminPassword(opts.PasswordFile); err != nil {
		return nil, err
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.SaveConfig == nil {
		opts.SaveConfig = config.SaveAtomic
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Diagnostics == nil {
		opts.Diagnostics = NewDiagnosticsRing(200, "info")
	}
	if opts.StartedAt.IsZero() {
		opts.StartedAt = opts.Now()
	}
	t, err := template.New("admin").Funcs(template.FuncMap{
		"fmtOptionalTime": fmtOptionalTime,
		"statusClass":     func(status string) string { return connectionStateClass(strings.ToUpper(status)) },
		"upper":           strings.ToUpper,
	}).ParseFS(templateFS, "templates/admin.html")
	if err != nil {
		return nil, err
	}
	return &AdminHandler{opts: opts, secret: secret, templates: t, logger: opts.Logger, auditSecret: auditSecret}, nil
}

func loadAdminSecret(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("admin token file unreadable")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("admin token file permissions must be 0600 or stricter")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("admin token file unreadable")
	}
	secret := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(secret)
	if err != nil || len(decoded) < adminMinSecretBytes {
		return nil, fmt.Errorf("admin token too short")
	}
	return []byte(secret), nil
}

type adminSession struct {
	expiry time.Time
	nonce  string
}

type adminView struct {
	Page string
	// RefreshSeconds is only consumed by the authenticated Operator Console
	// pages that opt into bounded polling. It never affects /display.
	RefreshSeconds int
	// Login screen.
	LoginError    string
	SetupRequired bool
	SetupError    string
	// Management screen (authenticated).
	CSRF                string
	Nodes               []adminNodeRow
	Message             string
	Err                 string
	RestartPending      bool
	Result              string // one-time generated node token
	ResultFor           string // node id the token belongs to
	Overview            adminOverviewView
	Settings            adminSettingsView
	Logs                adminLogsView
	AgentQuotaLogs      []Diagnostic
	AgentQuotaAuditLogs []adminAgentQuotaAuditRow
	AgentQuotaTest      adminAgentQuotaTestView
	Displays            []adminDisplayView
	ServerBaseURL       string
	PrimaryDisplayURL   string
}

type adminOverviewView struct {
	Role, Health, HealthClass, Version, Commit, Uptime, LastSnapshot       string
	Registered, Enabled, Online, Stale, Offline                            int
	ConfigReady, AdminCredentialReady, AdminPasswordReady, PersistentReady bool
	AgentQuota                                                             adminAgentQuotaView
	NodeChartStyle, ReadinessChartStyle                                    string
	NodeStatusSegments, ReadinessSegments                                  []adminChartSegment
}

type adminChartSegment struct {
	Label, Value, Percent, Remainder, Offset, Class string
}

type adminAgentQuotaView struct {
	Enabled, KeyConfigured                                      bool
	Provider, State, Message, NextRun, LastAttempt, LastSuccess string
}

type adminAgentQuotaTestView struct {
	Attempted, Verified bool
	State, StateClass   string
	HTTPStatus          string
	Model               string
	Response            string
	Usage               string
	ReceivedAt          string
	Message             string
}

type adminAgentQuotaAuditRow struct {
	Timestamp   string
	ScheduledAt string
	Level       string
	EventCode   string
	Detail      string
}

type adminSettingsView struct {
	CSRF                    string
	ConsoleRefreshSeconds   int
	DiagnosticsMinLevel     string
	DiagnosticsCapacity     int
	PadPath                 string
	KindleRightPath         string
	KindleLeftPath          string
	PasswordConfigured      bool
	Message                 string
	Err                     string
	RestartPending          bool
	AgentQuotaEnabled       bool
	AgentQuotaProvider      string
	AgentQuotaEndpoint      string
	AgentQuotaModel         string
	AgentQuotaSchedules     string
	AgentQuotaScheduleRows  []adminScheduleRow
	AgentQuotaKeyConfigured bool
}

type adminScheduleRow struct {
	Value string
}

type adminDisplayView struct {
	ID          string
	Label       string
	Description string
	Path        string
	URL         string
	Field       string
}

type adminLogsView struct {
	CSRF        string
	Diagnostics []Diagnostic
	Level       string
	Component   string
	Limit       int
	Components  []string
}

type adminNodeRow struct {
	ID          string
	DisplayName string
	Accent      string
	Enabled     bool
	Status      string
	LastSuccess string
}

func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/admin":
		h.handleIndex(w, r)
	case "/admin/overview", "/admin/nodes", "/admin/logs":
		h.handlePage(w, r)
	case "/admin/settings":
		if r.Method == http.MethodPost {
			h.handleSettings(w, r)
		} else {
			h.handlePage(w, r)
		}
	case "/admin/settings/save":
		h.handleSettings(w, r)
	case "/admin/settings/display":
		h.handleDisplaySettings(w, r)
	case "/admin/settings/agent-quota":
		h.handleAgentQuotaSettings(w, r)
	case "/admin/settings/agent-quota/test":
		h.handleAgentQuotaTest(w, r)
	case "/admin/settings/password":
		h.handlePasswordChange(w, r)
	case "/admin/login":
		h.handleLogin(w, r)
	case "/admin/setup":
		if r.Method == http.MethodPost {
			h.handleSetup(w, r)
		} else {
			h.handleSetupPage(w, r)
		}
	case "/admin/logout":
		h.handleLogout(w, r)
	case "/admin/nodes/add", "/admin/nodes/enable", "/admin/nodes/disable", "/admin/nodes/reset", "/admin/nodes/accent":
		h.handleMutation(w, r)
	case "/admin/api/v1/nodes":
		h.handleProvision(w, r)
	case "/admin/api/v1/agent-quota/events":
		h.handleAgentQuotaAuditAPI(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleAgentQuotaAuditAPI exposes only the durable, redacted scheduler
// events. It deliberately uses its own read-only bearer token instead of the
// Node provisioning credential or a browser session, so external monitors can
// poll it without gaining mutation authority.
func (h *AdminHandler) handleAgentQuotaAuditAPI(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	if h.opts.AgentQuotaAudit == nil || len(h.auditSecret) == 0 {
		http.NotFound(w, r)
		return
	}
	if !h.auditAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="devboard-agent-quota-audit"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	query, err := parseAgentQuotaAuditQuery(r)
	if err != nil {
		http.Error(w, "invalid audit query", http.StatusBadRequest)
		return
	}
	records, err := h.opts.AgentQuotaAudit.List(query)
	if err != nil {
		h.logger.Warn("agent quota audit unavailable", "error", "audit_unavailable")
		http.Error(w, "agent quota audit unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		SchemaVersion int                      `json:"schemaVersion"`
		GeneratedAt   time.Time                `json:"generatedAt"`
		Records       []agentquota.AuditRecord `json:"records"`
	}{SchemaVersion: 1, GeneratedAt: h.opts.Now().UTC(), Records: records})
}

func (h *AdminHandler) auditAuthorized(r *http.Request) bool {
	const prefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if len(authorization) <= len(prefix) || !strings.HasPrefix(authorization, prefix) {
		return false
	}
	given := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(authorization, prefix))))
	want := sha256.Sum256(h.auditSecret)
	return subtle.ConstantTimeCompare(given[:], want[:]) == 1
}

func parseAgentQuotaAuditQuery(r *http.Request) (agentquota.AuditQuery, error) {
	values := r.URL.Query()
	for key := range values {
		switch key {
		case "limit", "since", "until":
		default:
			return agentquota.AuditQuery{}, fmt.Errorf("unsupported query")
		}
	}
	query := agentquota.AuditQuery{Limit: 200}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return agentquota.AuditQuery{}, fmt.Errorf("invalid limit")
		}
		query.Limit = limit
	}
	parseTime := func(name string) (*time.Time, error) {
		raw := strings.TrimSpace(values.Get(name))
		if raw == "" {
			return nil, nil
		}
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, err
		}
		return &value, nil
	}
	var err error
	if query.Since, err = parseTime("since"); err != nil {
		return agentquota.AuditQuery{}, err
	}
	if query.Until, err = parseTime("until"); err != nil {
		return agentquota.AuditQuery{}, err
	}
	if query.Since != nil && query.Until != nil && query.Until.Before(*query.Since) {
		return agentquota.AuditQuery{}, fmt.Errorf("inverted window")
	}
	return query, nil
}

func (h *AdminHandler) recordAgentQuotaEvent(event agentquota.Event) {
	if h.opts.AgentQuotaAudit != nil {
		if err := h.opts.AgentQuotaAudit.Record(event); err != nil {
			h.logger.Warn("agent quota audit record failed", "error", "audit_write_failed")
		}
	}
	if h.opts.Diagnostics != nil {
		h.opts.Diagnostics.RecordAgentQuota(event)
	}
}

func (h *AdminHandler) agentQuotaAuditRows(limit int) []adminAgentQuotaAuditRow {
	if h.opts.AgentQuotaAudit == nil {
		return nil
	}
	records, err := h.opts.AgentQuotaAudit.List(agentquota.AuditQuery{Limit: limit})
	if err != nil {
		h.logger.Warn("agent quota audit read failed", "error", "audit_read_failed")
		return nil
	}
	rows := make([]adminAgentQuotaAuditRow, 0, len(records))
	for _, record := range records {
		rows = append(rows, adminAgentQuotaAuditRow{
			Timestamp:   record.RecordedAt.Local().Format("2006-01-02 15:04:05 MST"),
			ScheduledAt: auditScheduledAt(record),
			Level:       auditLevel(record),
			EventCode:   record.EventCode,
			Detail:      auditDetail(record),
		})
	}
	return rows
}

func auditScheduledAt(record agentquota.AuditRecord) string {
	if record.ScheduledAt == nil {
		return "—"
	}
	return record.ScheduledAt.Local().Format("2006-01-02 15:04:05 MST")
}

func auditLevel(record agentquota.AuditRecord) string {
	switch record.EventCode {
	case "activation_failed", "activation_skipped":
		return "error"
	case "activation_deferred", "activation_retry_scheduled":
		return "warn"
	default:
		return "info"
	}
}

func auditDetail(record agentquota.AuditRecord) string {
	reasons := map[string]string{
		"scheduled_time":            "Scheduled time reached",
		"http_request":              "Outbound request started",
		"manual_test":               "Manual test: outbound request started",
		"response_verified":         "Model response received and verified",
		"manual_test_verified":      "Manual test: model response received and verified",
		"api_key_unavailable":       "GLM API key unavailable",
		"request_build":             "Request could not be built",
		"endpoint_invalid":          "Endpoint is invalid",
		"transport":                 "Network or timeout failure",
		"response_read":             "Response could not be read",
		"response_unverified":       "Response did not contain a verifiable model result",
		"provider_error":            "Provider returned an error payload",
		"http_status":               "Provider returned a non-success HTTP status",
		"retryable_provider_result": "Retry scheduled after provider response",
		"missed_trigger_grace":      "Hub did not claim this time within the two-minute trigger window",
		"cycle_busy":                "A previous activation cycle is still active",
	}
	detail := reasons[record.Reason]
	if detail == "" {
		detail = "No additional detail"
	}
	if record.HTTPStatus != 0 {
		detail += fmt.Sprintf(" · HTTP %d", record.HTTPStatus)
	}
	if record.ProviderCode != "" {
		detail += " · provider code " + record.ProviderCode
	}
	if record.ResetAt != nil {
		detail += " · reset " + record.ResetAt.Local().Format("2006-01-02 15:04:05 MST")
	}
	if record.UsageSummary != "" {
		detail += " · usage " + record.UsageSummary
	}
	return detail
}

// handleProvision is the machine-readable half of the idempotent Node
// onboarding flow. It is a registry mutation, not a data-plane or remote
// control route. The machine provisioning secret is accepted only in an
// Authorization header, and the generated Node token is returned only to that
// authenticated caller so the local onboarding command can write its own
// config.
func (h *AdminHandler) handleProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) <= len(prefix) || !strings.HasPrefix(auth, prefix) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	given := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(auth, prefix))))
	want := sha256.Sum256(h.secret)
	if subtle.ConstantTimeCompare(given[:], want[:]) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, managedFormMaxBytes)
	var request struct {
		NodeID      string `json:"nodeId"`
		DisplayName string `json:"displayName"`
		Accent      string `json:"accent"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	request.NodeID = strings.TrimSpace(request.NodeID)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.Accent = strings.ToLower(strings.TrimSpace(request.Accent))
	if request.NodeID == "" {
		http.Error(w, "nodeId is required", http.StatusBadRequest)
		return
	}

	h.mutation.Lock()
	defer h.mutation.Unlock()
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		http.Error(w, "admin unavailable", http.StatusInternalServerError)
		return
	}
	token := ""
	created := true
	for i := range cfg.Nodes.Registered {
		if cfg.Nodes.Registered[i].NodeID != request.NodeID {
			continue
		}
		created = false
		if request.DisplayName != "" && cfg.Nodes.Registered[i].DisplayName != request.DisplayName {
			cfg.Nodes.Registered[i].DisplayName = request.DisplayName
		}
		if request.Accent != "" {
			cfg.Nodes.Registered[i].Accent = request.Accent
		}
		token = cfg.Nodes.Registered[i].Token
		break
	}
	if created {
		token, err = newNodeToken()
		if err != nil {
			http.Error(w, "admin unavailable", http.StatusInternalServerError)
			return
		}
		cfg.Nodes.Registered = append(cfg.Nodes.Registered, config.NodeConfig{NodeID: request.NodeID, DisplayName: request.DisplayName, Accent: request.Accent, Token: token})
	}
	if err := h.opts.SaveConfig(h.opts.ConfigPath, cfg); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "registry_rejected")
		http.Error(w, "registry save failed", http.StatusInternalServerError)
		return
	}
	h.requestRestart()
	h.opts.Diagnostics.Record("info", "admin", "registry_saved")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"schemaVersion": 1, "ok": true, "status": "registered", "created": created,
		"nodeId": request.NodeID, "displayName": request.DisplayName, "accent": request.Accent, "token": token,
	})
}

func (h *AdminHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, ok := h.session(r)
	if !ok {
		h.renderUnauthenticated(w, r)
		return
	}
	http.Redirect(w, r, "/admin/overview", http.StatusSeeOther)
}

func (h *AdminHandler) renderUnauthenticated(w http.ResponseWriter, r *http.Request) {
	configured, err := h.passwordConfigured()
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	if !configured {
		h.render(w, http.StatusOK, adminView{SetupRequired: true})
		return
	}
	h.render(w, http.StatusOK, adminView{})
}

func (h *AdminHandler) authenticated(w http.ResponseWriter, r *http.Request) (adminSession, bool) {
	session, ok := h.session(r)
	if !ok {
		h.renderUnauthenticated(w, r)
		return adminSession{}, false
	}
	configured, err := h.passwordConfigured()
	if err != nil {
		h.httpUnavailable(w)
		return adminSession{}, false
	}
	if !configured {
		h.render(w, http.StatusOK, adminView{SetupRequired: true})
		return adminSession{}, false
	}
	return session, true
}

func (h *AdminHandler) handlePage(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.logger.Error("admin: load config failed", "err", "config_unreadable")
		http.Error(w, "admin unavailable", http.StatusInternalServerError)
		return
	}
	view, err := h.consoleView(r, session, cfg, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.render(w, http.StatusOK, view)
}

func (h *AdminHandler) consoleView(r *http.Request, session adminSession, cfg config.Config, message, errMsg string) (adminView, error) {
	logs, err := h.logsView(r, session, cfg)
	if err != nil {
		return adminView{}, err
	}
	view := adminView{
		Page:                "console",
		RefreshSeconds:      cfg.Operator.ConsoleRefreshSeconds,
		CSRF:                h.csrfToken(session),
		Message:             message,
		Err:                 errMsg,
		Overview:            h.overview(cfg),
		Nodes:               h.nodeRows(cfg),
		Settings:            h.settingsView(session, cfg, "", ""),
		Logs:                logs,
		AgentQuotaLogs:      h.opts.Diagnostics.Query("", "agent-quota", 20),
		AgentQuotaAuditLogs: h.agentQuotaAuditRows(20),
		Displays:            h.displayViews(r, cfg),
		ServerBaseURL:       adminServerBaseURL(r),
		PrimaryDisplayURL:   adminServerBaseURL(r) + cfg.Display.PadPath,
	}
	if h.opts.AgentQuotaHealth != nil {
		if health := h.opts.AgentQuotaHealth(); health.ManualTest != nil {
			view.AgentQuotaTest = h.agentQuotaTestView(cfg, *health.ManualTest)
		}
	}
	return view, nil
}

func adminServerBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: r.Host}).String()
}

func (h *AdminHandler) displayViews(r *http.Request, cfg config.Config) []adminDisplayView {
	base := adminServerBaseURL(r)
	return []adminDisplayView{
		{ID: "pad", Label: "Pad Display", Description: "桌面 / 手机的主状态看板", Path: cfg.Display.PadPath, URL: base + cfg.Display.PadPath, Field: "pad_path"},
		{ID: "kindle-right", Label: "Kindle · Right", Description: "Kindle 横向右侧安装方向", Path: cfg.Display.KindleRightPath, URL: base + cfg.Display.KindleRightPath, Field: "kindle_right_path"},
		{ID: "kindle-left", Label: "Kindle · Left", Description: "Kindle 横向左侧安装方向", Path: cfg.Display.KindleLeftPath, URL: base + cfg.Display.KindleLeftPath, Field: "kindle_left_path"},
	}
}

func (h *AdminHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) || !exactFormFields(r, "csrf", "console_refresh_seconds", "diagnostics_min_level", "diagnostics_capacity") {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid settings", http.StatusBadRequest)
		}
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.logger.Error("admin: load config failed", "err", "config_unreadable")
		h.httpUnavailable(w)
		return
	}
	refresh, refreshErr := strconv.Atoi(r.PostFormValue("console_refresh_seconds"))
	capacity, capacityErr := strconv.Atoi(r.PostFormValue("diagnostics_capacity"))
	level := strings.ToLower(strings.TrimSpace(r.PostFormValue("diagnostics_min_level")))
	if refreshErr != nil || capacityErr != nil {
		view, viewErr := h.consoleView(r, session, cfg, "", "Operator settings must use numeric values.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	next := cfg
	next.Operator = config.OperatorConfig{ConsoleRefreshSeconds: refresh, DiagnosticsMinLevel: level, DiagnosticsCapacity: capacity}
	if strings.TrimSpace(next.Admin.PasswordFile) == "" {
		next.Admin.PasswordFile = h.opts.PasswordFile
	}
	if err := h.opts.SaveConfig(h.opts.ConfigPath, next); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "settings_rejected")
		view, viewErr := h.consoleView(r, session, cfg, "", "Operator settings rejected.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	h.opts.Diagnostics.SetPolicy(capacity, level)
	h.opts.Diagnostics.Record("info", "admin", "settings_saved")
	view, viewErr := h.consoleView(r, session, next, "Operator settings saved. The Hub will restart to apply this setting.", "")
	if viewErr != nil {
		http.Error(w, viewErr.Error(), http.StatusBadRequest)
		return
	}
	view.RestartPending = true
	h.render(w, http.StatusOK, view)
	h.requestRestart()
}

func (h *AdminHandler) handleDisplaySettings(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) || !exactFormFields(r, "csrf", "pad_path", "kindle_right_path", "kindle_left_path") {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid display settings", http.StatusBadRequest)
		}
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	next := cfg
	next.Display.PadPath = strings.TrimSpace(r.PostFormValue("pad_path"))
	next.Display.KindleRightPath = strings.TrimSpace(r.PostFormValue("kindle_right_path"))
	next.Display.KindleLeftPath = strings.TrimSpace(r.PostFormValue("kindle_left_path"))
	if err := h.opts.SaveConfig(h.opts.ConfigPath, next); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "display_settings_rejected")
		view, viewErr := h.consoleView(r, session, cfg, "", "Display paths rejected: use unique URL suffixes such as /display or /kindle/R.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "display_settings_saved")
	h.logger.Info("admin: display paths saved, requesting restart")
	view, viewErr := h.consoleView(r, session, next, "Display paths saved. The Hub will restart to activate the new links.", "")
	if viewErr != nil {
		http.Error(w, viewErr.Error(), http.StatusBadRequest)
		return
	}
	view.RestartPending = true
	h.render(w, http.StatusOK, view)
	h.requestRestart()
}

func (h *AdminHandler) handleAgentQuotaSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) || !exactAgentQuotaFormFields(r) {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid agent quota settings", http.StatusBadRequest)
		}
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	enabled, enabledErr := strconv.ParseBool(r.PostFormValue("agent_quota_enabled"))
	if enabledErr != nil {
		view, viewErr := h.consoleView(r, session, cfg, "", "Agent quota settings must use valid values.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	schedules := append([]string(nil), r.PostForm["agent_quota_schedule"]...)
	// Accept the old one-field representation during the transition so an
	// older open Admin page can still be submitted once after an upgrade.
	if len(schedules) == 0 {
		if raw := strings.TrimSpace(r.PostFormValue("agent_quota_schedules")); raw != "" {
			for _, schedule := range strings.Split(raw, ",") {
				schedules = append(schedules, strings.TrimSpace(schedule))
			}
		}
	}
	next := cfg
	next.AgentQuota = config.AgentQuotaConfig{
		Enabled:   enabled,
		Provider:  strings.ToLower(strings.TrimSpace(r.PostFormValue("agent_quota_provider"))),
		Endpoint:  strings.TrimSpace(r.PostFormValue("agent_quota_endpoint")),
		Model:     strings.TrimSpace(r.PostFormValue("agent_quota_model")),
		Schedules: schedules,
	}
	if err := config.Validate(next); err != nil {
		view, viewErr := h.consoleView(r, session, cfg, "", "Agent quota settings rejected: "+err.Error())
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	if key := strings.TrimSpace(r.PostFormValue("agent_quota_api_key")); key != "" {
		if err := agentquota.SaveAPIKey(h.opts.AgentQuotaKeyFile, key); err != nil {
			view, viewErr := h.consoleView(r, session, cfg, "", "Agent quota API key was not saved: keep the Hub data directory private.")
			if viewErr != nil {
				http.Error(w, viewErr.Error(), http.StatusBadRequest)
				return
			}
			h.render(w, http.StatusBadRequest, view)
			return
		}
	}
	if err := h.opts.SaveConfig(h.opts.ConfigPath, next); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "agent_quota_settings_rejected")
		view, viewErr := h.consoleView(r, session, cfg, "", "Agent quota settings could not be saved.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "agent_quota_settings_saved")
	view, viewErr := h.consoleView(r, session, next, "Agent quota schedule saved. The independent activator will reload it within a few seconds; the Hub stayed online.", "")
	if viewErr != nil {
		http.Error(w, viewErr.Error(), http.StatusBadRequest)
		return
	}
	h.render(w, http.StatusOK, view)
}

func (h *AdminHandler) handleAgentQuotaTest(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) || !exactFormFields(r, "csrf") {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid agent quota test request", http.StatusBadRequest)
		}
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	// A double click or two browser tabs must not create two real model
	// requests. The actual request is made only by the independent activator.
	h.agentQuotaTestMu.Lock()
	defer h.agentQuotaTestMu.Unlock()
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	if _, err := agentquota.QueueManualRequest(h.opts.AgentQuotaControlFile); err != nil {
		view, viewErr := h.consoleView(r, session, cfg, "", "Manual activation test could not be queued. It may already be waiting for the activator.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusConflict, view)
		return
	}
	view, viewErr := h.consoleView(r, session, cfg, "", "")
	if viewErr != nil {
		http.Error(w, viewErr.Error(), http.StatusBadRequest)
		return
	}
	view.AgentQuotaTest = adminAgentQuotaTestView{Attempted: true, State: "QUEUED · WAITING FOR ACTIVATOR", StateClass: "is-stale", Message: "The independent activator will run this test and record the verified result in the persistent audit log."}
	h.render(w, http.StatusOK, view)
}

func (h *AdminHandler) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	configured, err := h.passwordConfigured()
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	if configured {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	h.render(w, http.StatusOK, adminView{SetupRequired: true})
}

func (h *AdminHandler) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !parseManagedForm(w, r) || !exactFormFields(r, "password", "confirm_password") {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid password setup", http.StatusBadRequest)
		}
		return
	}
	password, confirm := r.PostFormValue("password"), r.PostFormValue("confirm_password")
	if errMsg := validateAdminPasswordPair(password, confirm); errMsg != "" {
		h.render(w, http.StatusBadRequest, adminView{SetupRequired: true, SetupError: errMsg})
		return
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	configured, err := h.passwordConfigured()
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	if configured {
		h.render(w, http.StatusConflict, adminView{LoginError: "Admin password is already configured."})
		return
	}
	record, err := hashAdminPassword(password)
	if err != nil || saveAdminPassword(h.opts.PasswordFile, record) != nil {
		h.logger.Error("admin: password setup failed", "err", "password_file_unavailable")
		h.render(w, http.StatusInternalServerError, adminView{SetupRequired: true, SetupError: "Admin password could not be saved."})
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "password_initialized")
	h.logger.Info("admin: password initialized")
	if !h.setSessionCookie(w, r) {
		return
	}
	http.Redirect(w, r, "/admin/overview", http.StatusSeeOther)
}

func (h *AdminHandler) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) || !exactFormFields(r, "csrf", "current_password", "new_password", "confirm_password") {
		if w.Header().Get("Content-Type") == "" {
			http.Error(w, "invalid password change", http.StatusBadRequest)
		}
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	newPassword, confirm := r.PostFormValue("new_password"), r.PostFormValue("confirm_password")
	if errMsg := validateAdminPasswordPair(newPassword, confirm); errMsg != "" {
		view, viewErr := h.consoleView(r, session, cfg, "", errMsg)
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusBadRequest)
			return
		}
		h.render(w, http.StatusBadRequest, view)
		return
	}
	h.mutation.Lock()
	defer h.mutation.Unlock()
	record, configured, err := loadAdminPassword(h.opts.PasswordFile)
	if err != nil || !configured || !verifyAdminPassword(record, r.PostFormValue("current_password")) {
		h.opts.Diagnostics.Record("warn", "admin", "password_change_rejected")
		view, viewErr := h.consoleView(r, session, cfg, "", "Current admin password is incorrect.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusUnauthorized)
			return
		}
		h.render(w, http.StatusUnauthorized, view)
		return
	}
	nextRecord, err := hashAdminPassword(newPassword)
	if err != nil || saveAdminPassword(h.opts.PasswordFile, nextRecord) != nil {
		h.logger.Error("admin: password change failed", "err", "password_file_unavailable")
		view, viewErr := h.consoleView(r, session, cfg, "", "Admin password could not be saved.")
		if viewErr != nil {
			http.Error(w, viewErr.Error(), http.StatusInternalServerError)
			return
		}
		h.render(w, http.StatusInternalServerError, view)
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "password_changed")
	h.logger.Info("admin: password changed")
	view, viewErr := h.consoleView(r, session, cfg, "Admin password updated.", "")
	if viewErr != nil {
		http.Error(w, viewErr.Error(), http.StatusBadRequest)
		return
	}
	h.render(w, http.StatusOK, view)
}

func validateAdminPasswordPair(password, confirm string) string {
	if !validAdminPassword(password) {
		return fmt.Sprintf("Admin password must not be empty and must be at most %d characters.", adminPasswordMaxLength)
	}
	if password != confirm {
		return "Admin password confirmation does not match."
	}
	return ""
}

func (h *AdminHandler) passwordConfigured() (bool, error) {
	_, configured, err := loadAdminPassword(h.opts.PasswordFile)
	return configured, err
}

func (h *AdminHandler) setSessionCookie(w http.ResponseWriter, r *http.Request) bool {
	expiry := h.opts.Now().Add(adminSessionTTL)
	nonce, err := randomHex(16)
	if err != nil {
		h.httpUnavailable(w)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name: "devboard_admin", Value: h.cookieValue(expiry, nonce), Path: "/admin",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		MaxAge: int(adminSessionTTL.Seconds()),
	})
	return true
}

func exactFormFields(r *http.Request, allowed ...string) bool {
	want := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		want[key] = struct{}{}
		if len(r.PostForm[key]) != 1 {
			return false
		}
	}
	for key := range r.PostForm {
		if _, ok := want[key]; !ok {
			return false
		}
	}
	return true
}

func exactAgentQuotaFormFields(r *http.Request) bool {
	required := []string{"csrf", "agent_quota_enabled", "agent_quota_provider", "agent_quota_endpoint", "agent_quota_model", "agent_quota_api_key"}
	allowed := make(map[string]struct{}, len(required)+2)
	for _, key := range required {
		allowed[key] = struct{}{}
		if len(r.PostForm[key]) != 1 {
			return false
		}
	}
	// The new editor submits one checked value per selected time. The old
	// comma-separated field remains accepted for one transition so an already
	// open Admin page can still be submitted after an upgrade.
	if len(r.PostForm["agent_quota_schedule"]) > 12 || len(r.PostForm["agent_quota_schedules"]) > 1 {
		return false
	}
	if len(r.PostForm["agent_quota_schedule"]) > 0 && len(r.PostForm["agent_quota_schedules"]) > 0 {
		return false
	}
	allowed["agent_quota_schedule"] = struct{}{}
	allowed["agent_quota_schedules"] = struct{}{}
	for key := range r.PostForm {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func (h *AdminHandler) requestRestart() {
	if h.opts.RequestRestart == nil {
		return
	}
	if h.opts.RestartDelay <= 0 {
		h.opts.RequestRestart()
		return
	}
	time.AfterFunc(h.opts.RestartDelay, h.opts.RequestRestart)
}

func (h *AdminHandler) httpUnavailable(w http.ResponseWriter) {
	http.Error(w, "admin unavailable", http.StatusInternalServerError)
}

func (h *AdminHandler) settingsView(session adminSession, cfg config.Config, message, errMsg string) adminSettingsView {
	scheduleRows := make([]adminScheduleRow, 0, len(cfg.AgentQuota.Schedules))
	for _, schedule := range cfg.AgentQuota.Schedules {
		scheduleRows = append(scheduleRows, adminScheduleRow{Value: schedule})
	}
	return adminSettingsView{CSRF: h.csrfToken(session), ConsoleRefreshSeconds: cfg.Operator.ConsoleRefreshSeconds,
		DiagnosticsMinLevel: cfg.Operator.DiagnosticsMinLevel, DiagnosticsCapacity: cfg.Operator.DiagnosticsCapacity,
		PadPath: cfg.Display.PadPath, KindleRightPath: cfg.Display.KindleRightPath, KindleLeftPath: cfg.Display.KindleLeftPath,
		AgentQuotaEnabled: cfg.AgentQuota.Enabled, AgentQuotaProvider: cfg.AgentQuota.Provider,
		AgentQuotaEndpoint: cfg.AgentQuota.Endpoint, AgentQuotaModel: cfg.AgentQuota.Model,
		AgentQuotaSchedules:     strings.Join(cfg.AgentQuota.Schedules, ", "),
		AgentQuotaScheduleRows:  scheduleRows,
		AgentQuotaKeyConfigured: privateReady(h.opts.AgentQuotaKeyFile),
		PasswordConfigured:      h.passwordReady(),
		Message:                 message, Err: errMsg}
}

func (h *AdminHandler) agentQuotaTestView(cfg config.Config, health agentquota.Health) adminAgentQuotaTestView {
	view := adminAgentQuotaTestView{
		Attempted:  health.LastAttemptAt != nil,
		Verified:   health.State == "healthy" && strings.TrimSpace(health.LastResponsePreview) != "",
		Model:      cfg.AgentQuota.Model,
		Response:   health.LastResponsePreview,
		Usage:      health.LastUsageSummary,
		Message:    health.Message,
		ReceivedAt: fmtOptionalTimeInTimezone(health.LastResponseAt, cfg.Server.Timezone),
	}
	if health.LastHTTPStatus > 0 {
		view.HTTPStatus = strconv.Itoa(health.LastHTTPStatus)
	}
	if view.Verified {
		view.State = "SUCCESS · RESPONSE RECEIVED"
		view.StateClass = "is-online"
	} else {
		view.State = "FAILED · RESPONSE NOT VERIFIED"
		view.StateClass = "is-offline"
		if view.Message == "" {
			view.Message = "No detailed model response was received."
		}
	}
	return view
}

func (h *AdminHandler) passwordReady() bool {
	configured, err := h.passwordConfigured()
	return err == nil && configured
}

func (h *AdminHandler) logsView(r *http.Request, session adminSession, cfg config.Config) (adminLogsView, error) {
	query := r.URL.Query()
	for key := range query {
		if key != "level" && key != "component" && key != "limit" {
			return adminLogsView{}, fmt.Errorf("unsupported diagnostics filter")
		}
		if len(query[key]) != 1 {
			return adminLogsView{}, fmt.Errorf("duplicate diagnostics filter")
		}
	}
	level := strings.ToLower(strings.TrimSpace(query.Get("level")))
	component := strings.ToLower(strings.TrimSpace(query.Get("component")))
	if level != "" {
		if _, ok := diagnosticLevels[level]; !ok {
			return adminLogsView{}, fmt.Errorf("unsupported diagnostics level")
		}
	}
	if component != "" && !diagnosticComponentAllowed(component) {
		return adminLogsView{}, fmt.Errorf("unsupported diagnostics component")
	}
	limit := cfg.Operator.DiagnosticsCapacity
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > cfg.Operator.DiagnosticsCapacity {
			return adminLogsView{}, fmt.Errorf("diagnostics limit is out of range")
		}
		limit = parsed
	}
	return adminLogsView{CSRF: h.csrfToken(session), Diagnostics: h.opts.Diagnostics.Query(level, component, limit), Level: level, Component: component, Limit: limit, Components: diagnosticComponents()}, nil
}

func diagnosticComponentAllowed(component string) bool {
	for _, allowed := range diagnosticComponents() {
		if component == allowed {
			return true
		}
	}
	return false
}

func (h *AdminHandler) overview(cfg config.Config) adminOverviewView {
	view := adminOverviewView{Role: "hub", Health: "unavailable", HealthClass: "is-offline", Version: h.opts.ProductVersion, Commit: h.opts.GitCommit,
		ConfigReady: privateReady(h.opts.ConfigPath), AdminCredentialReady: privateReady(h.opts.TokenFile), AdminPasswordReady: h.passwordReady(),
		PersistentReady: privateDirReady(filepath.Dir(h.opts.ConfigPath))}
	view.AgentQuota = adminAgentQuotaView{
		Enabled: cfg.AgentQuota.Enabled, Provider: strings.ToUpper(cfg.AgentQuota.Provider),
		State: "disabled", Message: "Agent quota activation is disabled.",
		KeyConfigured: privateReady(h.opts.AgentQuotaKeyFile),
	}
	if cfg.AgentQuota.Enabled {
		view.AgentQuota.State = "configuration_required"
		view.AgentQuota.Message = "GLM activation is enabled and waiting for runtime configuration."
	}
	if h.opts.AgentQuotaHealth != nil {
		health := h.opts.AgentQuotaHealth()
		view.AgentQuota.Enabled = health.Enabled
		view.AgentQuota.Provider = strings.ToUpper(health.Provider)
		view.AgentQuota.State = health.State
		view.AgentQuota.Message = health.Message
		view.AgentQuota.NextRun = fmtOptionalTimeInTimezone(health.NextRunAt, cfg.Server.Timezone)
		view.AgentQuota.LastAttempt = fmtOptionalTimeInTimezone(health.LastAttemptAt, cfg.Server.Timezone)
		view.AgentQuota.LastSuccess = fmtOptionalTimeInTimezone(health.LastSuccessAt, cfg.Server.Timezone)
	}
	if view.ConfigReady && view.AdminCredentialReady && view.PersistentReady && view.AdminPasswordReady {
		if h.opts.RuntimeReady {
			view.Health, view.HealthClass = "healthy", "is-online"
		} else {
			view.Health, view.HealthClass = "degraded", "is-stale"
		}
	} else if view.ConfigReady && view.AdminCredentialReady && view.PersistentReady && !view.AdminPasswordReady {
		view.Health, view.HealthClass = "setup required", "is-stale"
	}
	if view.Version == "" {
		view.Version = "unknown"
	}
	if view.Commit == "" {
		view.Commit = "unknown"
	}
	if elapsed := h.opts.Now().Sub(h.opts.StartedAt); elapsed >= 0 {
		view.Uptime = formatUptime(elapsed)
	} else {
		view.Uptime = "unknown"
	}
	view.Registered = len(cfg.Nodes.Registered)
	for _, node := range cfg.Nodes.Registered {
		if !containsString(cfg.Nodes.Disabled, node.NodeID) {
			view.Enabled++
		}
	}
	if h.opts.Nodes == nil {
		view.Offline = view.Registered
	} else {
		dash := h.opts.Nodes.Dashboard(h.opts.Now().UTC())
		for _, host := range dash.Hosts {
			switch string(host.Source.Status) {
			case "online":
				view.Online++
			case "stale":
				view.Stale++
			default:
				view.Offline++
			}
			if host.Source.LastSuccessAt != nil && (view.LastSnapshot == "" || host.Source.LastSuccessAt.After(parseOptionalTime(view.LastSnapshot))) {
				view.LastSnapshot = fmtOptionalTime(host.Source.LastSuccessAt)
			}
		}
	}
	if view.LastSnapshot == "" {
		view.LastSnapshot = "No accepted snapshot"
	}
	view.NodeStatusSegments, view.NodeChartStyle = nodeStatusChart(view)
	view.ReadinessSegments, view.ReadinessChartStyle = readinessChart(view)
	return view
}

func fmtOptionalTimeInTimezone(value *time.Time, timezone string) string {
	if value == nil {
		return "—"
	}
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.UTC
	}
	return value.In(loc).Format("2006-01-02 15:04:05 MST")
}

func nodeStatusChart(view adminOverviewView) ([]adminChartSegment, string) {
	total := view.Online + view.Stale + view.Offline
	if total == 0 {
		return []adminChartSegment{{Label: "No Nodes", Value: "0", Percent: "100", Remainder: "0", Offset: "0", Class: "is-empty"}}, ""
	}
	segments := []adminChartSegment{
		{Label: "Online", Value: strconv.Itoa(view.Online), Class: "is-online"},
		{Label: "Stale", Value: strconv.Itoa(view.Stale), Class: "is-stale"},
		{Label: "Offline", Value: strconv.Itoa(view.Offline), Class: "is-offline"},
	}
	return chartSegments(segments, total, "#35c98b", "#f3b64d", "#e06a6a")
}

func readinessChart(view adminOverviewView) ([]adminChartSegment, string) {
	segments := []adminChartSegment{
		{Label: "Config", Value: readinessValue(view.ConfigReady), Class: readinessClass(view.ConfigReady)},
		{Label: "Node API", Value: readinessValue(view.AdminCredentialReady), Class: readinessClass(view.AdminCredentialReady)},
		{Label: "Admin password", Value: readinessValue(view.AdminPasswordReady), Class: readinessClass(view.AdminPasswordReady)},
		{Label: "Persistent directory", Value: readinessValue(view.PersistentReady), Class: readinessClass(view.PersistentReady)},
	}
	for i := range segments {
		segments[i].Percent = "25"
	}
	return segments, ""
}

func chartSegments(segments []adminChartSegment, total int, colors ...string) ([]adminChartSegment, string) {
	parts := make([]string, 0, len(segments))
	start := 0.0
	for i := range segments {
		percent := float64(0)
		if total > 0 {
			value, _ := strconv.Atoi(segments[i].Value)
			percent = float64(value) / float64(total) * 100
		}
		segments[i].Percent = fmt.Sprintf("%.2f", percent)
		segments[i].Remainder = fmt.Sprintf("%.2f", 100-percent)
		segments[i].Offset = fmt.Sprintf("-%.2f", start)
		end := start + percent
		color := "#d8e1ea"
		if i < len(colors) {
			color = colors[i]
		}
		parts = append(parts, fmt.Sprintf("%s %.2f%% %.2f%%", color, start, end))
		start = end
	}
	return segments, "background: conic-gradient(" + strings.Join(parts, ", ") + ")"
}

func readinessValue(ready bool) string {
	if ready {
		return "Ready"
	}
	return "Needs attention"
}

func readinessClass(ready bool) string {
	if ready {
		return "is-online"
	}
	return "is-stale"
}

func parseOptionalTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatUptime(d time.Duration) string {
	seconds := int64(d / time.Second)
	return fmt.Sprintf("%dd %02dh %02dm", seconds/86400, (seconds/3600)%24, (seconds/60)%60)
}

func privateReady(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0
}

func privateDirReady(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() && info.Mode().Perm()&0o077 == 0
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (h *AdminHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) {
		return
	}
	if !exactFormFields(r, "password") {
		http.Error(w, "invalid login form", http.StatusBadRequest)
		return
	}
	record, configured, err := loadAdminPassword(h.opts.PasswordFile)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	if !configured {
		h.render(w, http.StatusConflict, adminView{SetupRequired: true, SetupError: "Set an admin password before signing in."})
		return
	}
	// Password verification is constant-time over a salted, iterated record;
	// the password itself is never logged and never echoed.
	if !verifyAdminPassword(record, r.PostFormValue("password")) {
		h.opts.Diagnostics.Record("warn", "admin", "login_rejected")
		h.logger.Warn("admin: login rejected")
		h.render(w, http.StatusUnauthorized, adminView{LoginError: "Invalid admin password."})
		return
	}
	if !h.setSessionCookie(w, r) {
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "login_accepted")
	h.logger.Info("admin: login accepted")
	http.Redirect(w, r, "/admin/overview", http.StatusSeeOther)
}

func (h *AdminHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) {
		return
	}
	session, ok := h.session(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "devboard_admin", Value: "", Path: "/admin", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) handleMutation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !parseManagedForm(w, r) {
		return
	}
	session, ok := h.session(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	// Serialize the complete load-modify-save transaction. SaveAtomic
	// serializes individual renames process-wide, but without this handler
	// lock two concurrent admin requests could both load the same registry
	// and the later rename could silently lose the earlier mutation.
	h.mutation.Lock()
	defer h.mutation.Unlock()

	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.logger.Error("admin: load config failed", "err", "config_unreadable")
		http.Error(w, "admin unavailable", http.StatusInternalServerError)
		return
	}

	var resultToken, resultFor, message string
	switch r.URL.Path {
	case "/admin/nodes/add":
		nodeID := strings.TrimSpace(r.PostFormValue("node_id"))
		display := strings.TrimSpace(r.PostFormValue("display_name"))
		accent := strings.ToLower(strings.TrimSpace(r.PostFormValue("accent")))
		token, err := newNodeToken()
		if err != nil {
			http.Error(w, "admin unavailable", http.StatusInternalServerError)
			return
		}
		cfg.Nodes.Registered = append(cfg.Nodes.Registered, config.NodeConfig{NodeID: nodeID, DisplayName: display, Token: token, Accent: accent})
		resultToken, resultFor = token, nodeID
	case "/admin/nodes/reset":
		nodeID := strings.TrimSpace(r.PostFormValue("node_id"))
		token, err := newNodeToken()
		if err != nil {
			http.Error(w, "admin unavailable", http.StatusInternalServerError)
			return
		}
		found := false
		for i := range cfg.Nodes.Registered {
			if cfg.Nodes.Registered[i].NodeID == nodeID {
				cfg.Nodes.Registered[i].Token = token
				found = true
				break
			}
		}
		if !found {
			h.renderManagement(w, r, session, cfg, "", fmt.Sprintf("unknown node %q", nodeID))
			return
		}
		resultToken, resultFor = token, nodeID
	case "/admin/nodes/enable", "/admin/nodes/disable":
		nodeID := strings.TrimSpace(r.PostFormValue("node_id"))
		disable := r.URL.Path == "/admin/nodes/disable"
		known := false
		for _, node := range cfg.Nodes.Registered {
			if node.NodeID == nodeID {
				known = true
				break
			}
		}
		if !known {
			h.renderManagement(w, r, session, cfg, "", fmt.Sprintf("unknown node %q", nodeID))
			return
		}
		cfg.Nodes.Disabled = setDisabled(cfg.Nodes.Disabled, nodeID, disable)
		if disable {
			message = "Node " + nodeID + " disabled."
		} else {
			message = "Node " + nodeID + " enabled."
		}
	case "/admin/nodes/accent":
		nodeID := strings.TrimSpace(r.PostFormValue("node_id"))
		accent := strings.ToLower(strings.TrimSpace(r.PostFormValue("accent")))
		known := false
		for i := range cfg.Nodes.Registered {
			if cfg.Nodes.Registered[i].NodeID != nodeID {
				continue
			}
			known = true
			cfg.Nodes.Registered[i].Accent = accent
			break
		}
		if !known {
			h.renderManagement(w, r, session, cfg, "", fmt.Sprintf("unknown node %q", nodeID))
			return
		}
		message = "Display accent saved for " + nodeID + "."
	}

	if err := h.opts.SaveConfig(h.opts.ConfigPath, cfg); err != nil {
		// Rejected mutation (validation or write failure): nothing was
		// replaced on disk, no restart is requested, and no token is shown.
		h.logger.Warn("admin: mutation rejected", "err", "validation")
		h.opts.Diagnostics.Record("warn", "admin", "registry_rejected")
		h.renderManagement(w, r, session, cfg, "", "Registry change rejected: validation failed.")
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "registry_saved")
	h.logger.Info("admin: registry saved, requesting restart")
	if resultToken != "" {
		// One-time token display: exactly the mutation result, never a
		// normal admin page, never a log line.
		h.render(w, http.StatusOK, adminView{Page: "nodes", CSRF: h.csrfToken(session), Result: resultToken, ResultFor: resultFor, Nodes: h.nodeRows(cfg), Message: message, Displays: h.displayViews(r, cfg), ServerBaseURL: adminServerBaseURL(r), PrimaryDisplayURL: adminServerBaseURL(r) + cfg.Display.PadPath})
	} else if r.URL.Path == "/admin/nodes/enable" || r.URL.Path == "/admin/nodes/disable" {
		view, viewErr := h.consoleView(r, session, cfg, "Registry change saved. "+message+" The Hub will restart to apply this registry change. Node status will repopulate after the Hub returns and Nodes resume outbound snapshots.", "")
		if viewErr != nil {
			h.httpUnavailable(w)
			return
		}
		view.RestartPending = true
		h.render(w, http.StatusOK, view)
	} else {
		h.renderManagement(w, r, session, cfg, message, "")
	}
	h.requestRestart()
}

func (h *AdminHandler) renderManagement(w http.ResponseWriter, r *http.Request, session adminSession, cfg config.Config, message, errMsg string) {
	view, err := h.consoleView(r, session, cfg, message, errMsg)
	if err != nil {
		h.httpUnavailable(w)
		return
	}
	view.RestartPending = message != ""
	h.render(w, http.StatusOK, view)
}

func (h *AdminHandler) render(w http.ResponseWriter, status int, view adminView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates.ExecuteTemplate(w, "admin.html", view); err != nil {
		h.logger.Error("admin: render failed")
	}
}

// nodeRows builds the management table. Status and last success come from
// the push-native node store; the raw registry token never enters a row.
func (h *AdminHandler) nodeRows(cfg config.Config) []adminNodeRow {
	disabled := make(map[string]bool, len(cfg.Nodes.Disabled))
	for _, id := range cfg.Nodes.Disabled {
		disabled[id] = true
	}
	status := make(map[string]struct {
		status string
		last   *time.Time
	})
	if h.opts.Nodes != nil {
		dash := h.opts.Nodes.Dashboard(h.opts.Now().UTC())
		for _, host := range dash.Hosts {
			status[host.ConfiguredHostID] = struct {
				status string
				last   *time.Time
			}{status: string(host.Source.Status), last: host.Source.LastSuccessAt}
		}
	}
	rows := make([]adminNodeRow, 0, len(cfg.Nodes.Registered))
	for _, node := range cfg.Nodes.Registered {
		s := status[node.NodeID]
		rows = append(rows, adminNodeRow{
			ID:          node.NodeID,
			DisplayName: node.DisplayName,
			Accent:      node.Accent,
			Enabled:     !disabled[node.NodeID],
			Status:      s.status,
			LastSuccess: fmtOptionalTime(s.last),
		})
	}
	return rows
}

// session validates the HMAC-signed admin cookie. Sessions survive hub
// restarts because they are signed by the same secret file.
func (h *AdminHandler) session(r *http.Request) (adminSession, bool) {
	cookie, err := r.Cookie("devboard_admin")
	if err != nil || cookie.Value == "" {
		return adminSession{}, false
	}
	parts := strings.Split(cookie.Value, "|")
	if len(parts) != 4 || parts[0] != "v1" {
		return adminSession{}, false
	}
	expiryUnix, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return adminSession{}, false
	}
	if h.opts.Now().After(expiryUnix) {
		return adminSession{}, false
	}
	want := h.mac("admin-session", parts[1], parts[2])
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[3])) != 1 {
		return adminSession{}, false
	}
	return adminSession{expiry: expiryUnix, nonce: parts[2]}, true
}

// csrfToken derives the mutation CSRF token from the authenticated session
// values: it is bound to the session and stable until the session expires.
func (h *AdminHandler) csrfToken(s adminSession) string {
	return h.mac("admin-csrf", s.expiry.UTC().Format(time.RFC3339), s.nonce)
}

func (h *AdminHandler) cookieValue(expiry time.Time, nonce string) string {
	exp := expiry.UTC().Format(time.RFC3339)
	return "v1|" + exp + "|" + nonce + "|" + h.mac("admin-session", exp, nonce)
}

func (h *AdminHandler) mac(label, expiry, nonce string) string {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte(label + "|" + expiry + "|" + nonce))
	return hex.EncodeToString(m.Sum(nil))
}

func setDisabled(list []string, nodeID string, disable bool) []string {
	out := make([]string, 0, len(list)+1)
	present := false
	for _, id := range list {
		if id == nodeID {
			present = true
			if disable {
				out = append(out, id)
			}
			continue
		}
		out = append(out, id)
	}
	if disable && !present {
		out = append(out, nodeID)
	}
	return out
}

// newNodeToken generates one per-node bearer credential: 32 crypto-random
// bytes, hex encoded (M5.2 §9).
func newNodeToken() (string, error) {
	return randomHex(32)
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
