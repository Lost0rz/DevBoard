package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agent"
	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
	"github.com/Lost0rz/DevBoard/internal/networkmetrics"
	"github.com/Lost0rz/DevBoard/internal/quota"
	"github.com/Lost0rz/DevBoard/internal/state"
	"github.com/Lost0rz/DevBoard/internal/systemmetrics"
	"github.com/Lost0rz/DevBoard/internal/uplink"
	"github.com/Lost0rz/DevBoard/internal/web"
)

// These values are development-safe defaults and are replaced for a
// provenance-bearing bundle with -ldflags from the controlled build script.
var (
	productVersion = "development"
	gitCommit      = "unknown"
)

// runtimePlan fixes production runtime authority per role. The M5.2 push
// topology removed hub-originated peer polling entirely: the HUB owns only
// the receiver/node-store authority, and historical multihost polling stays
// out of the production path.
type runtimePlan struct {
	localAuthority bool
	agentIngest    bool
	hubReceiver    bool
}

func planRuntime(role config.RuntimeRole, mock bool) runtimePlan {
	return runtimePlan{
		localAuthority: role == config.RuntimeRoleNode,
		agentIngest:    role == config.RuntimeRoleNode && !mock,
		hubReceiver:    role == config.RuntimeRoleHub && !mock,
	}
}

// hubNodeConfigs maps the configured registry into hub runtime entries,
// applying the optional disabled list.
func hubNodeConfigs(cfg config.Config) []hub.NodeConfig {
	disabled := make(map[string]struct{}, len(cfg.Nodes.Disabled))
	for _, id := range cfg.Nodes.Disabled {
		disabled[id] = struct{}{}
	}
	out := make([]hub.NodeConfig, 0, len(cfg.Nodes.Registered))
	for _, node := range cfg.Nodes.Registered {
		_, off := disabled[node.NodeID]
		out = append(out, hub.NodeConfig{NodeID: node.NodeID, DisplayName: node.DisplayName, Enabled: !off, Token: node.Token})
	}
	return out
}

// nodeUplinkWanted decides whether the M5.4 node uplink runtime runs: node
// role, uplink enabled in config, and not the synthetic mock run. The hub
// role never owns an uplink, and mock mode never pushes synthetic state to a
// real hub.
func nodeUplinkWanted(role config.RuntimeRole, mock bool, uplinkEnabled bool) bool {
	return role == config.RuntimeRoleNode && !mock && uplinkEnabled
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		if err := writeVersionMetadata(os.Stdout, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "devboard:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "product" {
		result, code := runProductCommand(os.Args[2:])
		_ = json.NewEncoder(os.Stdout).Encode(result)
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "agent-hook" {
		runAgentHook(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "devboard:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "devboard:", err)
		os.Exit(1)
	}
}

// restartSignal is the M5.5A graceful-restart request channel. Handlers ask
// for a restart through Request() after a successful atomic config save; the
// serve loop performs a graceful http.Server.Shutdown and the process exits
// normally, letting the supervisor (LaunchAgent / Docker) start it again.
// Handlers never call os.Exit themselves, and a lost request can never block
// a handler: the send is non-blocking on a buffered slot.
type restartSignal struct {
	ch chan struct{}
}

func newRestartSignal() *restartSignal {
	return &restartSignal{ch: make(chan struct{}, 1)}
}

func (r *restartSignal) Request() {
	select {
	case r.ch <- struct{}{}:
	default:
	}
}

func (r *restartSignal) C() <-chan struct{} { return r.ch }

// schedulerHealth adapts the uplink scheduler's operational health for the
// web settings page without the web layer importing the uplink runtime.
type schedulerHealth struct{ sched *uplink.Scheduler }

func (h schedulerHealth) UplinkHealth() web.UplinkHealth {
	if h.sched == nil {
		return web.UplinkHealth{}
	}
	hl := h.sched.Health()
	return web.UplinkHealth{
		Connected:      hl.Connected,
		LastAttemptAt:  hl.LastAttemptAt,
		LastSuccessAt:  hl.LastSuccessAt,
		LastErrorClass: hl.LastErrorClass,
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: devboard version --json | devboard serve [--config PATH] [--mock] | devboard agent-hook <codex|claude-code> | devboard healthcheck [--url URL] [--expect-role ROLE] | devboard product setup | devboard product mac <status|configure> | devboard product ...")
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to DevBoard YAML config")
	mock := fs.Bool("mock", false, "run with synthetic DevBoard mock state")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	cfg := config.Defaults()
	var err error
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			return err
		}
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if cfg.Runtime.Role == config.RuntimeRoleHub && *configPath != "" {
		if err := config.RequirePrivateFile(*configPath); err != nil {
			return fmt.Errorf("hub config is not private: %w", err)
		}
	}
	startedAt := time.Now().UTC()
	plan := planRuntime(cfg.Runtime.Role, *mock)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	projector := state.ProjectionConfig{
		KindleRefreshSeconds:          cfg.Display.KindleRefreshSeconds,
		CompleteHighVisibilitySeconds: cfg.Display.CompleteHighVisibilitySeconds,
		CompleteRetentionSeconds:      cfg.Display.CompleteRetentionSeconds,
	}

	var store *state.Store
	var hubRuntime *hub.Runtime
	var diagnostics *web.DiagnosticsRing
	if plan.hubReceiver {
		diagnostics = web.NewDiagnosticsRing(cfg.Operator.DiagnosticsCapacity, cfg.Operator.DiagnosticsMinLevel)
	}
	if plan.localAuthority {
		now := time.Now().UTC()
		var internal state.InternalRootState
		if *mock {
			internal = state.MockInternalState(now, state.HostState{ID: cfg.Host.ID, DisplayName: cfg.Host.DisplayName})
		} else {
			internal = state.LiveInitialState(now, state.HostState{ID: cfg.Host.ID, DisplayName: cfg.Host.DisplayName})
		}
		store = state.NewStore(internal)
	} else if plan.hubReceiver {
		hubRuntime, err = hub.NewRuntimeWithDiagnostics(hubNodeConfigs(cfg), logger, nil, diagnostics)
		if err != nil {
			return fmt.Errorf("initialize hub runtime: %w", err)
		}
	}

	var app *web.Server
	if cfg.Runtime.Role == config.RuntimeRoleHub {
		app, err = web.NewHubServer(projector, *mock, logger, hubRuntime, cfg.Display.DashboardRefreshSeconds)
	} else {
		app, err = web.NewRoleServer(store, projector, *mock, logger, nil, cfg.Runtime.Role, cfg.Display.DashboardRefreshSeconds)
	}
	if err != nil {
		return fmt.Errorf("initialize web server: %w", err)
	}

	var metrics *systemmetrics.Runtime
	var network *networkmetrics.Runtime
	var quotaRuntime *quota.Runtime
	if plan.localAuthority {
		metrics = startSystemMetrics(*mock, store, logger, systemmetrics.NewGopsutilBackend())
		if metrics != nil {
			defer metrics.Close()
		}
		network = startNetworkMetrics(*mock, store, logger, cfg.Network, networkmetrics.NewGopsutilBackend())
		if network != nil {
			defer network.Close()
		}
		if !*mock {
			// Quota is optional and read-only. It is enabled only when an
			// existing shared HMAC key is explicitly configured; a missing key
			// must never fall back to a per-Mac random salt.
			if keyPath := cfg.Quota.IdentityKeyFile; keyPath == "" {
				logger.Warn("quota identity key unavailable", "error", "not_configured")
			} else if identityKey, err := quota.LoadIdentityKey(keyPath); err != nil {
				logger.Warn("quota identity key unavailable", "error", "configuration")
			} else {
				collector := quota.NewCollector(store, cfg.Host.ID, identityKey, logger)
				// Fail-closed startup gate: an unparsable alias map, or one
				// that does not cover the current Codex account keys, must
				// not start an alias-less collector. The gap is recorded as
				// degraded/configuration_required instead of a silent
				// "not connected".
				gateCtx, cancelGate := context.WithTimeout(context.Background(), quota.CommandTimeout)
				check := quota.CheckStartup(gateCtx, quota.DefaultRunner(), identityKey, cfg.Quota.AccountAliases)
				cancelGate()
				if !check.StartCollector {
					logger.Error("quota collector disabled", "error", check.Reason)
					if err := quota.MarkConfigurationRequired(store, time.Now()); err != nil {
						logger.Warn("quota configuration marker failed", "error", err)
					}
				} else {
					collector.SetAliases(check.Aliases)
					quotaRuntime = quota.Start(context.Background(), collector)
					defer quotaRuntime.Close()
				}
			}
		}
	}

	var ingest *agent.IngestServer
	var stopMaintenance chan struct{}
	if plan.agentIngest {
		paths, err := agent.ResolveRuntimePaths()
		if err != nil {
			return err
		}
		reducer := agent.NewReducer(store, agent.ReducerConfig{
			StaleAfter:             time.Duration(cfg.Agent.StaleAfterSeconds) * time.Second,
			CompleteHighVisibility: time.Duration(cfg.Display.CompleteHighVisibilitySeconds) * time.Second,
			CompleteRetention:      time.Duration(cfg.Display.CompleteRetentionSeconds) * time.Second,
		})
		ingest, err = agent.StartIngestServer(paths, reducer)
		if err != nil {
			return fmt.Errorf("start agent ingest: %w", err)
		}
		defer ingest.Close()
		stopMaintenance = make(chan struct{})
		defer close(stopMaintenance)
		go maintenanceLoop(reducer, stopMaintenance)
	}

	// M5.4 node uplink: push sanitized PublicState snapshots to the hub.
	// Started last so its shutdown defer runs first: scheduling stops, the
	// current in-flight request completes, then ingest and the web server
	// wind down.
	var health web.UplinkHealthSource
	if nodeUplinkWanted(cfg.Runtime.Role, *mock, cfg.Uplink.Enabled) {
		now := func() time.Time { return time.Now().UTC() }
		builder := uplink.NewSnapshotBuilder(store, cfg.Uplink.NodeID, state.RuntimeCapabilities{}, projector, now)
		client := uplink.NewClient(cfg.Uplink.Endpoint, cfg.Uplink.Token, uplink.DefaultRequestTimeout)
		scheduler := uplink.NewScheduler(store, builder, client, uplink.DefaultSchedulerConfig(), logger, now)
		health = schedulerHealth{sched: scheduler}
		uplinkCtx, cancelUplink := context.WithCancel(context.Background())
		go scheduler.Run(uplinkCtx)
		defer func() {
			cancelUplink()
			scheduler.Wait()
		}()
		logger.Info("node uplink started", "node", cfg.Uplink.NodeID, "endpoint", cfg.Uplink.Endpoint)
	}

	// M5.5A managed surfaces: the node-local settings page and the hub admin
	// surface both persist config atomically and then request a graceful
	// restart; the supervisor brings the process back with the new config.
	restart := newRestartSignal()
	if err := attachManagedSurfaces(app, cfg, *configPath, *mock, health, hubRuntime, diagnostics, restart, startedAt, logger); err != nil {
		return err
	}

	addr := cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("starting DevBoard server", "addr", addr, "mock", *mock, "role", cfg.Runtime.Role, "nodes", len(cfg.Nodes.Registered))
	return serveUntilSignal(server, restart)
}

// attachManagedSurfaces wires the M5.5A dogfood management surfaces onto the
// role server: the loopback-only node /settings page and, for an
// admin-enabled hub with a config file, the authenticated /admin surface.
func attachManagedSurfaces(app *web.Server, cfg config.Config, configPath string, mock bool, health web.UplinkHealthSource, hubRuntime *hub.Runtime, diagnostics *web.DiagnosticsRing, restart *restartSignal, startedAt time.Time, logger *slog.Logger) error {
	if configPath == "" || mock {
		return nil
	}
	if cfg.Runtime.Role == config.RuntimeRoleNode {
		settings, err := web.NewSettingsHandler(web.SettingsOptions{
			ConfigPath:     configPath,
			HealthSource:   health,
			RequestRestart: restart.Request,
			Logger:         logger,
		})
		if err != nil {
			return fmt.Errorf("initialize settings: %w", err)
		}
		app.AttachSettings(settings)
		return nil
	}
	if cfg.Runtime.Role == config.RuntimeRoleHub && cfg.Admin.Enabled && hubRuntime != nil {
		admin, err := web.NewAdminHandler(web.AdminOptions{
			ConfigPath:     configPath,
			TokenFile:      cfg.Admin.TokenFile,
			Nodes:          hubRuntime.Store(),
			RequestRestart: restart.Request,
			Diagnostics:    diagnostics,
			ProductVersion: productVersion,
			GitCommit:      gitCommit,
			RuntimeReady:   hubRuntime != nil,
			StartedAt:      startedAt,
			Logger:         logger,
		})
		if err != nil {
			return fmt.Errorf("initialize admin: %w", err)
		}
		app.AttachAdmin(admin)
	}
	return nil
}

func serveUntilSignal(server *http.Server, restart *restartSignal) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveWithShutdown(server.ListenAndServe, server.Shutdown, ctx.Done(), restart.C())
}

// serveWithShutdown runs one serve cycle. It exists as a separate function so
// the restart/shutdown behavior is unit-testable with injected fakes instead
// of a real listener: listen blocks until shutdown completes; a graceful
// exit (signal or managed restart request) drains the server and returns nil
// so the supervisor restarts the process.
func serveWithShutdown(listen func() error, shutdown func(context.Context) error, signalDone, restartDone <-chan struct{}) error {
	errCh := make(chan error, 1)
	go func() { errCh <- listen() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-signalDone:
		return gracefulShutdown(shutdown, errCh)
	case <-restartDone:
		// Managed config change: shut down gracefully (in-flight requests
		// drain; deferred runtimes close) and exit normally so the
		// LaunchAgent / Docker supervisor restarts with the new config.
		return gracefulShutdown(shutdown, errCh)
	}
}

func gracefulShutdown(shutdown func(context.Context) error, errCh <-chan error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	err := <-errCh
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func startSystemMetrics(mock bool, store *state.Store, logger *slog.Logger, backend systemmetrics.Backend) *systemmetrics.Runtime {
	if mock {
		return nil
	}
	collector := systemmetrics.NewCollector(store, backend, logger)
	return systemmetrics.Start(collector, systemmetrics.DefaultSampleInterval)
}

func startNetworkMetrics(mock bool, store *state.Store, logger *slog.Logger, cfg config.NetworkConfig, backend networkmetrics.Backend) *networkmetrics.Runtime {
	if mock {
		return nil
	}
	probe := networkmetrics.NewTCPProbe(cfg.ProbeAddress, time.Duration(cfg.ProbeTimeoutMilliseconds)*time.Millisecond)
	collector := networkmetrics.NewCollector(store, probe, backend, logger)
	return networkmetrics.Start(collector, networkmetrics.DefaultSampleInterval)
}

func maintenanceLoop(r *agent.Reducer, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			_ = r.Maintenance(now.UTC())
		case <-stop:
			return
		}
	}
}

func runAgentHook(args []string) {
	diagnostic := false
	providerArg := ""
	for _, arg := range args {
		if arg == "--diagnostic" {
			diagnostic = true
			continue
		}
		if providerArg == "" {
			providerArg = arg
		} else {
			return
		}
	}
	provider := agent.Provider(providerArg)
	if provider != agent.ProviderCodex && provider != agent.ProviderClaude {
		if diagnostic {
			fmt.Fprintln(os.Stderr, "devboard agent-hook: unsupported provider")
		}
		return
	}
	raw, err := agent.ReadBounded(os.Stdin, agent.MaxRawProviderBytes)
	if err != nil {
		if diagnostic {
			fmt.Fprintln(os.Stderr, "devboard agent-hook: provider input rejected")
		}
		return
	}
	id, err := agent.NewEventID()
	if err != nil {
		if diagnostic {
			fmt.Fprintln(os.Stderr, "devboard agent-hook: event id unavailable")
		}
		return
	}
	at := time.Now().UTC()
	event, supported, err := agent.Normalize(provider, raw, at, id)
	if err != nil || !supported {
		if diagnostic && err != nil {
			fmt.Fprintln(os.Stderr, "devboard agent-hook: provider event rejected")
		}
		return
	}
	paths, err := agent.ResolveRuntimePaths()
	if err != nil {
		if diagnostic {
			fmt.Fprintln(os.Stderr, "devboard agent-hook: runtime path unavailable")
		}
		return
	}
	if err := agent.SendEvent(paths, event); err != nil && diagnostic {
		fmt.Fprintln(os.Stderr, "devboard agent-hook: monitoring transport unavailable")
	}
}
