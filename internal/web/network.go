package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type DesktopViewModel struct {
	ViewModel
	Network NetworkView
	Tasks   []TaskView
}

type NetworkView struct {
	Quality      string
	Reachable    string
	Latency      string
	Failure      string
	Receive      string
	Send         string
	SourceStatus string
}

func buildDesktopViewModel(pub state.PublicState, now time.Time, mock bool, layout string) DesktopViewModel {
	return DesktopViewModel{
		ViewModel: BuildViewModel(pub, now, mock, layout),
		Network:   buildNetworkView(pub),
		Tasks:     buildTaskViews(pub.Tasks, now),
	}
}

func buildNetworkView(pub state.PublicState) NetworkView {
	quality := strings.ToUpper(string(pub.Network.Quality))
	if quality == "" {
		quality = strings.ToUpper(string(state.NetworkUnknown))
	}
	sourceStatus := string(state.SourceUnavailable)
	if source, ok := pub.Sources["network"]; ok {
		sourceStatus = string(source.Status)
	}
	return NetworkView{Quality: quality, Reachable: formatReachable(pub.Network.Reachable), Latency: formatMilliseconds(pub.Network.ConnectLatencyMs), Failure: formatFailurePercent(pub.Network.ProbeFailurePercent), Receive: formatByteRate(pub.Network.ReceiveBytesPerSecond), Send: formatByteRate(pub.Network.SendBytesPerSecond), SourceStatus: sourceStatus}
}

func formatReachable(v *bool) string {
	if v == nil {
		return "N/A"
	}
	if *v {
		return "YES"
	}
	return "NO"
}
func formatMilliseconds(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.0f ms", *v)
}
func formatFailurePercent(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 100 {
		return "N/A"
	}
	if math.Abs(*v-math.Round(*v)) < 0.05 {
		return fmt.Sprintf("%.0f%%", *v)
	}
	return fmt.Sprintf("%.1f%%", *v)
}
func formatByteRate(v *float64) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return "N/A"
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case *v >= gib:
		return fmt.Sprintf("%.1f GiB/s", *v/gib)
	case *v >= mib:
		return fmt.Sprintf("%.1f MiB/s", *v/mib)
	case *v >= kib:
		return fmt.Sprintf("%.1f KiB/s", *v/kib)
	default:
		return fmt.Sprintf("%.0f B/s", *v)
	}
}
