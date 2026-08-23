package main

import (
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/hub"
)

// M5.2 froze the push topology: production hub authority is the receiver plus
// push-native node store. The runtime plan must never reintroduce hub-origin
// polling, and the hub runtime itself must not start pollers or collectors.
func TestM53HubProductionPlanOwnsReceiverWithoutPolling(t *testing.T) {
	live := planRuntime(config.RuntimeRoleHub, false)
	if !live.hubReceiver {
		t.Fatalf("live hub plan lacks receiver authority: %+v", live)
	}
	node := planRuntime(config.RuntimeRoleNode, false)
	if node.hubReceiver {
		t.Fatalf("node plan must not own hub receiver: %+v", node)
	}
}

func TestM53HubNodeConfigsRespectDisabledList(t *testing.T) {
	cfg := config.Defaults()
	cfg.Runtime.Role = config.RuntimeRoleHub
	cfg.Nodes = config.NodesConfig{
		Registered: []config.NodeConfig{
			{NodeID: "mac-a", DisplayName: "Mac A", Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{NodeID: "mac-b", DisplayName: "Mac B", Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
		Disabled: []string{"mac-b"},
	}
	entries := hubNodeConfigs(cfg)
	if len(entries) != 2 || entries[0].NodeID != "mac-a" || entries[1].NodeID != "mac-b" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if !entries[0].Enabled || entries[1].Enabled {
		t.Fatalf("disabled list not applied: %#v", entries)
	}
	if _, err := hub.NewRuntime(entries, nil, nil); err != nil {
		t.Fatalf("runtime rejected valid registry: %v", err)
	}
}
