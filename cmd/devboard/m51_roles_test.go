package main

import (
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
)

func TestM51NodeRuntimeNeverPollsPeers(t *testing.T) {
	for _, mock := range []bool{false, true} {
		plan := planRuntime(config.RuntimeRoleNode, mock, 3)
		if !plan.localAuthority || plan.peerPolling {
			t.Fatalf("node plan mock=%v: %+v", mock, plan)
		}
		if plan.agentIngest == mock {
			t.Fatalf("node agent ingest mock=%v: %+v", mock, plan)
		}
	}
}

func TestM51HubRuntimeNeverStartsLocalAuthority(t *testing.T) {
	live := planRuntime(config.RuntimeRoleHub, false, 2)
	if live.localAuthority || live.agentIngest || !live.peerPolling {
		t.Fatalf("live hub plan: %+v", live)
	}
	zero := planRuntime(config.RuntimeRoleHub, false, 0)
	if zero.localAuthority || zero.agentIngest || zero.peerPolling {
		t.Fatalf("zero-peer hub plan: %+v", zero)
	}
	mock := planRuntime(config.RuntimeRoleHub, true, 2)
	if mock.localAuthority || mock.agentIngest || mock.peerPolling {
		t.Fatalf("mock hub plan: %+v", mock)
	}
}
