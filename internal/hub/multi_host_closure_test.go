package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
	"github.com/Lost0rz/DevBoard/internal/uplink"
)

func TestHubGlobalQuotaDedupPrefersHealthyObservation(t *testing.T) {
	const tokenA = "closure-token-mac-a-aaaaaaaaaaaaaaaaaaaaaaaa"
	const tokenB = "closure-token-mac-b-bbbbbbbbbbbbbbbbbbbbbbbb"
	rt, err := NewRuntime([]NodeConfig{
		{NodeID: "mac-a", DisplayName: "Studio Mac", Enabled: true, Token: tokenA},
		{NodeID: "mac-b", DisplayName: "Laptop", Enabled: true, Token: tokenB},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	post := func(nodeID, token string, sourceStatus state.SourceStatus) {
		used := 25.0
		windows := []state.PublicQuotaWindow{{Name: "5H", UsedPercent: &used}}
		envelope := uplink.NodeSnapshot{
			SchemaVersion: 1, StateKind: "nodeSnapshot", NodeID: nodeID,
			SessionID: strings.Repeat(string(nodeID[len(nodeID)-1]), 32), Sequence: 1, SentAt: now,
			State: state.PublicState{
				SchemaVersion: 1, StateKind: "public", GeneratedAt: now,
				Host:    state.PublicHost{ID: nodeID},
				Quota:   []state.PublicQuota{{Provider: "codex", AccountKey: "acct_same", DisplayLabel: "Codex A", Windows: &windows, SourceStatus: sourceStatus, SampledAt: &now, ObservedBy: nodeID}},
				Sources: map[string]state.PublicSourceHealth{"quota": {Status: sourceStatus}},
			},
		}
		body, _ := json.Marshal(envelope)
		req := httptest.NewRequest(http.MethodPost, SnapshotRoute, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
			t.Fatalf("node %s status=%d body=%s", nodeID, rec.Code, rec.Body.String())
		}
	}
	post("mac-a", tokenA, state.SourceDegraded)
	post("mac-b", tokenB, state.SourceAvailable)
	dash := rt.Store().Dashboard(now.Add(time.Second))
	if len(dash.Quota) != 1 || dash.Quota[0].AccountKey != "acct_same" || dash.Quota[0].SourceStatus != state.SourceAvailable || dash.Quota[0].ObservedBy != "mac-b" {
		t.Fatalf("global quota=%+v", dash.Quota)
	}
}
