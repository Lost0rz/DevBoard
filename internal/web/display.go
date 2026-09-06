package web

import (
	"time"

	"github.com/Lost0rz/DevBoard/internal/dashboard"
)

// DisplayViewModel is the responsive colour presentation for /display. Its
// task, host, and quota slices intentionally come from the same compact
// projection as Kindle so the two surfaces show the same operational facts.
// The template is separate because a web display can use colour, CSS grid,
// and a fluid viewport while Kindle must remain fixed and monochrome.
type DisplayViewModel struct {
	Mock            bool
	Updated         string
	HubStatus       string
	HostSummary     string
	ReturnPath      string
	HiddenTaskCount int
	Tasks           []KindleDemoTaskView
	Hosts           []KindleDemoHostView
	Quota           []KindleDemoQuotaView
	QuotaConnected  bool
	// Legacy keeps the non-visible compatibility contract consumed by older
	// integrations and regression checks. It is not the source for the colour
	// surface; the visible template uses the Kindle-compatible fields above.
	Legacy DashboardDesktopViewModel

	ProductRole    string
	FragmentPath   string
	RefreshSeconds int
}

func buildDisplayViewModel(model dashboard.State, now time.Time, mock bool, returnPath string) DisplayViewModel {
	return buildDisplayViewModelWithTimezone(model, now, mock, returnPath, "")
}

func buildDisplayViewModelWithTimezone(model dashboard.State, now time.Time, mock bool, returnPath, timezone string) DisplayViewModel {
	kindle := buildKindleDemoViewModelWithTimezone(model, now, mock, "right", timezone)
	kindle.ReturnPath = returnPath
	legacy := buildDashboardViewModelWithTimezone(model, now, mock, timezone)
	legacy.RefreshSeconds = 0
	legacy.ReturnPath = returnPath
	return DisplayViewModel{
		Mock:            kindle.Mock,
		Updated:         kindle.Updated,
		HubStatus:       kindle.HubStatus,
		HostSummary:     kindle.HostSummary,
		ReturnPath:      kindle.ReturnPath,
		HiddenTaskCount: kindle.HiddenTaskCount,
		Tasks:           kindle.Tasks,
		Hosts:           kindle.Hosts,
		Quota:           kindle.Quota,
		QuotaConnected:  kindle.QuotaConnected,
		Legacy:          legacy,
	}
}
