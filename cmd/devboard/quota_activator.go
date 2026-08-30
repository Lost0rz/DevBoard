package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/Lost0rz/DevBoard/internal/agentquota"
	"github.com/Lost0rz/DevBoard/internal/config"
)

const quotaActivatorHeartbeat = 2 * time.Second

// quotaActivator is intentionally separate from the Hub HTTP server. The
// only cross-container contract is the private data directory: config/key in,
// and status/audit/control files out. That keeps a Hub release from owning or
// restarting the outbound scheduler.
type quotaActivator struct {
	configPath string
	keyPath    string
	statusPath string
	control    string
	audit      *agentquota.FileAuditLog
	logger     *slog.Logger

	runtime *agentquota.Runtime
	cfg     config.Config
	fired   []string
	manual  *agentquota.Health
}

func runQuotaActivator(args []string) error {
	fs := flag.NewFlagSet("quota-activator", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to Hub YAML config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *configPath == "" {
		return fmt.Errorf("usage: devboard quota-activator --config PATH")
	}
	if err := config.RequirePrivateFile(*configPath); err != nil {
		return fmt.Errorf("hub config is not private: %w", err)
	}
	audit, err := agentquota.NewFileAuditLog(agentquota.AuditLogFile(*configPath))
	if err != nil {
		return fmt.Errorf("initialize agent quota audit: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	worker := &quotaActivator{
		configPath: *configPath,
		keyPath:    agentquota.KeyFile(*configPath),
		statusPath: agentquota.StatusFile(*configPath),
		control:    agentquota.ControlFile(*configPath),
		audit:      audit,
		logger:     logger,
	}
	if previous, err := agentquota.ReadWorkerStatus(worker.statusPath); err == nil {
		worker.fired = previous.FiredAnchors
		worker.manual = previous.ManualTest
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return worker.run(ctx)
}

func (w *quotaActivator) run(ctx context.Context) error {
	if err := w.reload(); err != nil {
		w.publish(agentquota.Health{State: "configuration_required", Message: "Activator could not load the Hub configuration."})
		w.logger.Warn("agent quota activator configuration unavailable", "error", "configuration")
	}
	ticker := time.NewTicker(quotaActivatorHeartbeat)
	defer ticker.Stop()
	for {
		w.handleManual(ctx)
		w.publishCurrent()
		select {
		case <-ctx.Done():
			if w.runtime != nil {
				w.runtime.Close()
			}
			return nil
		case <-ticker.C:
			if err := w.reload(); err != nil {
				w.publish(agentquota.Health{Enabled: w.cfg.AgentQuota.Enabled, Provider: w.cfg.AgentQuota.Provider, State: "configuration_required", Message: "Activator could not reload the Hub configuration."})
				w.logger.Warn("agent quota activator configuration reload failed", "error", "configuration")
			}
		}
	}
}

func (w *quotaActivator) reload() error {
	cfg, err := config.Load(w.configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if cfg.Runtime.Role != config.RuntimeRoleHub {
		return fmt.Errorf("quota activator requires a hub config")
	}
	if w.runtime != nil && cfg.Server.Timezone == w.cfg.Server.Timezone && reflect.DeepEqual(cfg.AgentQuota, w.cfg.AgentQuota) {
		return nil
	}
	if w.runtime != nil {
		w.runtime.Close()
		w.fired = w.runtime.FiredAnchorKeys()
	}
	w.cfg = cfg
	w.runtime = agentquota.StartWithTimezoneAndFired(context.Background(), cfg.AgentQuota, w.keyPath, cfg.Server.Timezone, w.logger, w.fired, w.record)
	w.publishCurrent()
	w.logger.Info("agent quota activator configuration applied", "enabled", cfg.AgentQuota.Enabled, "schedules", len(cfg.AgentQuota.Schedules), "timezone", cfg.Server.Timezone)
	return nil
}

func (w *quotaActivator) record(event agentquota.Event) {
	if err := w.audit.Record(event); err != nil {
		w.logger.Warn("agent quota audit record failed", "error", "audit_write_failed")
	}
	w.publishCurrent()
}

func (w *quotaActivator) handleManual(ctx context.Context) {
	request, ok, err := agentquota.ClaimManualRequest(w.control)
	if err != nil {
		w.logger.Warn("agent quota manual control unavailable", "error", "control_unavailable")
		return
	}
	if !ok {
		return
	}
	if w.runtime == nil {
		w.publish(agentquota.Health{State: "configuration_required", Message: "Manual test could not start because the activator has no valid configuration."})
		return
	}
	w.logger.Info("agent quota manual test claimed", "request", request.ID)
	health := agentquota.TestActivation(ctx, w.cfg.AgentQuota, w.keyPath, w.logger, w.record)
	w.manual = &health
	w.publishCurrent()
}

func (w *quotaActivator) publishCurrent() {
	if w.runtime == nil {
		return
	}
	w.fired = w.runtime.FiredAnchorKeys()
	w.publish(w.runtime.Health())
}

func (w *quotaActivator) publish(health agentquota.Health) {
	if err := agentquota.WriteWorkerStatusSnapshot(w.statusPath, health, w.fired, w.manual); err != nil {
		w.logger.Warn("agent quota worker status write failed", "error", "status_write_failed")
	}
}

func runQuotaActivatorHealthcheck(args []string) error {
	fs := flag.NewFlagSet("quota-activator-healthcheck", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to Hub YAML config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *configPath == "" {
		return fmt.Errorf("usage: devboard quota-activator-healthcheck --config PATH")
	}
	status, err := agentquota.ReadWorkerStatus(agentquota.StatusFile(*configPath))
	if err != nil {
		return fmt.Errorf("activator status unavailable")
	}
	if time.Since(status.UpdatedAt) > 15*time.Second {
		return fmt.Errorf("activator heartbeat is stale")
	}
	return nil
}
