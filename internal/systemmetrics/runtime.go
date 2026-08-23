package systemmetrics

import (
	"context"
	"sync"
	"time"
)

const DefaultSampleInterval = 5 * time.Second

type Runtime struct {
	collector *Collector
	interval  time.Duration
	cancel    context.CancelFunc
	stopOnce  sync.Once
	done      chan struct{}
}

func Start(collector *Collector, interval time.Duration) *Runtime {
	if interval <= 0 {
		interval = DefaultSampleInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{collector: collector, interval: interval, cancel: cancel, done: make(chan struct{})}
	_ = collector.Collect(ctx)
	go r.loop(ctx)
	return r
}

func (r *Runtime) loop(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = r.collector.Collect(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (r *Runtime) Close() error {
	r.stopOnce.Do(r.cancel)
	<-r.done
	return nil
}
