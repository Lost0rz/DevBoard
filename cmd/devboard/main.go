package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
	"github.com/Lost0rz/DevBoard/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "devboard:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: devboard serve [--config PATH] --mock")
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to M1 YAML config")
	mock := fs.Bool("mock", false, "run with synthetic M1 mock state")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if !*mock {
		return fmt.Errorf("M1 has no live collectors; run serve with --mock")
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

	now := time.Now().UTC()
	internal := state.MockInternalState(now, state.HostState{ID: cfg.Host.ID, DisplayName: cfg.Host.DisplayName})
	store := state.NewStore(internal)
	projector := state.ProjectionConfig{
		KindleRefreshSeconds:          cfg.Display.KindleRefreshSeconds,
		CompleteHighVisibilitySeconds: cfg.Display.CompleteHighVisibilitySeconds,
		CompleteRetentionSeconds:      cfg.Display.CompleteRetentionSeconds,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	app, err := web.NewServer(store, projector, true, logger)
	if err != nil {
		return fmt.Errorf("initialize web server: %w", err)
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
	logger.Info("starting DevBoard M1 mock server", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
