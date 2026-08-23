package systemmetrics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type countingBackend struct {
	mu    sync.Mutex
	calls int
}

func (b *countingBackend) record() {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
}
func (b *countingBackend) CPUPercent(context.Context, time.Duration) ([]float64, error) {
	b.record()
	return []float64{1}, nil
}
func (b *countingBackend) VirtualMemory(context.Context) (MetricStats, error) {
	b.record()
	return MetricStats{TotalBytes: 1}, nil
}
func (b *countingBackend) SwapMemory(context.Context) (MetricStats, error) {
	b.record()
	return MetricStats{}, nil
}
func (b *countingBackend) DiskUsage(context.Context, string) (MetricStats, error) {
	b.record()
	return MetricStats{TotalBytes: 1}, nil
}
func (b *countingBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func TestRuntimeInitialCollectionAndCleanClose(t *testing.T) {
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	store := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "host"}))
	backend := &countingBackend{}
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }
	runtime := Start(collector, time.Hour)
	if backend.count() != 4 {
		t.Fatalf("initial backend calls=%d want 4", backend.count())
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
