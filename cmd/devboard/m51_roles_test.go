package main

import (
	"testing"

	"github.com/Lost0rz/DevBoard/internal/config"
)

func TestM51NodeRuntimeNeverPollsPeers(t *testing.T) {
	for _, mock := range []bool{false, true} {
		plan := planRuntime(config.RuntimeRoleNode, mock)
		if !plan.localAuthority || plan.hubReceiver {
			t.Fatalf("node plan mock=%v: %+v", mock, plan)
		}
		if plan.agentIngest == mock {
			t.Fatalf("node agent ingest mock=%v: %+v", mock, plan)
		}
	}
}

func TestM51HubRuntimeNeverStartsLocalAuthority(t *testing.T) {
	live := planRuntime(config.RuntimeRoleHub, false)
	if live.localAuthority || live.agentIngest {
		t.Fatalf("live hub plan: %+v", live)
	}
	mock := planRuntime(config.RuntimeRoleHub, true)
	if mock.localAuthority || mock.agentIngest || mock.hubReceiver {
		t.Fatalf("mock hub plan: %+v", mock)
	}
}
