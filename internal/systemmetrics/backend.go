package systemmetrics

import (
	"context"
	"time"
)

// Backend isolates platform/library collection from state reduction so M3.1
// behavior can be tested without depending on the developer machine.
type Backend interface {
	CPUPercent(context.Context, time.Duration) ([]float64, error)
	VirtualMemory(context.Context) (MetricStats, error)
	SwapMemory(context.Context) (MetricStats, error)
	DiskUsage(context.Context, string) (MetricStats, error)
}

type MetricStats struct {
	UsedBytes   uint64
	TotalBytes  uint64
	PercentUsed float64
}
