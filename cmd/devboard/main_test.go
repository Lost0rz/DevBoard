package main

import (
	"context"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
	"github.com/Lost0rz/DevBoard/internal/systemmetrics"
)

type panicMetricsBackend struct{}

func (panicMetricsBackend) CPUPercent(context.Context, time.Duration) ([]float64, error) {
	panic("real system collector started in mock mode")
}
func (panicMetricsBackend) VirtualMemory(context.Context) (systemmetrics.MetricStats, error) {
	panic("real system collector started in mock mode")
}
func (panicMetricsBackend) SwapMemory(context.Context) (systemmetrics.MetricStats, error) {
	panic("real system collector started in mock mode")
}
func (panicMetricsBackend) DiskUsage(context.Context, string) (systemmetrics.MetricStats, error) {
	panic("real system collector started in mock mode")
}

func TestMockModeDoesNotStartSystemMetrics(t *testing.T) {
	now := time.Date(2026, 8, 21, 4, 30, 0, 0, time.UTC)
	mock := state.MockInternalState(now, state.HostState{ID: "mock-host"})
	store := state.NewStore(mock)
	before := store.Snapshot()

	if runtime := startSystemMetrics(true, store, nil, panicMetricsBackend{}); runtime != nil {
		t.Fatal("mock mode unexpectedly started system metrics runtime")
	}
	after := store.Snapshot()
	if after.GeneratedAt != before.GeneratedAt || len(after.System.ProcessGroups) != len(before.System.ProcessGroups) {
		t.Fatal("mock state changed while system metrics were isolated")
	}
}
