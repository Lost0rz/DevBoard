package networkmetrics

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeStartsFirstSampleAsynchronouslyAndSerializesCycles(t *testing.T) {
	store := newStore()
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	var active int32
	var maxActive int32
	probe := ProbeFunc(func(context.Context) ProbeResult {
		current := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if current <= old || atomic.CompareAndSwapInt32(&maxActive, old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(&active, -1)
		return ProbeResult{Completed: true, Reachable: true, Latency: time.Millisecond, LocalIP: net.ParseIP("127.0.0.1")}
	})
	backend := &fakeBackend{interfaces: map[string]string{"127.0.0.1": "lo0"}, counters: []Counter{{Interface: "lo0"}, {Interface: "lo0"}}}
	collector := NewCollector(store, probe, backend, nil)
	started := time.Now()
	runtime := Start(collector, 5*time.Millisecond)
	if time.Since(started) > 50*time.Millisecond {
		t.Fatal("runtime start blocked on first network sample")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first sample did not start immediately")
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second sample did not run")
	}
	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("overlapping network cycles: max active = %d", got)
	}
	release <- struct{}{}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
