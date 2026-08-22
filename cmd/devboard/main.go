package main

import (
	"context"
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
	"github.com/Lost0rz/DevBoard/internal/multihost"
	"github.com/Lost0rz/DevBoard/internal/networkmetrics"
	"github.com/Lost0rz/DevBoard/internal/state"
	"github.com/Lost0rz/DevBoard/internal/systemmetrics"
	"github.com/Lost0rz/DevBoard/internal/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent-hook" {
		runAgentHook(os.Args[2:])
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "devboard:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: devboard serve [--config PATH] [--mock] | devboard agent-hook <codex|claude-code>")
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	projector := state.ProjectionConfig{
		KindleRefreshSeconds:          cfg.Display.KindleRefreshSeconds,
		CompleteHighVisibilitySeconds: cfg.Display.CompleteHighVisibilitySeconds,
		CompleteRetentionSeconds:      cfg.Display.CompleteRetentionSeconds,
	}

	var store *state.Store
	var peerStore *multihost.PeerSnapshotStore
	if cfg.Runtime.Role == config.RuntimeRoleNode {
		now := time.Now().UTC()
		var internal state.InternalRootState
		if *mock {
			internal = state.MockInternalState(now, state.HostState{ID: cfg.Host.ID, DisplayName: cfg.Host.DisplayName})
		} else {
			internal = state.LiveInitialState(now, state.HostState{ID: cfg.Host.ID, DisplayName: cfg.Host.DisplayName})
		}
		store = state.NewStore(internal)
	} else {
		peerStore = multihost.NewPeerSnapshotStore(cfg.MultiHost.Peers)
	}

	app, err := web.NewRoleServer(store, projector, *mock, logger, peerStore, cfg.Runtime.Role, cfg.Display.DashboardRefreshSeconds)
	if err != nil {
		return fmt.Errorf("initialize web server: %w", err)
	}

	var metrics *systemmetrics.Runtime
	var network *networkmetrics.Runtime
	if cfg.Runtime.Role == config.RuntimeRoleNode {
		metrics = startSystemMetrics(*mock, store, logger, systemmetrics.NewGopsutilBackend())
		if metrics != nil {
			defer metrics.Close()
		}
		network = startNetworkMetrics(*mock, store, logger, cfg.Network, networkmetrics.NewGopsutilBackend())
		if network != nil {
			defer network.Close()
		}
	}

	var peers *multihost.Runtime
	if cfg.Runtime.Role == config.RuntimeRoleHub && !*mock && len(cfg.MultiHost.Peers) > 0 {
		peers = multihost.Start(cfg.MultiHost.Peers, peerStore, "", logger)
		defer peers.Close()
	}

	var ingest *agent.IngestServer
	var stopMaintenance chan struct{}
	if cfg.Runtime.Role == config.RuntimeRoleNode && !*mock {
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

	addr := cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("starting DevBoard server", "addr", addr, "mock", *mock, "role", cfg.Runtime.Role, "peers", len(cfg.MultiHost.Peers))
	return serveUntilSignal(server)
}

func serveUntilSignal(server *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		err := <-errCh
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
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
