package systemmetrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	defaultCPUWindow = 250 * time.Millisecond
	rootFilesystem   = "/"
)

var errInvalidMeasurement = errors.New("invalid system metric measurement")

type Collector struct {
	store     *state.Store
	backend   Backend
	logger    *slog.Logger
	cpuWindow time.Duration
	now       func() time.Time
}

func NewCollector(store *state.Store, backend Backend, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Collector{
		store:     store,
		backend:   backend,
		logger:    logger,
		cpuWindow: defaultCPUWindow,
		now:       time.Now,
	}
}

func (c *Collector) Collect(ctx context.Context) error {
	attemptAt := c.now().UTC()

	cpuValue, cpuOK, cpuErr := c.collectCPU(ctx)
	memory, memoryOK, memoryErr := c.collectMetric(ctx, "memory", c.backend.VirtualMemory)
	swap, swapOK, swapErr := c.collectMetric(ctx, "swap", c.backend.SwapMemory)
	disk, diskOK, diskErr := c.collectMetric(ctx, "disk", func(ctx context.Context) (MetricStats, error) {
		return c.backend.DiskUsage(ctx, rootFilesystem)
	})

	c.logFailure("cpu", cpuErr)
	c.logFailure("memory", memoryErr)
	c.logFailure("swap", swapErr)
	c.logFailure("disk", diskErr)

	successes := 0
	for _, ok := range []bool{cpuOK, memoryOK, swapOK, diskOK} {
		if ok {
			successes++
		}
	}

	return c.store.Update(func(root *state.InternalRootState) error {
		previousHealth := root.Sources["system"]
		health := reduceHealth(previousHealth, attemptAt, successes)

		root.System = state.SystemState{
			CPUPercent:    cpuValue,
			Memory:        memory,
			Swap:          swap,
			Disk:          disk,
			ProcessGroups: []state.ProcessGroup{},
		}
		if root.Sources == nil {
			root.Sources = make(map[string]state.SourceHealth)
		}
		root.Sources["system"] = health
		root.GeneratedAt = attemptAt
		return nil
	})
}

func (c *Collector) collectCPU(ctx context.Context) (*float64, bool, error) {
	values, err := c.backend.CPUPercent(ctx, c.cpuWindow)
	if err != nil {
		return nil, false, err
	}
	if len(values) == 0 || !validPercent(values[0]) {
		return nil, false, fmt.Errorf("%w: cpu", errInvalidMeasurement)
	}
	value := values[0]
	return &value, true, nil
}

func (c *Collector) collectMetric(ctx context.Context, name string, collect func(context.Context) (MetricStats, error)) (state.MetricSet, bool, error) {
	stats, err := collect(ctx)
	if err != nil {
		return state.MetricSet{}, false, err
	}
	if !validPercent(stats.PercentUsed) {
		return state.MetricSet{}, false, fmt.Errorf("%w: %s", errInvalidMeasurement, name)
	}
	used := stats.UsedBytes
	total := stats.TotalBytes
	percent := stats.PercentUsed
	return state.MetricSet{UsedBytes: &used, TotalBytes: &total, PercentUsed: &percent}, true, nil
}

func (c *Collector) logFailure(component string, err error) {
	if err == nil {
		return
	}
	c.logger.Warn("system metrics component collection failed", "component", component, "err", err)
}

func validPercent(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100
}

func reduceHealth(previous state.SourceHealth, attemptAt time.Time, successes int) state.SourceHealth {
	health := state.SourceHealth{
		LastAttemptAt: &attemptAt,
		LastSuccessAt: previous.LastSuccessAt,
	}
	switch successes {
	case 4:
		health.Status = state.SourceAvailable
		health.LastSuccessAt = &attemptAt
		health.Message = "Embedded system metrics collector is available."
	case 0:
		health.Status = state.SourceUnavailable
		health.Message = "Embedded system metrics collector is unavailable."
	default:
		health.Status = state.SourceDegraded
		health.Message = "Embedded system metrics collector is partially available."
	}
	return health
}
