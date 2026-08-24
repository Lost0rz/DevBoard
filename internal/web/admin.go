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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
)

// adminSessionTTL bounds an authenticated admin session (M5.5A B7: 8–12h).
const adminSessionTTL = 12 * time.Hour

// adminMinSecretBytes is the minimum decoded admin secret length: the
// bootstrap procedure generates 32 crypto-random bytes (64 hex characters).
const adminMinSecretBytes = 32

// AdminOptions configures the M5.5A authenticated hub admin surface.
type AdminOptions struct {
	// ConfigPath is the hub's persistent config file; mutations edit it
	// atomically and then request a supervised restart.
	ConfigPath string
	// TokenFile holds the admin secret (openssl rand -hex 32), mode 0600.
	TokenFile string
	// Nodes exposes per-node connection status for the management page.
	Nodes *hub.NodeStateStore
	// RequestRestart is invoked after a successful atomic save.
	RequestRestart func()
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
}

// AdminHandler serves the hub /admin surface: login, node management
// (add / enable / disable / reset token). Sessions are HMAC-signed cookies
// derived from the admin secret file, so they survive hub restarts; the
// cookie never contains the raw admin token and node tokens are shown
// exactly once, in the mutation result that generated them.
type AdminHandler struct {
	opts      AdminOptions
	secret    []byte
	templates *template.Template
	logger    *slog.Logger
	mutation  sync.Mutex
}

func NewAdminHandler(opts AdminOptions) (*AdminHandler, error) {
	if err := config.RequirePrivateFile(opts.ConfigPath); err != nil {
		return nil, fmt.Errorf("admin config unreadable")
	}
	secret, err := loadAdminSecret(opts.TokenFile)
	if err != nil {
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
	return &AdminHandler{opts: opts, secret: secret, templates: t, logger: opts.Logger}, nil
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
	LoginError string
	// Management screen (authenticated).
	CSRF           string
	Nodes          []adminNodeRow
	Message        string
	Err            string
	RestartPending bool
	Result         string // one-time generated node token
	ResultFor      string // node id the token belongs to
	Overview       adminOverviewView
	Settings       adminSettingsView
	Logs           adminLogsView
}

type adminOverviewView struct {
	Role, Health, HealthClass, Version, Commit, Uptime, LastSnapshot string
	Registered, Enabled, Online, Stale, Offline                      int
	ConfigReady, AdminCredentialReady, PersistentReady               bool
}

type adminSettingsView struct {
	CSRF                  string
	ConsoleRefreshSeconds int
	DiagnosticsMinLevel   string
	DiagnosticsCapacity   int
	Message               string
	Err                   string
	RestartPending        bool
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
	case "/admin/login":
		h.handleLogin(w, r)
	case "/admin/logout":
		h.handleLogout(w, r)
	case "/admin/nodes/add", "/admin/nodes/enable", "/admin/nodes/disable", "/admin/nodes/reset":
		h.handleMutation(w, r)
	case "/admin/api/v1/nodes":
		h.handleProvision(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleProvision is the machine-readable half of the idempotent Node
// onboarding flow. It is a registry mutation, not a data-plane or remote
// control route. The admin secret is accepted only in an Authorization
// header, and the generated Node token is returned only to that authenticated
// caller so the local onboarding command can write its own config.
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
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	request.NodeID = strings.TrimSpace(request.NodeID)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
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
		token = cfg.Nodes.Registered[i].Token
		break
	}
	if created {
		token, err = newNodeToken()
		if err != nil {
			http.Error(w, "admin unavailable", http.StatusInternalServerError)
			return
		}
		cfg.Nodes.Registered = append(cfg.Nodes.Registered, config.NodeConfig{NodeID: request.NodeID, DisplayName: request.DisplayName, Token: token})
	}
	if err := h.opts.SaveConfig(h.opts.ConfigPath, cfg); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "registry_rejected")
		http.Error(w, "registry save failed", http.StatusInternalServerError)
		return
	}
	if h.opts.RequestRestart != nil {
		h.opts.RequestRestart()
	}
	h.opts.Diagnostics.Record("info", "admin", "registry_saved")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"schemaVersion": 1, "ok": true, "status": "registered", "created": created,
		"nodeId": request.NodeID, "displayName": request.DisplayName, "token": token,
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
		h.render(w, http.StatusOK, adminView{})
		return
	}
	http.Redirect(w, r, "/admin/overview", http.StatusSeeOther)
}

func (h *AdminHandler) authenticated(w http.ResponseWriter, r *http.Request) (adminSession, bool) {
	session, ok := h.session(r)
	if !ok {
		h.render(w, http.StatusOK, adminView{})
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
	view := adminView{CSRF: h.csrfToken(session), Page: strings.TrimPrefix(r.URL.Path, "/admin/"), RefreshSeconds: cfg.Operator.ConsoleRefreshSeconds}
	switch r.URL.Path {
	case "/admin/overview":
		view.Overview = h.overview(cfg)
	case "/admin/nodes":
		view.Nodes = h.nodeRows(cfg)
	case "/admin/settings":
		view.Settings = h.settingsView(session, cfg, "", "")
	case "/admin/logs":
		logs, err := h.logsView(r, session, cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		view.Logs = logs
	}
	h.render(w, http.StatusOK, view)
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
		h.render(w, http.StatusBadRequest, adminView{Page: "settings", RefreshSeconds: cfg.Operator.ConsoleRefreshSeconds, CSRF: h.csrfToken(session), Settings: h.settingsView(session, cfg, "", "Operator settings must use numeric values.")})
		return
	}
	next := cfg
	next.Operator = config.OperatorConfig{ConsoleRefreshSeconds: refresh, DiagnosticsMinLevel: level, DiagnosticsCapacity: capacity}
	if err := h.opts.SaveConfig(h.opts.ConfigPath, next); err != nil {
		h.opts.Diagnostics.Record("warn", "admin", "settings_rejected")
		h.render(w, http.StatusBadRequest, adminView{Page: "settings", RefreshSeconds: cfg.Operator.ConsoleRefreshSeconds, CSRF: h.csrfToken(session), Settings: h.settingsView(session, cfg, "", "Operator settings rejected.")})
		return
	}
	h.opts.Diagnostics.SetPolicy(capacity, level)
	h.opts.Diagnostics.Record("info", "admin", "settings_saved")
	h.render(w, http.StatusOK, adminView{Page: "settings", RefreshSeconds: next.Operator.ConsoleRefreshSeconds, CSRF: h.csrfToken(session), Settings: adminSettingsView{
		CSRF: h.csrfToken(session), ConsoleRefreshSeconds: next.Operator.ConsoleRefreshSeconds,
		DiagnosticsMinLevel: next.Operator.DiagnosticsMinLevel, DiagnosticsCapacity: next.Operator.DiagnosticsCapacity,
		Message: "Operator settings saved.", RestartPending: true,
	}})
	if h.opts.RequestRestart != nil {
		h.opts.RequestRestart()
	}
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

func (h *AdminHandler) httpUnavailable(w http.ResponseWriter) {
	http.Error(w, "admin unavailable", http.StatusInternalServerError)
}

func (h *AdminHandler) settingsView(session adminSession, cfg config.Config, message, errMsg string) adminSettingsView {
	return adminSettingsView{CSRF: h.csrfToken(session), ConsoleRefreshSeconds: cfg.Operator.ConsoleRefreshSeconds,
		DiagnosticsMinLevel: cfg.Operator.DiagnosticsMinLevel, DiagnosticsCapacity: cfg.Operator.DiagnosticsCapacity,
		Message: message, Err: errMsg}
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
		ConfigReady: privateReady(h.opts.ConfigPath), AdminCredentialReady: privateReady(h.opts.TokenFile),
		PersistentReady: privateDirReady(filepath.Dir(h.opts.ConfigPath))}
	if view.ConfigReady && view.AdminCredentialReady && view.PersistentReady {
		if h.opts.RuntimeReady {
			view.Health, view.HealthClass = "healthy", "is-online"
		} else {
			view.Health, view.HealthClass = "degraded", "is-stale"
		}
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
		return view
	}
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
	if view.LastSnapshot == "" {
		view.LastSnapshot = "No accepted snapshot"
	}
	return view
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
	// Constant-time verification over keyed digests; the secret itself is
	// never logged and never echoed.
	given := sha256.Sum256([]byte(r.PostFormValue("secret")))
	want := sha256.Sum256(h.secret)
	if subtle.ConstantTimeCompare(given[:], want[:]) != 1 {
		h.opts.Diagnostics.Record("warn", "admin", "login_rejected")
		h.logger.Warn("admin: login rejected")
		h.render(w, http.StatusUnauthorized, adminView{LoginError: "Invalid admin secret."})
		return
	}
	expiry := h.opts.Now().Add(adminSessionTTL)
	nonce, err := randomHex(16)
	if err != nil {
		http.Error(w, "admin unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "devboard_admin",
		Value:    h.cookieValue(expiry, nonce),
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(adminSessionTTL.Seconds()),
	})
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
		token, err := newNodeToken()
		if err != nil {
			http.Error(w, "admin unavailable", http.StatusInternalServerError)
			return
		}
		cfg.Nodes.Registered = append(cfg.Nodes.Registered, config.NodeConfig{NodeID: nodeID, DisplayName: display, Token: token})
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
			h.renderManagement(w, session, cfg, "", fmt.Sprintf("unknown node %q", nodeID))
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
			h.renderManagement(w, session, cfg, "", fmt.Sprintf("unknown node %q", nodeID))
			return
		}
		cfg.Nodes.Disabled = setDisabled(cfg.Nodes.Disabled, nodeID, disable)
		if disable {
			message = "Node " + nodeID + " disabled."
		} else {
			message = "Node " + nodeID + " enabled."
		}
	}

	if err := h.opts.SaveConfig(h.opts.ConfigPath, cfg); err != nil {
		// Rejected mutation (validation or write failure): nothing was
		// replaced on disk, no restart is requested, and no token is shown.
		h.logger.Warn("admin: mutation rejected", "err", "validation")
		h.opts.Diagnostics.Record("warn", "admin", "registry_rejected")
		h.renderManagement(w, session, cfg, "", "Registry change rejected: validation failed.")
		return
	}
	h.opts.Diagnostics.Record("info", "admin", "registry_saved")
	h.logger.Info("admin: registry saved, requesting restart")
	if resultToken != "" {
		// One-time token display: exactly the mutation result, never a
		// normal admin page, never a log line.
		h.render(w, http.StatusOK, adminView{Page: "nodes", CSRF: h.csrfToken(session), Result: resultToken, ResultFor: resultFor, Nodes: h.nodeRows(cfg), Message: message})
	} else if r.URL.Path == "/admin/nodes/enable" || r.URL.Path == "/admin/nodes/disable" {
		h.render(w, http.StatusOK, adminView{Page: "nodes", CSRF: h.csrfToken(session), Message: message, RestartPending: true})
	} else {
		h.renderManagement(w, session, cfg, message, "")
	}
	if h.opts.RequestRestart != nil {
		h.opts.RequestRestart()
	}
}

func (h *AdminHandler) renderManagement(w http.ResponseWriter, session adminSession, cfg config.Config, message, errMsg string) {
	h.render(w, http.StatusOK, adminView{Page: "nodes", CSRF: h.csrfToken(session), Nodes: h.nodeRows(cfg), Message: message, Err: errMsg})
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
