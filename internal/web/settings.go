package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

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
	Logger         *slog.Logger
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
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

	if err := config.SaveAtomic(h.opts.ConfigPath, next); err != nil {
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
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// fmtTime renders an optional instant for the settings status panel.
func fmtOptionalTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}
