package networkmetrics

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPProbeSuccessAndLatency(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
		close(accepted)
	}()
	p := NewTCPProbe(listener.Addr().String(), time.Second)
	result := p.Probe(context.Background())
	if !result.Completed || !result.Reachable || result.Latency < 0 || result.LocalIP == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	<-accepted
}

func TestTCPProbeFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	p := NewTCPProbe(address, 200*time.Millisecond)
	result := p.Probe(context.Background())
	if !result.Completed || result.Reachable || result.Err == nil || result.Latency != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestTCPProbeShutdownCancellationNotCompleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewTCPProbe("192.0.2.1:443", time.Second)
	result := p.Probe(ctx)
	if result.Completed || result.Reachable || result.Err != context.Canceled {
		t.Fatalf("unexpected result: %+v", result)
	}
}
