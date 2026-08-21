package web

import (
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

func TestKindleSystemBarUsesDegradedPartialMetrics(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 16, 0, 0, time.Local)
	cpu := 24.0
	memUsed := uint64(14 * 1024 * 1024 * 1024)
	memTotal := uint64(24 * 1024 * 1024 * 1024)
	swapUsed := uint64(0)
	swapTotal := uint64(4 * 1024 * 1024 * 1024)
	memPct := 58.3
	swapPct := 0.0
	pub := state.PublicState{
		System: state.PublicSystem{
			CPUPercent:    &cpu,
			Memory:        state.PublicMetricSet{UsedBytes: &memUsed, TotalBytes: &memTotal, PercentUsed: &memPct},
			Swap:          state.PublicMetricSet{UsedBytes: &swapUsed, TotalBytes: &swapTotal, PercentUsed: &swapPct},
			Disk:          state.PublicMetricSet{},
			ProcessGroups: []state.PublicProcessGroup{},
		},
		Sources: map[string]state.PublicSourceHealth{
			"system": {Status: state.SourceDegraded, Message: "Embedded system metrics collector is partially available."},
		},
	}

	vm := BuildKindleViewModel(pub, now, false, "landscape", "none")
	if !vm.SystemConnected {
		t.Fatal("degraded system source with usable metrics should remain connected")
	}
	want := "CPU 24% | MEM 14/24G | SWAP 0/4G | DISK N/A | 11:16"
	if vm.SystemBar != want {
		t.Fatalf("system bar=%q want %q", vm.SystemBar, want)
	}
}
