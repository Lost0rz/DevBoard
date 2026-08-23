package networkmetrics

import (
	"context"
	"net"
	"time"
)

type ProbeResult struct {
	Completed bool
	Reachable bool
	Latency   time.Duration
	LocalIP   net.IP
	Err       error
}

type Probe interface {
	Probe(context.Context) ProbeResult
}

type TCPProbe struct {
	address string
	timeout time.Duration
	dialer  net.Dialer
	now     func() time.Time
}

func NewTCPProbe(address string, timeout time.Duration) *TCPProbe {
	return &TCPProbe{address: address, timeout: timeout, now: time.Now}
}

func (p *TCPProbe) Probe(ctx context.Context) ProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	started := p.now()
	conn, err := p.dialer.DialContext(probeCtx, "tcp", p.address)
	elapsed := p.now().Sub(started)
	if err != nil {
		if ctx.Err() != nil {
			return ProbeResult{Err: ctx.Err()}
		}
		return ProbeResult{Completed: true, Reachable: false, Err: err}
	}
	defer conn.Close()
	result := ProbeResult{Completed: true, Reachable: true, Latency: elapsed}
	if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		result.LocalIP = append(net.IP(nil), addr.IP...)
	}
	return result
}
