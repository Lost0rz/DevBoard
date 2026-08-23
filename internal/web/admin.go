package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
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
	Logger     *slog.Logger
	Now        func() time.Time
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
	t, err := template.New("admin").Funcs(template.FuncMap{"fmtOptionalTime": fmtOptionalTime}).ParseFS(templateFS, "templates/admin.html")
	if err != nil {
		return nil, err
	}
	return &AdminHandler{opts: opts, secret: secret, templates: t, logger: opts.Logger}, nil
}

func loadAdminSecret(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
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
	// Login screen.
	LoginError string
	// Management screen (authenticated).
	CSRF      string
	Nodes     []adminNodeRow
	Message   string
	Err       string
	Result    string // one-time generated node token
	ResultFor string // node id the token belongs to
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
	case "/admin/login":
		h.handleLogin(w, r)
	case "/admin/logout":
		h.handleLogout(w, r)
	case "/admin/nodes/add", "/admin/nodes/enable", "/admin/nodes/disable", "/admin/nodes/reset":
		h.handleMutation(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *AdminHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := h.session(r)
	if !ok {
		h.render(w, http.StatusOK, adminView{})
		return
	}
	cfg, err := config.Load(h.opts.ConfigPath)
	if err != nil {
		h.logger.Error("admin: load config failed", "err", "config_unreadable")
		http.Error(w, "admin unavailable", http.StatusInternalServerError)
		return
	}
	h.render(w, http.StatusOK, adminView{CSRF: h.csrfToken(session), Nodes: h.nodeRows(cfg)})
}

func (h *AdminHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// Constant-time verification over keyed digests; the secret itself is
	// never logged and never echoed.
	given := sha256.Sum256([]byte(r.PostFormValue("secret")))
	want := sha256.Sum256(h.secret)
	if subtle.ConstantTimeCompare(given[:], want[:]) != 1 {
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
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(adminSessionTTL.Seconds()),
	})
	h.logger.Info("admin: login accepted")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := h.session(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(h.csrfToken(session))) != 1 {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "devboard_admin", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) handleMutation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := h.session(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
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
		h.renderManagement(w, session, cfg, "", err.Error())
		return
	}
	h.logger.Info("admin: registry saved, requesting restart")
	if resultToken != "" {
		// One-time token display: exactly the mutation result, never a
		// normal admin page, never a log line.
		h.render(w, http.StatusOK, adminView{CSRF: h.csrfToken(session), Result: resultToken, ResultFor: resultFor, Nodes: h.nodeRows(cfg), Message: message})
	} else {
		h.renderManagement(w, session, cfg, message, "")
	}
	if h.opts.RequestRestart != nil {
		h.opts.RequestRestart()
	}
}

func (h *AdminHandler) renderManagement(w http.ResponseWriter, session adminSession, cfg config.Config, message, errMsg string) {
	h.render(w, http.StatusOK, adminView{CSRF: h.csrfToken(session), Nodes: h.nodeRows(cfg), Message: message, Err: errMsg})
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
