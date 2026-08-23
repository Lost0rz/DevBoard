package systemmetrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type fakeBackend struct {
	cpu         []float64
	cpuErr      error
	memory      MetricStats
	memoryErr   error
	swap        MetricStats
	swapErr     error
	disk        MetricStats
	diskErr     error
	blockCPU    <-chan struct{}
	cpuCalled   chan struct{}
	callOnce    sync.Once
	cpuInterval time.Duration
	diskPath    string
}

func (f *fakeBackend) CPUPercent(_ context.Context, interval time.Duration) ([]float64, error) {
	f.cpuInterval = interval
	if f.cpuCalled != nil {
		f.callOnce.Do(func() { close(f.cpuCalled) })
	}
	if f.blockCPU != nil {
		<-f.blockCPU
	}
	return append([]float64(nil), f.cpu...), f.cpuErr
}
func (f *fakeBackend) VirtualMemory(context.Context) (MetricStats, error) {
	return f.memory, f.memoryErr
}
func (f *fakeBackend) SwapMemory(context.Context) (MetricStats, error) {
	return f.swap, f.swapErr
}
func (f *fakeBackend) DiskUsage(_ context.Context, path string) (MetricStats, error) {
	f.diskPath = path
	return f.disk, f.diskErr
}

func healthyFake() *fakeBackend {
	return &fakeBackend{
		cpu:    []float64{23.5},
		memory: MetricStats{UsedBytes: 12, TotalBytes: 24, PercentUsed: 50},
		swap:   MetricStats{UsedBytes: 1, TotalBytes: 4, PercentUsed: 25},
		disk:   MetricStats{UsedBytes: 43, TotalBytes: 100, PercentUsed: 43},
	}
}

func testStore(now time.Time) *state.Store {
	return state.NewStore(state.LiveInitialState(now.Add(-time.Minute), state.HostState{ID: "host", DisplayName: "Host"}))
}

func TestSuccessfulSampleUpdatesSystemAndHealth(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 0, 0, 0, time.UTC)
	store := testStore(now)
	collector := NewCollector(store, healthyFake(), nil)
	collector.now = func() time.Time { return now }

	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	assertFloat(t, got.System.CPUPercent, 23.5)
	assertMetric(t, got.System.Memory, 12, 24, 50)
	assertMetric(t, got.System.Swap, 1, 4, 25)
	assertMetric(t, got.System.Disk, 43, 100, 43)
	if len(got.System.ProcessGroups) != 0 {
		t.Fatalf("processGroups=%v, want empty", got.System.ProcessGroups)
	}
	health := got.Sources["system"]
	if health.Status != state.SourceAvailable || health.LastAttemptAt == nil || health.LastSuccessAt == nil {
		t.Fatalf("health=%+v", health)
	}
	if !health.LastAttemptAt.Equal(now) || !health.LastSuccessAt.Equal(now) {
		t.Fatalf("health timestamps=%+v", health)
	}
	if !got.GeneratedAt.Equal(now) {
		t.Fatalf("generatedAt=%s want %s", got.GeneratedAt, now)
	}
	backend := collector.backend.(*fakeBackend)
	if backend.cpuInterval != defaultCPUWindow {
		t.Fatalf("cpu interval=%s want %s", backend.cpuInterval, defaultCPUWindow)
	}
	if backend.diskPath != rootFilesystem {
		t.Fatalf("disk path=%q want %q", backend.diskPath, rootFilesystem)
	}
}

func TestPartialFailureKeepsCurrentSuccessesAndNullsFailure(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 1, 0, 0, time.UTC)
	store := testStore(now)
	backend := healthyFake()
	backend.diskErr = errors.New("PRIVATE /Volumes/secret failure")
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }

	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if got.Sources["system"].Status != state.SourceDegraded {
		t.Fatalf("health=%+v", got.Sources["system"])
	}
	assertFloat(t, got.System.CPUPercent, 23.5)
	assertMetric(t, got.System.Memory, 12, 24, 50)
	assertMetric(t, got.System.Swap, 1, 4, 25)
	assertNilMetric(t, got.System.Disk)
	if strings.Contains(got.Sources["system"].Message, "PRIVATE") || strings.Contains(got.Sources["system"].Message, "/Volumes") {
		t.Fatalf("public health message leaked raw error: %q", got.Sources["system"].Message)
	}
}

func TestTotalFailurePreservesPreviousLastSuccessAndClearsMeasurements(t *testing.T) {
	previousSuccess := time.Date(2026, 8, 21, 2, 55, 0, 0, time.UTC)
	now := previousSuccess.Add(10 * time.Minute)
	initial := state.LiveInitialState(previousSuccess, state.HostState{ID: "host"})
	oldCPU := 99.0
	initial.System.CPUPercent = &oldCPU
	initial.System.Memory = state.MetricSet{UsedBytes: uint64Ptr(10), TotalBytes: uint64Ptr(20), PercentUsed: float64Ptr(50)}
	health := initial.Sources["system"]
	health.LastSuccessAt = &previousSuccess
	initial.Sources["system"] = health
	store := state.NewStore(initial)
	failed := errors.New("collector failed")
	backend := &fakeBackend{cpuErr: failed, memoryErr: failed, swapErr: failed, diskErr: failed}
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }

	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if got.Sources["system"].Status != state.SourceUnavailable {
		t.Fatalf("health=%+v", got.Sources["system"])
	}
	if got.Sources["system"].LastAttemptAt == nil || !got.Sources["system"].LastAttemptAt.Equal(now) {
		t.Fatalf("lastAttemptAt=%v", got.Sources["system"].LastAttemptAt)
	}
	if got.Sources["system"].LastSuccessAt == nil || !got.Sources["system"].LastSuccessAt.Equal(previousSuccess) {
		t.Fatalf("lastSuccessAt=%v want %s", got.Sources["system"].LastSuccessAt, previousSuccess)
	}
	if got.System.CPUPercent != nil {
		t.Fatalf("cpu=%v want nil", *got.System.CPUPercent)
	}
	assertNilMetric(t, got.System.Memory)
	assertNilMetric(t, got.System.Swap)
	assertNilMetric(t, got.System.Disk)
}

func TestValidZeroRemainsMeasuredZero(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 2, 0, 0, time.UTC)
	backend := &fakeBackend{
		cpu:    []float64{0},
		memory: MetricStats{UsedBytes: 0, TotalBytes: 24, PercentUsed: 0},
		swap:   MetricStats{UsedBytes: 0, TotalBytes: 0, PercentUsed: 0},
		disk:   MetricStats{UsedBytes: 0, TotalBytes: 100, PercentUsed: 0},
	}
	store := testStore(now)
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	assertFloat(t, got.System.CPUPercent, 0)
	assertMetric(t, got.System.Swap, 0, 0, 0)
	if got.Sources["system"].Status != state.SourceAvailable {
		t.Fatalf("health=%+v", got.Sources["system"])
	}
}

func TestInvalidAggregateCPUIsNullAndDegraded(t *testing.T) {
	tests := []struct {
		name string
		cpu  []float64
	}{
		{name: "empty", cpu: nil},
		{name: "nan", cpu: []float64{math.NaN()}},
		{name: "positive infinity", cpu: []float64{math.Inf(1)}},
		{name: "negative infinity", cpu: []float64{math.Inf(-1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 21, 3, 3, 0, 0, time.UTC)
			backend := healthyFake()
			backend.cpu = tt.cpu
			store := testStore(now)
			collector := NewCollector(store, backend, nil)
			collector.now = func() time.Time { return now }
			if err := collector.Collect(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := store.Snapshot()
			if got.System.CPUPercent != nil {
				t.Fatalf("cpu=%v want nil", *got.System.CPUPercent)
			}
			if got.Sources["system"].Status != state.SourceDegraded {
				t.Fatalf("health=%+v", got.Sources["system"])
			}
		})
	}
}

func TestProjectionUsesExistingSystemFieldsAndSanitizedHealth(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 4, 0, 0, time.UTC)
	backend := healthyFake()
	backend.diskErr = errors.New("PRIVATE_CREDENTIAL=/tmp/private")
	store := testStore(now)
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	pub := state.ProjectPublic(store.Snapshot(), state.RuntimeCapabilities{}, state.ProjectionConfig{}, now)
	raw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "PRIVATE_CREDENTIAL") || strings.Contains(body, "/tmp/private") {
		t.Fatalf("public projection leaked collector error: %s", body)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	system, ok := decoded["system"].(map[string]any)
	if !ok {
		t.Fatalf("system=%T", decoded["system"])
	}
	want := map[string]bool{"cpuPercent": true, "memory": true, "swap": true, "disk": true, "processGroups": true}
	if len(system) != len(want) {
		t.Fatalf("system keys=%v", system)
	}
	for key := range system {
		if !want[key] {
			t.Fatalf("unexpected public system key %q", key)
		}
	}
}

func TestCollectorUpdateDoesNotLoseConcurrentAgentState(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 5, 0, 0, time.UTC)
	store := testStore(now)
	release := make(chan struct{})
	called := make(chan struct{})
	backend := healthyFake()
	backend.blockCPU = release
	backend.cpuCalled = called
	collector := NewCollector(store, backend, nil)
	collector.now = func() time.Time { return now }

	done := make(chan error, 1)
	go func() { done <- collector.Collect(context.Background()) }()
	<-called
	if err := store.Update(func(root *state.InternalRootState) error {
		root.Agents = append(root.Agents, state.AgentState{ID: "codex:concurrent", Provider: "codex", SessionID: "concurrent"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot()
	if len(got.Agents) != 1 || got.Agents[0].ID != "codex:concurrent" {
		t.Fatalf("agents=%+v", got.Agents)
	}
	if got.Sources["system"].Status != state.SourceAvailable {
		t.Fatalf("health=%+v", got.Sources["system"])
	}
}

func assertMetric(t *testing.T, got state.MetricSet, used, total uint64, percent float64) {
	t.Helper()
	if got.UsedBytes == nil || *got.UsedBytes != used || got.TotalBytes == nil || *got.TotalBytes != total || got.PercentUsed == nil || *got.PercentUsed != percent {
		t.Fatalf("metric=%+v want used=%d total=%d percent=%v", got, used, total, percent)
	}
}

func assertNilMetric(t *testing.T, got state.MetricSet) {
	t.Helper()
	if got.UsedBytes != nil || got.TotalBytes != nil || got.PercentUsed != nil {
		t.Fatalf("metric=%+v want all nil", got)
	}
}

func assertFloat(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("value=%v want %v", got, want)
	}
}

func uint64Ptr(v uint64) *uint64    { return &v }
func float64Ptr(v float64) *float64 { return &v }
