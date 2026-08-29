package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/navigation"
	"github.com/Lost0rz/DevBoard/internal/state"
)

//go:embed templates/*.html static/*
var templateFS embed.FS

type Server struct {
	store            *state.Store
	projector        state.ProjectionConfig
	mock             bool
	peers            *multihost.PeerSnapshotStore
	nodes            *hub.NodeStateStore
	role             config.RuntimeRole
	legacyCombined   bool
	dashboardRefresh int
	now              func() time.Time
	logger           *slog.Logger
	templates        *template.Template
	displayRoutes    displayRouteSet
	safeNavigation   bool
	navigation       NavigationDispatcher
	mux              *http.ServeMux
	handler          http.Handler
}

// NavigationDispatcher is the local Mac execution seam. Hub servers enqueue
// actions for the authenticated node; node servers dispatch directly to their
// local private target registry.
type NavigationDispatcher interface {
	DispatchNavigation(targetID string, action state.NavigationAction) (navigation.Result, error)
}

type NavigationDispatchFunc func(targetID string, action state.NavigationAction) (navigation.Result, error)

func (f NavigationDispatchFunc) DispatchNavigation(targetID string, action state.NavigationAction) (navigation.Result, error) {
	return f(targetID, action)
}

func NewServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger) (*Server, error) {
	return newServer(store, cfg, mock, logger, nil, nil, nil, config.RuntimeRoleNode, 0, false)
}

// NewServerWithDashboard preserves the historical M5 combined local+peer
// behavior for regression tests. Production M5.1 uses NewRoleServer.
func NewServerWithDashboard(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore) (*Server, error) {
	return newServer(store, cfg, mock, logger, peers, nil, nil, config.RuntimeRoleNode, 0, true)
}

// NewRoleServer builds the role server for the production NODE role. Passing
// RuntimeRoleHub with a caller-built peer store keeps the frozen M5.1 pull
// hub alive for regression tests only; production hubs must use NewHubServer.
func NewRoleServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore, role config.RuntimeRole, dashboardRefresh int) (*Server, error) {
	return newServer(store, cfg, mock, logger, peers, nil, nil, role, dashboardRefresh, false)
}

// NewHubServer builds the M5.3 production HUB server: push-native node
// dashboard plus the frozen machine write route. The hub never fabricates
// local NAS state. A non-mock hub requires a push runtime; there is no
// fallback to the legacy pull dashboard. A nil runtime is valid only for the
// synthetic mock hub.
func NewHubServer(cfg state.ProjectionConfig, mock bool, logger *slog.Logger, runtime *hub.Runtime, dashboardRefresh int) (*Server, error) {
	if runtime != nil && mock {
		return nil, fmt.Errorf("hub runtime cannot be combined with mock mode")
	}
	if runtime == nil && !mock {
		return nil, fmt.Errorf("hub server requires a push runtime")
	}
	var receiver http.Handler
	var nodes *hub.NodeStateStore
	if runtime != nil {
		receiver = runtime
		nodes = runtime.Store()
	}
	return newServer(nil, cfg, mock, logger, nil, receiver, nodes, config.RuntimeRoleHub, dashboardRefresh, false)
}

func newServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore, receiver http.Handler, nodes *hub.NodeStateStore, role config.RuntimeRole, dashboardRefresh int, legacyCombined bool) (*Server, error) {
	// Managed settings/admin templates are parsed by their own handlers with
	// their own function maps. Keeping the display parser explicit prevents a
	// newly added managed template from breaking the frozen display surfaces.
	t, err := template.New("root").Funcs(template.FuncMap{"quotaRailLabel": quotaRailLabel}).ParseFS(
		templateFS,
		"templates/display.html",
		"templates/dashboard_fragment.html",
		"templates/kindle_demo.html",
	)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	routes := displayRouteSetFor(cfg)
	s := &Server{store: store, projector: cfg, mock: mock, peers: peers, nodes: nodes, role: role, legacyCombined: legacyCombined, dashboardRefresh: dashboardRefresh, now: time.Now, logger: logger, templates: t, displayRoutes: routes}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.root)
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/api/state", s.apiState)
	mux.HandleFunc("/api/dashboard", s.apiDashboard)
	mux.HandleFunc("/api/navigation", s.apiNavigation)
	mux.HandleFunc(routes.PadPath, s.display)
	mux.HandleFunc(routes.PadPath+"/fragment", s.displayFragment)
	mux.HandleFunc("/assets/app.css", s.appCSS)
	mux.HandleFunc("/assets/dashboard.js", s.dashboardJS)
	mux.HandleFunc("/assets/admin.js", s.adminJS)
	// Kindle has one canonical e-ink surface with two physical mounting
	// directions. Keep the address short and deterministic for old WebKit;
	// the retired /display/kindle and demo aliases are deliberately absent.
	mux.HandleFunc(routes.KindleRightPath, s.kindleDemo)
	mux.HandleFunc(routes.KindleLeftPath, s.kindleDemo)
	if receiver != nil {
		mux.Handle(hub.SnapshotRoute, receiver)
		mux.Handle(hub.ActionsRoute, receiver)
		mux.Handle(hub.ActionsAckRoute, receiver)
	}
	s.mux = mux
	s.handler = mux
	return s, nil
}

type displayRouteSet struct {
	PadPath         string
	KindleRightPath string
	KindleLeftPath  string
}

func displayRouteSetFor(cfg state.ProjectionConfig) displayRouteSet {
	routes := displayRouteSet{PadPath: cfg.PadPath, KindleRightPath: cfg.KindleRightPath, KindleLeftPath: cfg.KindleLeftPath}
	if routes.PadPath == "" {
		routes.PadPath = "/display"
	}
	if routes.KindleRightPath == "" {
		routes.KindleRightPath = "/kindle/R"
	}
	if routes.KindleLeftPath == "" {
		routes.KindleLeftPath = "/kindle/L"
	}
	return routes
}
func (s *Server) Handler() http.Handler { return s.handler }

// EnableSafeNavigation turns on the task-level public references only after
// the caller has wired the corresponding hub queue or local dispatcher.
func (s *Server) EnableSafeNavigation() { s.safeNavigation = true }

func (s *Server) SetNavigationDispatcher(dispatcher NavigationDispatcher) {
	s.navigation = dispatcher
}

// AttachSettings wires the M5.5A loopback-only node settings page. It is a
// node-role surface by construction and is never available on the hub.
func (s *Server) AttachSettings(h *SettingsHandler) {
	s.mux.Handle("/settings", h)
	s.mux.Handle("/api/node/status", http.HandlerFunc(h.ServeNodeStatus))
}

// AttachAdmin wires the M5.5A authenticated hub admin surface. It is a
// hub-role surface by construction and is never available on a node.
func (s *Server) AttachAdmin(h *AdminHandler) {
	s.mux.Handle("/admin", h)
	s.mux.Handle("/admin/", h)
}
func (s *Server) publicStateAt(now time.Time) state.PublicState {
	if s.store == nil {
		return state.PublicState{}
	}
	return state.ProjectPublic(s.store.Snapshot(), state.RuntimeCapabilities{SafeNavigation: s.safeNavigation}, s.projector, now)
}
func (s *Server) dashboardStateAt(now time.Time) dashboard.State {
	if s.legacyCombined {
		local := s.publicStateAt(now)
		if s.mock {
			return multihost.MockDashboard(local, now)
		}
		if s.peers == nil {
			return multihost.NewPeerSnapshotStore(nil).Dashboard(local, now)
		}
		return s.peers.Dashboard(local, now)
	}
	if s.role == config.RuntimeRoleHub {
		if s.mock {
			return multihost.MockHubDashboard(now)
		}
		// M5.3 push-native node store is the only production hub authority;
		// NewHubServer guarantees it is present outside mock mode.
		if s.nodes != nil {
			return s.nodes.Dashboard(now)
		}
		// Frozen M5.1 pull hub, reachable only when the explicitly legacy
		// role-server constructor was handed a caller-built peer store.
		// The production hub constructor never falls back to this path.
		if s.peers != nil {
			return s.peers.DashboardPeers(now)
		}
		return dashboard.State{SchemaVersion: 1, StateKind: "dashboard", GeneratedAt: now, Hosts: []dashboard.HostSnapshot{}}
	}
	local := s.publicStateAt(now)
	return multihost.NewPeerSnapshotStore(nil).Dashboard(local, now)
}
func methodGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}
func notFoundNoStore(w http.ResponseWriter, message string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, http.StatusNotFound)
}
func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !methodGET(w, r) {
		return
	}
	http.Redirect(w, r, "/display", http.StatusFound)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "schemaVersion": 1, "role": s.role}); err != nil {
		s.logger.Error("encode health response")
	}
}
func (s *Server) apiState(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	if s.role == config.RuntimeRoleHub && !s.legacyCombined {
		notFoundNoStore(w, "local monitored state is not available on hub")
		return
	}
	instant := s.now()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(s.publicStateAt(instant.UTC())); err != nil {
		s.logger.Error("encode public state")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body.Bytes())
}
func (s *Server) apiDashboard(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	instant := s.now().UTC()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(s.dashboardStateAt(instant)); err != nil {
		s.logger.Error("encode dashboard state")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body.Bytes())
}

func (s *Server) apiNavigation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid navigation form", http.StatusBadRequest)
		return
	}
	hostID := strings.TrimSpace(r.Form.Get("host_id"))
	targetID := strings.TrimSpace(r.Form.Get("target_id"))
	taskID := strings.TrimSpace(r.Form.Get("task_id"))
	action := state.NavigationAction(strings.TrimSpace(r.Form.Get("action")))
	if hostID == "" || targetID == "" || action != state.ActionFocusAgent {
		http.Error(w, "invalid navigation request", http.StatusBadRequest)
		return
	}
	var (
		item   navigation.Action
		result navigation.Result
		err    error
	)
	if s.role == config.RuntimeRoleHub {
		if s.nodes == nil {
			err = fmt.Errorf("hub navigation is unavailable")
		} else {
			item, err = s.nodes.EnqueueNavigationForTask(hostID, targetID, taskID, action, s.now())
			if err == nil {
				result = navigation.Result{OK: true, Message: "Navigation queued for the Mac."}
			}
		}
	} else if s.navigation != nil {
		result, err = s.navigation.DispatchNavigation(targetID, action)
	} else {
		err = fmt.Errorf("local navigation is unavailable")
	}
	if err == nil && !result.OK {
		err = fmt.Errorf("%s", result.Message)
	}
	if err == nil && result.OK && taskID != "" && s.store != nil {
		// Node displays persist the acknowledgement into the next uplink
		// snapshot. Hub displays persist it when the queued Mac action is
		// acknowledged by the node.
		_ = s.store.Update(func(root *state.InternalRootState) error {
			state.AcknowledgeTask(root, taskID, targetID, s.now())
			return nil
		})
	}
	if err != nil {
		if r.Header.Get("Accept") == "application/json" {
			writeNavigationJSON(w, http.StatusConflict, navigation.Result{Message: err.Error()})
			return
		}
		navigationResultPage(w, http.StatusConflict, "Navigation unavailable", err.Error())
		return
	}
	if r.Header.Get("Accept") == "application/json" {
		payload := result
		if item.ID != "" {
			payload.Message = item.ID + " · " + payload.Message
		}
		writeNavigationJSON(w, http.StatusAccepted, payload)
		return
	}
	// Display surfaces are kiosk-like pages. A successful card click must not
	// replace them with an intermediate confirmation document; return to the
	// originating display route and let its normal refresh show the new state.
	http.Redirect(w, r, s.navigationReturnPath(r), http.StatusSeeOther)
}

func (s *Server) navigationReturnPath(r *http.Request) string {
	fallback := s.displayRoutes.PadPath
	candidate := strings.TrimSpace(r.Form.Get("return_to"))
	if candidate == "" {
		candidate = strings.TrimSpace(r.Referer())
	}
	if candidate == "" {
		return fallback
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != "" && parsed.Host == "" || parsed.Host != "" && parsed.Host != r.Host {
		return fallback
	}
	path := parsed.Path
	if path != s.displayRoutes.PadPath && path != s.displayRoutes.KindleRightPath && path != s.displayRoutes.KindleLeftPath {
		return fallback
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path
}

func writeNavigationJSON(w http.ResponseWriter, status int, result navigation.Result) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func navigationResultPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The message is deliberately fixed/opaque (action IDs and allow-listed
	// executor messages only), so no provider prompt or private path is echoed.
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>DevBoard · %s</title><body><h1>%s</h1><p>%s</p><p>Return to the display page to continue.</p></body>", template.HTMLEscapeString(title), template.HTMLEscapeString(title), template.HTMLEscapeString(message))
}
func (s *Server) display(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	instant := s.now()
	nowUTC := instant.UTC()
	vm := buildDashboardViewModelWithTimezone(s.dashboardStateAt(nowUTC), nowUTC, s.mock, s.projector.Timezone)
	vm.RefreshSeconds = s.dashboardRefresh
	vm.ProductRole = string(s.role)
	vm.FragmentPath = s.displayRoutes.PadPath + "/fragment"
	vm.ReturnPath = s.displayRoutes.PadPath
	vm.LegacyRefresh = s.legacyCombined || s.peers != nil
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "display.html", vm); err != nil {
		s.logger.Error("render display")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body.Bytes())
}

func (s *Server) displayFragment(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	instant := s.now().UTC()
	vm := buildDashboardViewModelWithTimezone(s.dashboardStateAt(instant), instant, s.mock, s.projector.Timezone)
	vm.RefreshSeconds = s.dashboardRefresh
	vm.ProductRole = string(s.role)
	vm.ReturnPath = s.displayRoutes.PadPath
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "dashboard_fragment.html", vm); err != nil {
		s.logger.Error("render dashboard fragment")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body.Bytes())
}

func (s *Server) appCSS(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, err := templateFS.ReadFile("static/app.css")
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusNotFound)
		return
	}
	_, _ = w.Write(b)
}

func (s *Server) dashboardJS(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, err := templateFS.ReadFile("static/dashboard.js")
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusNotFound)
		return
	}
	_, _ = w.Write(b)
}

func (s *Server) adminJS(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, err := templateFS.ReadFile("static/admin.js")
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusNotFound)
		return
	}
	_, _ = w.Write(b)
}
func (s *Server) kindleDemo(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	rotate, ok := s.kindleRequestRotate(r)
	if !ok {
		notFoundNoStore(w, "unknown Kindle orientation")
		return
	}
	instant := s.now()
	model := s.dashboardStateAt(instant.UTC())
	vm := buildKindleDemoViewModelWithTimezone(model, instant, s.mock, rotate, s.projector.Timezone)
	if rotate == "left" {
		vm.ReturnPath = s.displayRoutes.KindleLeftPath
	} else {
		vm.ReturnPath = s.displayRoutes.KindleRightPath
	}
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "kindle_demo.html", vm); err != nil {
		s.logger.Error("render kindle demo")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body.Bytes())
}

func (s *Server) kindleRequestRotate(r *http.Request) (string, bool) {
	if r.URL.RawQuery != "" {
		return "", false
	}
	switch strings.TrimSuffix(r.URL.Path, "/") {
	case s.displayRoutes.KindleRightPath:
		return "right", true
	case s.displayRoutes.KindleLeftPath:
		return "left", true
	default:
		return "", false
	}
}
