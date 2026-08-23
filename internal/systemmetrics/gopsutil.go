package systemmetrics

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

type GopsutilBackend struct{}

func NewGopsutilBackend() GopsutilBackend { return GopsutilBackend{} }

func (GopsutilBackend) CPUPercent(ctx context.Context, interval time.Duration) ([]float64, error) {
	return cpu.PercentWithContext(ctx, interval, false)
}

func (GopsutilBackend) VirtualMemory(ctx context.Context) (MetricStats, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return MetricStats{}, err
	}
	return MetricStats{UsedBytes: v.Used, TotalBytes: v.Total, PercentUsed: v.UsedPercent}, nil
}

func (GopsutilBackend) SwapMemory(ctx context.Context) (MetricStats, error) {
	v, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return MetricStats{}, err
	}
	return MetricStats{UsedBytes: v.Used, TotalBytes: v.Total, PercentUsed: v.UsedPercent}, nil
}

func (GopsutilBackend) DiskUsage(ctx context.Context, path string) (MetricStats, error) {
	v, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return MetricStats{}, err
	}
	return MetricStats{UsedBytes: v.Used, TotalBytes: v.Total, PercentUsed: v.UsedPercent}, nil
}
