package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
	"github.com/Lost0rz/DevBoard/internal/state"
)

const kindleRefreshSeconds = 2

// KindleDemoViewModel is intentionally smaller than the Pad view model. It is
// rendered with old-WebKit-safe tables and contains only the information that
// can be read on the fixed 890x750 landscape canvas used by the e-ink
// presentation.
type KindleDemoViewModel struct {
	Mock            bool
	Rotate          string
	RotationClass   string
	CanvasClass     string
	Refresh         int
	Updated         string
	HubStatus       string
	HostSummary     string
	HiddenTaskCount int
	Tasks           []KindleDemoTaskView
	Hosts           []KindleDemoHostView
	Quota           []KindleDemoQuotaView
	QuotaConnected  bool
}

type KindleDemoTaskView struct {
	State           string
	StateClass      string
	Title           string
	DetailLabel     string
	Detail          string
	SupplementLabel string
	Supplement      string
	Host            string
	Provider        string
	ProviderGlyph   string
	Age             string
}

type KindleDemoHostView struct {
	Label           string
	Connection      string
	ConnectionClass string
	Metrics         []KindleDemoMetricView
}

type KindleDemoMetricView struct {
	Name  string
	Value string
}

type KindleDemoQuotaView struct {
	Label   string
	Windows []KindleDemoQuotaWindowView
}

type KindleDemoQuotaWindowView struct {
	Label       string
	Bar         string
	Remaining   string
	FillWidth   string
	StatusClass string
}

// kindleDemoRequestRotate accepts only the two canonical URLs for a device
// physically mounted either way around: /kindle/R or /kindle/L.
func kindleDemoRequestRotate(r *http.Request) (string, bool) {
	if r.URL.RawQuery != "" {
		return "", false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch path {
	case "/kindle/R":
		return "right", true
	case "/kindle/L":
		return "left", true
	default:
		return "", false
	}
}

func buildKindleDemoViewModel(model dashboard.State, now time.Time, mock bool, rotate string) KindleDemoViewModel {
	pad := buildPadDashboardViewModel(model, now)

	vm := KindleDemoViewModel{
		Mock:           mock,
		Rotate:         rotate,
		RotationClass:  "kindle-rotate-" + rotate,
		CanvasClass:    "kindle-fixed-890",
		Refresh:        kindleRefreshSeconds,
		Updated:        now.Local().Format("15:04"),
		HubStatus:      pad.Connection.HubStatus,
		HostSummary:    fmt.Sprintf("%d/%d", pad.Connection.OnlineCount, pad.Connection.HostCount),
		Tasks:          make([]KindleDemoTaskView, 0, 2),
		Hosts:          make([]KindleDemoHostView, 0, 2),
		Quota:          make([]KindleDemoQuotaView, 0, 3),
		QuotaConnected: false,
	}

	for i, task := range pad.Tasks {
		if i >= 2 {
			vm.HiddenTaskCount++
			continue
		}
		provider := task.Provider
		glyph := "C"
		if provider == "CLAUDE CODE" {
			glyph = "A"
		}
		vm.Tasks = append(vm.Tasks, KindleDemoTaskView{
			State:           task.State,
			StateClass:      kindleDemoStateClass(task.State),
			Title:           task.Title,
			DetailLabel:     task.DetailLabel,
			Detail:          task.Detail,
			SupplementLabel: task.SupplementLabel,
			Supplement:      task.Supplement,
			Host:            kindleDemoHostLabel(task.HostDisplayName, task.HostID, task.HostLabel),
			Provider:        provider,
			ProviderGlyph:   glyph,
			Age:             task.Age,
		})
	}
	vm.HiddenTaskCount += pad.HiddenTaskCount

	for i, host := range pad.Hosts {
		if i >= 2 {
			break
		}
		vm.Hosts = append(vm.Hosts, KindleDemoHostView{
			Label:           kindleDemoHostLabel("", "", host.Label),
			Connection:      host.Connection,
			ConnectionClass: kindleDemoConnectionClass(host.Connection),
			Metrics: []KindleDemoMetricView{
				kindleDemoMetric("CPU", host.CPU),
				kindleDemoMetric("MEM", host.Memory),
				kindleDemoMetric("SW", host.Swap),
				kindleDemoMetric("D", host.Disk),
			},
		})
	}

	vm.Quota, vm.QuotaConnected = buildKindleDemoQuota(model, now)
	if mock {
		kindleDemoFixture(&vm)
	}
	return vm
}

// kindleDemoFixture keeps the isolated mock route visually useful even when
// the generic mock state has no active tasks or quota samples. It is never
// used by a real node or hub runtime.
func kindleDemoFixture(vm *KindleDemoViewModel) {
	if len(vm.Hosts) < 2 {
		vm.Hosts = append(vm.Hosts, KindleDemoHostView{
			Label: "Mac B", Connection: "ONLINE", ConnectionClass: "kindle-connection-online",
			Metrics: []KindleDemoMetricView{
				{Name: "CPU", Value: "42%"},
				{Name: "MEM", Value: "61%"},
				{Name: "SW", Value: "4%"},
				{Name: "D", Value: "78%"},
			},
		})
		vm.HostSummary = fmt.Sprintf("%d/%d", len(vm.Hosts), len(vm.Hosts))
	}
	if len(vm.Tasks) == 0 {
		vm.Tasks = []KindleDemoTaskView{
			{State: "WORKING", StateClass: "kindle-state-working", Title: "Kindle display layout", DetailLabel: "CHECKPOINT", Detail: "compact landscape view", Host: "Mac A", Provider: "CODEX", ProviderGlyph: "C", Age: "<1M"},
			{State: "READY", StateClass: "kindle-state-ready", Title: "Review quota status", DetailLabel: "ACTION REQUIRED", Detail: "confirm account", SupplementLabel: "LAST PROGRESS", Supplement: "Inspecting the current quota source", Host: "Mac A", Provider: "CLAUDE CODE", ProviderGlyph: "A", Age: "2M"},
		}
	}
	if len(vm.Quota) == 0 {
		vm.Quota = []KindleDemoQuotaView{
			{Label: "CODEX A", Windows: []KindleDemoQuotaWindowView{{Label: "5H", Bar: "##..", Remaining: "52%", FillWidth: "52%", StatusClass: "kindle-quota-good"}, {Label: "WEEK", Bar: "###.", Remaining: "78%", FillWidth: "78%", StatusClass: "kindle-quota-good"}}},
			{Label: "CODEX B", Windows: []KindleDemoQuotaWindowView{{Label: "5H", Bar: "#...", Remaining: "24%", FillWidth: "24%", StatusClass: "kindle-quota-warn"}, {Label: "WEEK", Bar: "##..", Remaining: "49%", FillWidth: "49%", StatusClass: "kindle-quota-warn"}}},
			{Label: "GLM", Windows: []KindleDemoQuotaWindowView{{Label: "5H", Bar: "####", Remaining: "100%", FillWidth: "100%", StatusClass: "kindle-quota-good"}, {Label: "WEEK", Bar: "##..", Remaining: "65%", FillWidth: "65%", StatusClass: "kindle-quota-good"}}},
		}
		vm.QuotaConnected = true
	}
}

// buildKindleDemoQuota deliberately bypasses the Pad's compact GLM projection.
// The Kindle contract shows the two CodexBar rate windows for every provider:
// the short five-hour allowance and the weekly allowance. Hub dashboards use
// their already deduplicated global quota; node/legacy dashboards fall back to
// the accepted host snapshots and deduplicate by provider/account identity.
func buildKindleDemoQuota(model dashboard.State, now time.Time) ([]KindleDemoQuotaView, bool) {
	source := append([]state.PublicQuota(nil), model.Quota...)
	if len(source) == 0 {
		seen := make(map[string]struct{})
		for _, host := range model.Hosts {
			if host.State == nil {
				continue
			}
			for index, quota := range host.State.Quota {
				if quota.SourceStatus != state.SourceAvailable && quota.SourceStatus != state.SourceDegraded {
					continue
				}
				key := quota.Provider + "\x00" + quota.AccountKey
				if quota.AccountKey == "" {
					key = fmt.Sprintf("%s\x00%s\x00%d", key, host.ConfiguredHostID, index)
				}
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				source = append(source, quota)
			}
		}
	}

	views, _ := buildQuota(source, now)
	out := make([]KindleDemoQuotaView, 0, 3)
	for index, quota := range views {
		if len(out) >= 3 || index >= len(source) {
			break
		}
		if source[index].SourceStatus != state.SourceAvailable && source[index].SourceStatus != state.SourceDegraded {
			continue
		}
		if len(quota.Windows) == 0 {
			continue
		}
		label := strings.TrimSpace(source[index].DisplayLabel)
		if label == "" {
			label = strings.TrimSpace(quota.Provider)
		}
		out = append(out, kindleDemoQuotaView(label, quota.Windows))
	}
	return out, len(out) > 0
}

func kindleDemoHostLabel(displayName, hostID, fallback string) string {
	if strings.TrimSpace(displayName) != "" && strings.TrimSpace(hostID) != "" {
		return strings.TrimSpace(displayName)
	}
	if strings.TrimSpace(displayName) != "" {
		return strings.TrimSpace(displayName)
	}
	if separator := strings.Index(fallback, " · "); separator > 0 {
		return strings.TrimSpace(fallback[:separator])
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "MAC"
}

func kindleDemoMetric(name string, metric PadMetricView) KindleDemoMetricView {
	return KindleDemoMetricView{Name: name, Value: metric.Percent}
}

func kindlePixelBar(percent int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	// Four pixels are enough to show direction on a 600x800 Kindle while
	// leaving room for the account label, window label and exact percentage.
	filled := (percent + 12) / 25
	if filled > 4 {
		filled = 4
	}
	return strings.Repeat("#", filled) + strings.Repeat(".", 4-filled)
}

func kindleDemoStateClass(state string) string {
	switch state {
	case "READY":
		return "kindle-state-ready"
	case "WORKING":
		return "kindle-state-working"
	case "COMPLETE":
		return "kindle-state-complete"
	default:
		return "kindle-state-idle"
	}
}

func kindleDemoConnectionClass(connection string) string {
	switch connection {
	case "ONLINE":
		return "kindle-connection-online"
	case "STALE":
		return "kindle-connection-stale"
	default:
		return "kindle-connection-offline"
	}
}

func kindleDemoQuotaClass(percent int) string {
	switch {
	case percent <= 10:
		return "kindle-quota-low"
	case percent <= 35:
		return "kindle-quota-warn"
	default:
		return "kindle-quota-good"
	}
}

func kindleDemoQuotaView(label string, windows []QuotaWindowView) KindleDemoQuotaView {
	view := KindleDemoQuotaView{Label: label, Windows: make([]KindleDemoQuotaWindowView, 0, 2)}
	var slots [2]*QuotaWindowView
	for index := range windows {
		window := windows[index]
		name := strings.ToLower(strings.TrimSpace(window.Name))
		if strings.Contains(name, "mcp") {
			continue
		}
		slot := -1
		switch {
		case strings.Contains(name, "week"), strings.Contains(name, "weekly"), strings.Contains(name, "secondary"), strings.Contains(name, "7d"), strings.Contains(name, "10080"):
			slot = 1
		case strings.Contains(name, "5h"), strings.Contains(name, "primary"), strings.Contains(name, "five"), strings.Contains(name, "300"):
			slot = 0
		}
		if slot < 0 || slots[slot] != nil {
			if slots[0] == nil {
				slot = 0
			} else if slots[1] == nil {
				slot = 1
			} else {
				continue
			}
		}
		copyWindow := window
		slots[slot] = &copyWindow
	}
	for index, window := range slots {
		windowLabel := "5H"
		if index == 1 {
			windowLabel = "WEEK"
		}
		if window == nil {
			view.Windows = append(view.Windows, KindleDemoQuotaWindowView{Label: windowLabel, Bar: "....", Remaining: "--", FillWidth: "0%", StatusClass: "kindle-quota-empty"})
			continue
		}
		view.Windows = append(view.Windows, KindleDemoQuotaWindowView{
			Label: windowLabel, Bar: kindlePixelBar(window.RemainingPercent),
			Remaining: window.RemainingValue, FillWidth: fmt.Sprintf("%d%%", clampKindlePercent(window.RemainingPercent)), StatusClass: kindleDemoQuotaClass(window.RemainingPercent),
		})
	}
	return view
}

func clampKindlePercent(percent int) int {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
