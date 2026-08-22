package networkmetrics

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

const probeWindowSize = 12

type trafficBaseline struct {
	interfaceName string
	recv          uint64
	sent          uint64
	at            time.Time
}

type Collector struct {
	store    *state.Store
	probe    Probe
	backend  Backend
	logger   *slog.Logger
	now      func() time.Time
	outcomes []bool
	baseline *trafficBaseline
	route    string
}

func NewCollector(store *state.Store, probe Probe, backend Backend, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Collector{store: store, probe: probe, backend: backend, logger: logger, now: time.Now}
}

func (c *Collector) Collect(ctx context.Context) error {
	attemptAt := c.now().UTC()
	probeResult := c.probe.Probe(ctx)
	if ctx.Err() != nil && !probeResult.Completed {
		return nil
	}

	probeOK := probeResult.Completed
	if probeResult.Err != nil && probeOK {
		c.logger.Warn("network TCP probe failed", "err", probeResult.Err)
	}

	network := state.NetworkState{Quality: state.NetworkUnknown}
	if probeOK {
		reachable := probeResult.Reachable
		network.Reachable = &reachable
		if reachable {
			latency := float64(probeResult.Latency) / float64(time.Millisecond)
			network.ConnectLatencyMs = &latency
		}
		c.recordOutcome(reachable)
		failure := c.failurePercent()
		network.ProbeFailurePercent = &failure
		network.Quality = c.quality(probeResult)
	}

	if probeResult.Completed && probeResult.Reachable {
		iface, err := c.backend.InterfaceForIP(probeResult.LocalIP)
		if err != nil {
			c.logger.Warn("network route interface selection failed", "err", err)
			c.route = ""
			c.baseline = nil
		} else if iface != c.route {
			c.route = iface
			c.baseline = nil
		}
	}

	counterOK := false
	if c.route != "" {
		counter, err := c.backend.Counter(ctx, c.route)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Warn("network interface counter collection failed", "err", err)
		} else if counter.Interface == c.route {
			counterOK = true
			counterAt := c.now().UTC()
			network.ReceiveBytesPerSecond, network.SendBytesPerSecond = c.rates(counter, counterAt)
		}
	}

	if !probeOK {
		network.Quality = state.NetworkUnknown
		network.Reachable = nil
		network.ConnectLatencyMs = nil
		network.ProbeFailurePercent = nil
	}

	return c.store.Update(func(root *state.InternalRootState) error {
		if root.Sources == nil {
			root.Sources = make(map[string]state.SourceHealth)
		}
		root.Network = network
		root.Sources["network"] = reduceHealth(root.Sources["network"], attemptAt, probeOK, counterOK)
		root.GeneratedAt = attemptAt
		return nil
	})
}

func (c *Collector) recordOutcome(success bool) {
	c.outcomes = append(c.outcomes, success)
	if len(c.outcomes) > probeWindowSize {
		c.outcomes = append([]bool(nil), c.outcomes[len(c.outcomes)-probeWindowSize:]...)
	}
}

func (c *Collector) failurePercent() float64 {
	if len(c.outcomes) == 0 {
		return 0
	}
	failures := 0
	for _, success := range c.outcomes {
		if !success {
			failures++
		}
	}
	return float64(failures) * 100 / float64(len(c.outcomes))
}

func (c *Collector) consecutiveFailures() int {
	count := 0
	for i := len(c.outcomes) - 1; i >= 0 && !c.outcomes[i]; i-- {
		count++
	}
	return count
}

func (c *Collector) quality(latest ProbeResult) state.NetworkQuality {
	if len(c.outcomes) == 0 || !latest.Completed {
		return state.NetworkUnknown
	}
	if c.consecutiveFailures() >= 3 {
		return state.NetworkOffline
	}
	failure := c.failurePercent()
	if !latest.Reachable || failure > 10 || latest.Latency > 500*time.Millisecond {
		return state.NetworkDegraded
	}
	return state.NetworkGood
}

func (c *Collector) rates(counter Counter, at time.Time) (*float64, *float64) {
	if c.baseline == nil || c.baseline.interfaceName != counter.Interface {
		c.baseline = &trafficBaseline{interfaceName: counter.Interface, recv: counter.BytesRecv, sent: counter.BytesSent, at: at}
		return nil, nil
	}
	elapsed := at.Sub(c.baseline.at).Seconds()
	if elapsed <= 0 || counter.BytesRecv < c.baseline.recv || counter.BytesSent < c.baseline.sent {
		c.baseline = &trafficBaseline{interfaceName: counter.Interface, recv: counter.BytesRecv, sent: counter.BytesSent, at: at}
		return nil, nil
	}
	recv := float64(counter.BytesRecv-c.baseline.recv) / elapsed
	sent := float64(counter.BytesSent-c.baseline.sent) / elapsed
	c.baseline = &trafficBaseline{interfaceName: counter.Interface, recv: counter.BytesRecv, sent: counter.BytesSent, at: at}
	return &recv, &sent
}

func reduceHealth(previous state.SourceHealth, attemptAt time.Time, probeOK, counterOK bool) state.SourceHealth {
	health := state.SourceHealth{LastAttemptAt: &attemptAt, LastSuccessAt: previous.LastSuccessAt}
	switch {
	case probeOK && counterOK:
		health.Status = state.SourceAvailable
		health.LastSuccessAt = &attemptAt
		health.Message = "Network health collector is available."
	case probeOK || counterOK:
		health.Status = state.SourceDegraded
		health.Message = "Network health collector is partially available."
	default:
		health.Status = state.SourceUnavailable
		health.Message = "Network health collector is unavailable."
	}
	return health
}
