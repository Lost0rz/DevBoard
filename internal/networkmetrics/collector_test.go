package networkmetrics

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type fakeProbe struct {
	results []ProbeResult
	i       int
}

func (f *fakeProbe) Probe(context.Context) ProbeResult {
	if f.i >= len(f.results) {
		return ProbeResult{}
	}
	r := f.results[f.i]
	f.i++
	return r
}

type fakeBackend struct {
	interfaces   map[string]string
	counters     []Counter
	counterErr   error
	counterIndex int
	interfaceErr error
}

func (f *fakeBackend) InterfaceForIP(ip net.IP) (string, error) {
	if f.interfaceErr != nil {
		return "", f.interfaceErr
	}
	return f.interfaces[ip.String()], nil
}
func (f *fakeBackend) Counter(context.Context, string) (Counter, error) {
	if f.counterErr != nil {
		return Counter{}, f.counterErr
	}
	if f.counterIndex >= len(f.counters) {
		return Counter{}, errors.New("no counter")
	}
	c := f.counters[f.counterIndex]
	f.counterIndex++
	return c, nil
}
func newStore() *state.Store {
	return state.NewStore(state.InternalRootState{Network: state.NetworkState{Quality: state.NetworkUnknown}, Sources: map[string]state.SourceHealth{}})
}

func TestRollingWindowExactlyTwelve(t *testing.T) {
	c := &Collector{}
	for i := 0; i < 13; i++ {
		c.recordOutcome(i != 0)
	}
	if len(c.outcomes) != 12 {
		t.Fatalf("got %d", len(c.outcomes))
	}
	if got := c.failurePercent(); got != 0 {
		t.Fatalf("got %v", got)
	}
}
func TestQualityOfflineAfterThreeFailures(t *testing.T) {
	c := &Collector{}
	c.recordOutcome(false)
	c.recordOutcome(false)
	c.recordOutcome(false)
	if got := c.quality(ProbeResult{Completed: true}); got != state.NetworkOffline {
		t.Fatal(got)
	}
}
func TestQualityTransientFailureDegraded(t *testing.T) {
	c := &Collector{}
	c.recordOutcome(true)
	c.recordOutcome(false)
	if got := c.quality(ProbeResult{Completed: true, Reachable: false}); got != state.NetworkDegraded {
		t.Fatal(got)
	}
}
func TestQualityHighLatencyDegraded(t *testing.T) {
	c := &Collector{}
	c.recordOutcome(true)
	if got := c.quality(ProbeResult{Completed: true, Reachable: true, Latency: 501 * time.Millisecond}); got != state.NetworkDegraded {
		t.Fatal(got)
	}
}
func TestQualityHealthyGood(t *testing.T) {
	c := &Collector{}
	for i := 0; i < 12; i++ {
		c.recordOutcome(true)
	}
	if got := c.quality(ProbeResult{Completed: true, Reachable: true, Latency: 40 * time.Millisecond}); got != state.NetworkGood {
		t.Fatal(got)
	}
}
func TestFirstAndSecondTrafficSample(t *testing.T) {
	c := &Collector{}
	t0 := time.Unix(100, 0)
	rx, tx := c.rates(Counter{Interface: "en0", BytesRecv: 100, BytesSent: 200}, t0)
	if rx != nil || tx != nil {
		t.Fatal("first sample must be nil")
	}
	rx, tx = c.rates(Counter{Interface: "en0", BytesRecv: 300, BytesSent: 300}, t0.Add(4*time.Second))
	if *rx != 50 || *tx != 25 {
		t.Fatalf("%v %v", *rx, *tx)
	}
}
func TestActualElapsedAndZeroTraffic(t *testing.T) {
	c := &Collector{}
	t0 := time.Unix(100, 0)
	c.rates(Counter{Interface: "en0", BytesRecv: 100, BytesSent: 200}, t0)
	rx, tx := c.rates(Counter{Interface: "en0", BytesRecv: 100, BytesSent: 200}, t0.Add(2*time.Second))
	if *rx != 0 || *tx != 0 {
		t.Fatalf("%v %v", *rx, *tx)
	}
}
func TestCounterResetReturnsNil(t *testing.T) {
	c := &Collector{}
	t0 := time.Unix(100, 0)
	c.rates(Counter{Interface: "en0", BytesRecv: 100, BytesSent: 200}, t0)
	rx, tx := c.rates(Counter{Interface: "en0", BytesRecv: 99, BytesSent: 220}, t0.Add(time.Second))
	if rx != nil || tx != nil {
		t.Fatal("reset must null rates")
	}
}
func TestInterfaceChangeResetsBaseline(t *testing.T) {
	c := &Collector{}
	t0 := time.Unix(100, 0)
	c.rates(Counter{Interface: "en0", BytesRecv: 100, BytesSent: 200}, t0)
	rx, tx := c.rates(Counter{Interface: "en1", BytesRecv: 500, BytesSent: 600}, t0.Add(time.Second))
	if rx != nil || tx != nil {
		t.Fatal("interface change must null rates")
	}
}
func TestNoRouteInterfaceLeavesRatesNullAndNoAggregateFallback(t *testing.T) {
	store := newStore()
	probe := &fakeProbe{results: []ProbeResult{{Completed: true, Reachable: true, Latency: 10 * time.Millisecond, LocalIP: net.ParseIP("10.0.0.2")}}}
	backend := &fakeBackend{interfaceErr: errors.New("no route")}
	c := NewCollector(store, probe, backend, nil)
	c.now = func() time.Time { return time.Unix(100, 0) }
	if err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := store.Snapshot()
	if s.Network.ReceiveBytesPerSecond != nil || s.Network.SendBytesPerSecond != nil {
		t.Fatal("rates must be null")
	}
	if backend.counterIndex != 0 {
		t.Fatal("must not aggregate all interfaces")
	}
	if s.Sources["network"].Status != state.SourceDegraded {
		t.Fatal(s.Sources["network"].Status)
	}
}
func TestOfflineCanHaveAvailableSourceHealth(t *testing.T) {
	store := newStore()
	ip := net.ParseIP("10.0.0.2")
	probe := &fakeProbe{results: []ProbeResult{{Completed: true, Reachable: true, Latency: 10 * time.Millisecond, LocalIP: ip}, {Completed: true, Reachable: false}, {Completed: true, Reachable: false}, {Completed: true, Reachable: false}}}
	backend := &fakeBackend{interfaces: map[string]string{ip.String(): "en0"}, counters: []Counter{{Interface: "en0", BytesRecv: 100, BytesSent: 100}, {Interface: "en0", BytesRecv: 110, BytesSent: 110}, {Interface: "en0", BytesRecv: 120, BytesSent: 120}, {Interface: "en0", BytesRecv: 130, BytesSent: 130}}}
	c := NewCollector(store, probe, backend, nil)
	now := time.Unix(100, 0)
	c.now = func() time.Time { now = now.Add(time.Second); return now }
	for i := 0; i < 4; i++ {
		if err := c.Collect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	s := store.Snapshot()
	if s.Network.Quality != state.NetworkOffline {
		t.Fatal(s.Network.Quality)
	}
	if s.Sources["network"].Status != state.SourceAvailable {
		t.Fatal(s.Sources["network"].Status)
	}
}
func TestComponentFailureDegradesSourceAndDoesNotTouchSystem(t *testing.T) {
	store := newStore()
	_ = store.Update(func(r *state.InternalRootState) error {
		r.Sources["system"] = state.SourceHealth{Status: state.SourceAvailable, Message: "keep"}
		return nil
	})
	ip := net.ParseIP("10.0.0.2")
	probe := &fakeProbe{results: []ProbeResult{{Completed: true, Reachable: true, Latency: 10 * time.Millisecond, LocalIP: ip}}}
	backend := &fakeBackend{interfaces: map[string]string{ip.String(): "en0"}, counterErr: errors.New("counter")}
	c := NewCollector(store, probe, backend, nil)
	c.now = func() time.Time { return time.Unix(100, 0) }
	_ = c.Collect(context.Background())
	s := store.Snapshot()
	if s.Sources["network"].Status != state.SourceDegraded {
		t.Fatal(s.Sources["network"].Status)
	}
	if s.Sources["system"].Status != state.SourceAvailable || s.Sources["system"].Message != "keep" {
		t.Fatal("system changed")
	}
}
func TestLastAttemptAndSuccessSemantics(t *testing.T) {
	store := newStore()
	ip := net.ParseIP("10.0.0.2")
	probe := &fakeProbe{results: []ProbeResult{{Completed: true, Reachable: true, Latency: 10 * time.Millisecond, LocalIP: ip}, {Completed: true, Reachable: false}}}
	backend := &fakeBackend{interfaces: map[string]string{ip.String(): "en0"}, counters: []Counter{{Interface: "en0", BytesRecv: 1, BytesSent: 1}, {Interface: "en0", BytesRecv: 1, BytesSent: 1}}}
	c := NewCollector(store, probe, backend, nil)
	times := []time.Time{time.Unix(10, 0), time.Unix(10, 0), time.Unix(20, 0), time.Unix(20, 0)}
	i := 0
	c.now = func() time.Time { v := times[i]; i++; return v }
	_ = c.Collect(context.Background())
	first := store.Snapshot().Sources["network"]
	_ = c.Collect(context.Background())
	second := store.Snapshot().Sources["network"]
	if first.LastSuccessAt == nil || second.LastSuccessAt == nil || !second.LastSuccessAt.Equal(*second.LastAttemptAt) {
		t.Fatalf("bad health: %+v", second)
	}
}
func TestInvalidProbeObservationUnknown(t *testing.T) {
	store := newStore()
	probe := &fakeProbe{results: []ProbeResult{{Completed: false, Err: errors.New("subsystem")}}}
	backend := &fakeBackend{}
	c := NewCollector(store, probe, backend, nil)
	c.now = func() time.Time { return time.Unix(1, 0) }
	_ = c.Collect(context.Background())
	s := store.Snapshot()
	if s.Network.Quality != state.NetworkUnknown || s.Network.Reachable != nil || s.Network.ProbeFailurePercent != nil {
		t.Fatalf("unexpected network: %+v", s.Network)
	}
	if s.Sources["network"].Status != state.SourceUnavailable {
		t.Fatal(s.Sources["network"].Status)
	}
}
func TestShutdownCancellationDoesNotRecordFailure(t *testing.T) {
	store := newStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := &fakeProbe{results: []ProbeResult{{Completed: false, Err: context.Canceled}}}
	c := NewCollector(store, probe, &fakeBackend{}, nil)
	c.now = func() time.Time { return time.Unix(1, 0) }
	_ = c.Collect(ctx)
	if len(c.outcomes) != 0 {
		t.Fatal("cancellation recorded outcome")
	}
	if _, ok := store.Snapshot().Sources["network"]; ok {
		t.Fatal("shutdown cancellation updated state")
	}
}
func TestNetworkUpdatePreservesAgentAndSystem(t *testing.T) {
	store := newStore()
	cpu := 12.0
	_ = store.Update(func(r *state.InternalRootState) error {
		r.Agents = []state.AgentState{{ID: "keep"}}
		r.System.CPUPercent = &cpu
		return nil
	})
	probe := &fakeProbe{results: []ProbeResult{{Completed: true, Reachable: false}}}
	c := NewCollector(store, probe, &fakeBackend{}, nil)
	c.now = func() time.Time { return time.Unix(1, 0) }
	_ = c.Collect(context.Background())
	s := store.Snapshot()
	if len(s.Agents) != 1 || s.Agents[0].ID != "keep" || s.System.CPUPercent == nil || *s.System.CPUPercent != 12 {
		t.Fatal("unrelated state lost")
	}
}

func TestRollingFailurePercentAndThreshold(t *testing.T) {
	c := &Collector{}
	for i := 0; i < 10; i++ {
		c.recordOutcome(true)
	}
	c.recordOutcome(false)
	c.recordOutcome(false)
	if got := c.failurePercent(); got <= 10 {
		t.Fatalf("failure percent = %v", got)
	}
	c.recordOutcome(true)
	if got := c.quality(ProbeResult{Completed: true, Reachable: true, Latency: 20 * time.Millisecond}); got != state.NetworkDegraded {
		t.Fatalf("quality = %s", got)
	}
}

func TestConcurrentAgentAndSystemUpdatesPreserved(t *testing.T) {
	store := newStore()
	entered := make(chan struct{})
	release := make(chan struct{})
	probe := ProbeFunc(func(context.Context) ProbeResult {
		close(entered)
		<-release
		return ProbeResult{Completed: true, Reachable: false}
	})
	c := NewCollector(store, probe, &fakeBackend{}, nil)
	c.now = func() time.Time { return time.Unix(100, 0) }
	done := make(chan error, 1)
	go func() { done <- c.Collect(context.Background()) }()
	<-entered
	cpu := 37.0
	if err := store.Update(func(r *state.InternalRootState) error {
		r.Agents = []state.AgentState{{ID: "agent-concurrent"}}
		r.System.CPUPercent = &cpu
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s := store.Snapshot()
	if len(s.Agents) != 1 || s.Agents[0].ID != "agent-concurrent" {
		t.Fatalf("agent update lost: %+v", s.Agents)
	}
	if s.System.CPUPercent == nil || *s.System.CPUPercent != 37 {
		t.Fatalf("system update lost: %+v", s.System.CPUPercent)
	}
}

type ProbeFunc func(context.Context) ProbeResult

func (f ProbeFunc) Probe(ctx context.Context) ProbeResult { return f(ctx) }
