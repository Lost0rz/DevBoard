package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/networkmetrics"
	"github.com/Lost0rz/DevBoard/internal/state"
)

type panicNetworkBackend struct{}

func (panicNetworkBackend) InterfaceForIP(net.IP) (string, error) {
	panic("real network collector started in mock mode")
}
func (panicNetworkBackend) Counter(context.Context, string) (networkmetrics.Counter, error) {
	panic("real network collector started in mock mode")
}

func TestMockModeDoesNotStartNetworkMetrics(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	mock := state.MockInternalState(now, state.HostState{ID: "mock-host"})
	store := state.NewStore(mock)
	before := store.Snapshot()

	if runtime := startNetworkMetrics(true, store, nil, config.Defaults().Network, panicNetworkBackend{}); runtime != nil {
		t.Fatal("mock mode unexpectedly started network metrics runtime")
	}
	after := store.Snapshot()
	if after.GeneratedAt != before.GeneratedAt || after.Network.Quality != state.NetworkGood {
		t.Fatal("mock state changed while network metrics were isolated")
	}
}
