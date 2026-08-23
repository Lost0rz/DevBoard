package main

import (
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
)

// M5.2 froze the authority split: only a NODE runs the uplink runtime, and
// the synthetic mock run must never push state to a real hub.
func TestM54NodeUplinkWantedOnlyForLiveNodeRole(t *testing.T) {
	cases := []struct {
		role    config.RuntimeRole
		mock    bool
		enabled bool
		want    bool
	}{
		{config.RuntimeRoleNode, false, true, true},
		{config.RuntimeRoleNode, false, false, false},
		{config.RuntimeRoleNode, true, true, false},
		{config.RuntimeRoleHub, false, true, false},
	}
	for _, tc := range cases {
		if got := nodeUplinkWanted(tc.role, tc.mock, tc.enabled); got != tc.want {
			t.Fatalf("nodeUplinkWanted(%s, mock=%v, enabled=%v) = %v, want %v", tc.role, tc.mock, tc.enabled, got, tc.want)
		}
	}
}

// The uplink config boundary is enforced at validation time: a hub config
// with an uplink section must never load.
func TestM54UplinkConfigRejectedForHubRole(t *testing.T) {
	cfg := config.Defaults()
	cfg.Runtime.Role = config.RuntimeRoleHub
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "https://hub.example.com", NodeID: "mac-a", Token: "token-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := config.Validate(cfg); err == nil {
		t.Fatalf("hub role must not configure an uplink")
	}
}
