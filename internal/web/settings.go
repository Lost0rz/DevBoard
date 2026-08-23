package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

const managedFormMaxBytes = 16 << 10

func parseManagedForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, managedFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	return true
}

// UplinkHealth is the operational uplink status shown on the local settings
// page (M5.2 §29): never a credential, never a hub response body.
type UplinkHealth struct {
	Connected      bool
	LastAttemptAt  *time.Time
	LastSuccessAt  *time.Time
	LastErrorClass string
}

// UplinkHealthSource decouples the settings page from the uplink runtime: the
// process wires the scheduler in, tests wire fakes.
type UplinkHealthSource interface {
	UplinkHealth() UplinkHealth
}

// SettingsOptions configures the M5.5A loopback-only node settings page.
type SettingsOptions struct {
	// ConfigPath is the node's persistent config file. The file — not
	// in-memory state — is the source of truth: every request re-loads it.
	ConfigPath string
	// HealthSource may be nil (uplink not running).
	HealthSource UplinkHealthSource
	// RequestRestart is invoked after a successful atomic save; the serve
	// loop shuts down gracefully and the supervisor restarts the process.
	RequestRestart func()
	// SaveConfig defaults to config.SaveAtomic. Tests inject a failing saver
	// to prove that persistence failures never request a restart.
	SaveConfig func(string, config.Config) error
	Logger     *slog.Logger
}

// SettingsHandler serves the node-local settings page. It is node-only,
// loopback-only and CSRF-protected; the configured bearer token is never
// returned to the page — only a yes/no "token configured" indicator.
type SettingsHandler struct {
	opts      SettingsOptions
	csrfToken string
	templates *template.Template
	logger    *slog.Logger
}

func NewSettingsHandler(opts SettingsOptions) (*SettingsHandler, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	t, err := template.New("settings").Funcs(template.FuncMap{"fmtOptionalTime": fmtOptionalTime}).ParseFS(templateFS, "templates/settings.html")
	if err != nil {
		return nil, err
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.SaveConfig == nil {
		opts.SaveConfig = config.SaveAtomic
	}
	return &SettingsHandler{opts: opts, csrfToken: hex.EncodeToString(raw[:]), templates: t, logger: opts.Logger}, nil
}

type settingsView struct {
	CSRF            string
	NodeID          string
	DisplayName     string
	Endpoint        string
	UplinkEnabled   bool
	TokenConfigured bool
	ServiceRunning  bool
	BinaryPath      string
	Health          UplinkHealth
	HasHealth       bool
	Error           string
	Saved           bool
}

func (h *SettingsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// A loopback listener alone is not enough against DNS rebinding: a browser
	// can connect to 127.0.0.1 while presenting an attacker-controlled Host
	// header. Require the request authority itself to be loopback/localhost so
	// a hostile origin cannot read the form CSRF token and submit settings.
	if !LoopbackRequestHost(r.Host) {
		http.Error(w, "settings unavailable", http.StatusForbidden)
		return
	}
	cfg, ok := h.loadCurrent(w)
	if !ok {
		return
	}
	// Security invariant: the settings surface exists only while the node
	// web listener is loopback. A non-loopback bind makes it unavailable.
	if !LoopbackHost(cfg.Server.Host) {
		http.Error(w, "settings unavailable: node server is not bound to loopback", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.renderForm(w, cfg, "")
	case http.MethodPost:
		h.submit(w, r, cfg)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type nodeStatusResponse struct {
	SchemaVersion   int        `json:"schemaVersion"`
	ServiceRunning  bool       `json:"serviceRunning"`
	NodeID          string     `json:"nodeId"`
	DisplayName     string     `json:"displayName"`
	HubEndpoint     string     `json:"hubEndpoint"`
	UplinkEnabled   bool       `json:"uplinkEnabled"`
	TokenConfigured bool       `json:"tokenConfigured"`
	UplinkRunning   bool       `json:"uplinkRunning"`
	Connected       bool       `json:"connected"`
	LastAttemptAt   *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt   *time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorClass  string     `json:"lastErrorClass"`
}

// ServeNodeStatus serves the bounded local Node status contract. It shares
// the settings page's Host and bind-address checks and intentionally exposes
// only whether a token exists, never the token itself.
func (h *SettingsHandler) ServeNodeStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !LoopbackRequestHost(r.Host) {
		http.Error(w, "node status unavailable", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, ok := h.loadCurrent(w)
	if !ok {
		return
	}
	if !LoopbackHost(cfg.Server.Host) {
		http.Error(w, "node status unavailable: node server is not bound to loopback", http.StatusServiceUnavailable)
		return
	}
	status := nodeStatusResponse{
		SchemaVersion:   1,
		ServiceRunning:  true,
		NodeID:          cfg.Host.ID,
		DisplayName:     cfg.Host.DisplayName,
		HubEndpoint:     cfg.Uplink.Endpoint,
		UplinkEnabled:   cfg.Uplink.Enabled,
		TokenConfigured: cfg.Uplink.Token != "",
		UplinkRunning:   h.opts.HealthSource != nil,
	}
	if h.opts.HealthSource != nil {
		health := h.opts.HealthSource.UplinkHealth()
		status.Connected = health.Connected
		status.LastAttemptAt = health.LastAttemptAt
		status.LastSuccessAt = health.LastSuccessAt
		status.LastErrorClass = health.LastErrorClass
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		h.logger.Error("node status: encode failed")
	}
}

func (h *SettingsHandler) loadCurrent(w http.ResponseWriter) (config.Config, bool) {
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.logger.Error("settings: load config failed", "err", "config_unreadable")
		http.Error(w, "settings unavailable", http.StatusInternalServerError)
		return config.Config{}, false
	}
	return cfg, true
}

// binaryPath reports the running DevBoard binary so provider hook configs
// can point at the stable installed path (the installer deploys to a fixed
// location; a dev run shows wherever the binary was launched from).
func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func (h *SettingsHandler) renderForm(w http.ResponseWriter, cfg config.Config, errMsg string) {
	view := settingsView{
		CSRF:            h.csrfToken,
		NodeID:          cfg.Host.ID,
		DisplayName:     cfg.Host.DisplayName,
		Endpoint:        cfg.Uplink.Endpoint,
		UplinkEnabled:   cfg.Uplink.Enabled,
		TokenConfigured: cfg.Uplink.Token != "",
		ServiceRunning:  true,
		BinaryPath:      binaryPath(),
		Error:           errMsg,
	}
	if h.opts.HealthSource != nil {
		view.Health = h.opts.HealthSource.UplinkHealth()
		view.HasHealth = true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "settings.html", view); err != nil {
		h.logger.Error("settings: render failed")
	}
}

func (h *SettingsHandler) submit(w http.ResponseWriter, r *http.Request, current config.Config) {
	if !parseManagedForm(w, r) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken)) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	next := current
	next.Host.ID = strings.TrimSpace(r.PostFormValue("node_id"))
	next.Host.DisplayName = strings.TrimSpace(r.PostFormValue("display_name"))
	next.Uplink.NodeID = next.Host.ID
	next.Uplink.Endpoint = strings.TrimSpace(r.PostFormValue("endpoint"))
	next.Uplink.Enabled = r.PostFormValue("uplink") == "on"
	// Blank token field means "keep the configured credential"; a non-blank
	// value replaces it. The value itself is never echoed back.
	if token := strings.TrimSpace(r.PostFormValue("token")); token != "" {
		next.Uplink.Token = token
	}

	if err := h.opts.SaveConfig(h.opts.ConfigPath, next); err != nil {
		// Validation failure: no file mutation, no restart, form re-rendered
		// with the operator's entered values and the bounded error.
		h.logger.Warn("settings: save rejected", "err", "validation")
		view := settingsView{
			CSRF:            h.csrfToken,
			NodeID:          next.Host.ID,
			DisplayName:     next.Host.DisplayName,
			Endpoint:        next.Uplink.Endpoint,
			UplinkEnabled:   next.Uplink.Enabled,
			TokenConfigured: current.Uplink.Token != "",
			ServiceRunning:  true,
			BinaryPath:      binaryPath(),
			Error:           err.Error(),
		}
		if h.opts.HealthSource != nil {
			view.Health = h.opts.HealthSource.UplinkHealth()
			view.HasHealth = true
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.templates.ExecuteTemplate(w, "settings.html", view)
		return
	}

	h.logger.Info("settings: config saved, requesting restart")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.templates.ExecuteTemplate(w, "settings.html", settingsView{Saved: true, ServiceRunning: true, CSRF: h.csrfToken})
	// Response is written; now ask the serve loop for a graceful restart.
	if h.opts.RequestRestart != nil {
		h.opts.RequestRestart()
	}
}

// LoopbackHost reports whether a server bind address is loopback-only
// (127.0.0.0/8, ::1 or the localhost name). Managed settings may exist only
// behind such a bind.
func LoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LoopbackRequestHost validates an HTTP Host authority, with or without a
// port and with bracketed IPv6 support, for the DNS-rebinding guard above.
func LoopbackRequestHost(authority string) bool {
	authority = strings.TrimSpace(authority)
	if host, _, err := net.SplitHostPort(authority); err == nil {
		return LoopbackHost(host)
	}
	return LoopbackHost(strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]"))
}

// fmtTime renders an optional instant for the settings status panel.
func fmtOptionalTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}
