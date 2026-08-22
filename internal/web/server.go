package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/state"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	store            *state.Store
	projector        state.ProjectionConfig
	mock             bool
	peers            *multihost.PeerSnapshotStore
	role             config.RuntimeRole
	legacyCombined   bool
	dashboardRefresh int
	now              func() time.Time
	logger           *slog.Logger
	templates        *template.Template
	handler          http.Handler
}

func NewServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger) (*Server, error) {
	return newServer(store, cfg, mock, logger, nil, config.RuntimeRoleNode, 0, false)
}

// NewServerWithDashboard preserves the historical M5 combined local+peer
// behavior for regression tests. Production M5.1 uses NewRoleServer.
func NewServerWithDashboard(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore) (*Server, error) {
	return newServer(store, cfg, mock, logger, peers, config.RuntimeRoleNode, 0, true)
}

func NewRoleServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore, role config.RuntimeRole, dashboardRefresh int) (*Server, error) {
	return newServer(store, cfg, mock, logger, peers, role, dashboardRefresh, false)
}

func newServer(store *state.Store, cfg state.ProjectionConfig, mock bool, logger *slog.Logger, peers *multihost.PeerSnapshotStore, role config.RuntimeRole, dashboardRefresh int, legacyCombined bool) (*Server, error) {
	t, err := template.New("root").Funcs(template.FuncMap{"quotaRailLabel": quotaRailLabel}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: store, projector: cfg, mock: mock, peers: peers, role: role, legacyCombined: legacyCombined, dashboardRefresh: dashboardRefresh, now: time.Now, logger: logger, templates: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.root)
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/api/state", s.apiState)
	mux.HandleFunc("/api/dashboard", s.apiDashboard)
	mux.HandleFunc("/display", s.display)
	mux.HandleFunc("/display/kindle", s.kindle)
	s.handler = mux
	return s, nil
}
func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) publicStateAt(now time.Time) state.PublicState {
	if s.store == nil {
		return state.PublicState{}
	}
	return state.ProjectPublic(s.store.Snapshot(), state.RuntimeCapabilities{SafeNavigation: false}, s.projector, now)
}
func (s *Server) dashboardStateAt(now time.Time) multihost.DashboardState {
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
		if s.peers == nil {
			return multihost.NewPeerSnapshotStore(nil).DashboardPeers(now)
		}
		return s.peers.DashboardPeers(now)
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
		http.Error(w, "local monitored state is not available on hub", http.StatusNotFound)
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
func (s *Server) display(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	instant := s.now()
	nowUTC := instant.UTC()
	vm := buildDashboardViewModel(s.dashboardStateAt(nowUTC), nowUTC, s.mock)
	if s.role == config.RuntimeRoleHub && !s.legacyCombined {
		vm.RefreshSeconds = s.dashboardRefresh
	}
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
func (s *Server) kindle(w http.ResponseWriter, r *http.Request) {
	if !methodGET(w, r) {
		return
	}
	if s.role == config.RuntimeRoleHub && !s.legacyCombined {
		http.Error(w, "kindle display is not available on hub", http.StatusNotFound)
		return
	}
	layout := normalizeKindleLayout(r.URL.Query().Get("layout"))
	rotate := normalizeKindleRotate(r.URL.Query().Get("rotate"))
	instant := s.now()
	pub := s.publicStateAt(instant.UTC())
	vm := BuildKindleViewModel(pub, instant, s.mock, layout, rotate)
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "kindle.html", vm); err != nil {
		s.logger.Error("render kindle display")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body.Bytes())
}
