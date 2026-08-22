package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func stateGeneratedAt(t *testing.T, dashboardJSON, nodeID string) string {
	t.Helper()
	wrapper := hostWrapper(t, dashboardJSON, nodeID)
	state := wrapper["state"].(map[string]any)
	return state["generatedAt"].(string)
}

// M5.2 §39 acceptance 13-18: session/sequence ordering, idempotency and
// cross-session non-regression.
func TestM53FakeNodeOrderingAndReplay(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	rec := fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil)
	requireStatus(t, rec, http.StatusOK)

	// Exact retry of the accepted tuple is idempotent and keeps the accepted
	// state value.
	rec = fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil)
	requireStatus(t, rec, http.StatusOK)
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != m53Base.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("duplicate replaced state: %s", got)
	}

	// Same tuple, different body: conflict, no mutation.
	rec = fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, func(env map[string]any) {
		env["state"].(map[string]any)["host"].(map[string]any)["displayName"] = "Different"
	})
	requireStatus(t, rec, http.StatusConflict)
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != m53Base.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("conflict mutated state: %s", got)
	}

	// Higher sequence in the same session advances.
	clock.Advance(time.Second)
	rec = fn.postSnapshot("mac-a", sessionAlpha, 2, clock.Now(), nil)
	requireStatus(t, rec, http.StatusOK)
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != clock.Now().UTC().Format(time.RFC3339Nano) {
		t.Fatalf("sequence advance failed: %s", got)
	}

	// Lower sequence in the active session is stale input, even with a newer
	// generation timestamp: sequence is the in-session authority.
	clock.Advance(time.Second)
	rec = fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), nil)
	requireStatus(t, rec, http.StatusConflict)
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != m53Base.Add(time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("lower sequence mutated state: %s", got)
	}

	// A new session with non-regressing generatedAt becomes active.
	clock.Advance(time.Second)
	rec = fn.postSnapshot("mac-a", sessionBeta, 1, clock.Now(), nil)
	requireStatus(t, rec, http.StatusOK)

	// A delayed packet from the previous session with older generatedAt must
	// not rewind the newer accepted state.
	clock.Advance(time.Second)
	rec = fn.postSnapshot("mac-a", sessionAlpha, 5, clock.Now().Add(-2*time.Second), nil)
	requireStatus(t, rec, http.StatusConflict)
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != m53Base.Add(3*time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("delayed prior session regressed state: %s", got)
	}

	// A new session with regressing generatedAt is rejected.
	rec = fn.postSnapshot("mac-a", sessionGamma, 1, clock.Now().Add(-2*time.Second), nil)
	requireStatus(t, rec, http.StatusConflict)
}

// M5.2 §14.3: an exact duplicate refreshes liveness only while the retried
// snapshot is still inside its admission window.
func TestM53FakeNodeDuplicateLivenessRefreshWindow(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil), http.StatusOK)

	clock.Advance(20 * time.Second)
	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil), http.StatusOK)
	wrapper := hostWrapper(t, fn.dashboardJSON(), "mac-a")
	if wrapper["source"].(map[string]any)["status"] != "online" {
		t.Fatalf("recent duplicate did not refresh liveness: %v", wrapper["source"])
	}

	clock.Advance(40 * time.Second)
	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, m53Base, nil), http.StatusOK)
	wrapper = hostWrapper(t, fn.dashboardJSON(), "mac-a")
	source := wrapper["source"].(map[string]any)
	if source["status"] != "offline" {
		t.Fatalf("ancient duplicate refreshed liveness: %v", source)
	}
	lastSuccess := source["lastSuccessAt"].(string)
	if lastSuccess != m53Base.Add(20*time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("unexpected lastSuccessAt=%s", lastSuccess)
	}
}

// M5.2 §36 / §39 acceptance 19: every rejection class for mac-a leaves the
// accepted mac-b snapshot untouched.
func TestM53FakeNodeTwoNodeIsolation(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	requireStatus(t, fn.postSnapshot("mac-b", sessionBeta, 1, m53Base, nil), http.StatusOK)
	before := hostWrapper(t, fn.dashboardJSON(), "mac-b")

	failures := []struct {
		name string
		run  func()
	}{
		{"bad token", func() {
			fn.post(marshalEnvelope(t, snapshotEnvelope("mac-a", sessionAlpha, 1, clock.Now())), "token-wrong-aaaaaaaaaaaaaaaaaaaaaaaa")
		}},
		{"identity mismatch", func() {
			fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), func(env map[string]any) { env["nodeId"] = "mac-b" })
		}},
		{"malformed json", func() { fn.post([]byte("{"), testTokenA) }},
		{"oversize", func() {
			env := snapshotEnvelope("mac-a", sessionAlpha, 1, clock.Now())
			env["state"].(map[string]any)["host"].(map[string]any)["displayName"] = repeatChar('x', MaxBodyBytes)
			fn.post(marshalEnvelope(t, env), testTokenA)
		}},
		{"stale timestamp", func() { fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now().Add(-time.Minute), nil) }},
		{"future timestamp", func() { fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now().Add(3*time.Minute), nil) }},
		{"lower sequence", func() {
			requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 7, clock.Now(), nil), http.StatusOK)
			requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 3, clock.Now(), nil), http.StatusConflict)
		}},
		{"duplicate conflict", func() {
			requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 8, clock.Now(), nil), http.StatusOK)
			requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 8, clock.Now(), func(env map[string]any) {
				env["state"].(map[string]any)["host"].(map[string]any)["displayName"] = "Clash"
			}), http.StatusConflict)
		}},
	}
	for _, failure := range failures {
		failure.run()
		after := hostWrapper(t, fn.dashboardJSON(), "mac-b")
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if string(beforeJSON) != string(afterJSON) {
			t.Fatalf("%s mutated mac-b: before=%s after=%s", failure.name, beforeJSON, afterJSON)
		}
	}

	// mac-a failures never blocked mac-b's continued progress.
	requireStatus(t, fn.postSnapshot("mac-b", sessionBeta, 2, clock.Now(), nil), http.StatusOK)
}

func repeatChar(c byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = c
	}
	return string(out)
}

// Audit pass 2 coverage: concurrent duplicate, out-of-order and invalid
// traffic must stay race-free and never corrupt accepted state.
func TestM53FakeNodeConcurrentMixedTraffic(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			session := fmt.Sprintf("%032x", worker+1)
			for sequence := 1; sequence <= 10; sequence++ {
				env := marshalEnvelope(nil, snapshotEnvelope("mac-a", session, sequence, m53Base))
				fn.post(env, testTokenA)
				fn.post(env, testTokenA) // exact retry
				if sequence%3 == 0 {
					fn.post([]byte("{bad"), testTokenA)
				}
				if sequence%4 == 0 {
					fn.post(marshalEnvelope(nil, snapshotEnvelope("mac-b", session, sequence, m53Base)), testTokenB)
				}
			}
		}(worker)
	}
	wg.Wait()

	// All storm traffic used the same generation instant, so the final
	// authoritative snapshot is accepted for both nodes regardless of which
	// storm session won.
	clock.Advance(time.Second)
	requireStatus(t, fn.postSnapshot("mac-a", sessionAlpha, 1, clock.Now(), nil), http.StatusOK)
	requireStatus(t, fn.postSnapshot("mac-b", sessionBeta, 1, clock.Now(), nil), http.StatusOK)

	for _, id := range []string{"mac-a", "mac-b"} {
		wrapper := hostWrapper(t, fn.dashboardJSON(), id)
		if wrapper["state"] == nil {
			t.Fatalf("%s missing final state", id)
		}
		if wrapper["source"].(map[string]any)["status"] != "online" {
			t.Fatalf("%s not online after final snapshot: %v", id, wrapper["source"])
		}
	}
}

// Audit pass 2 coverage: many goroutines posting the identical new tuple
// concurrently must all observe success (one accept, idempotent duplicates).
func TestM53FakeNodeConcurrentIdenticalDuplicates(t *testing.T) {
	clock := newFakeClock(m53Base)
	fn := newFakeNode(t, defaultRegistryEntries(), clock)

	body := marshalEnvelope(nil, snapshotEnvelope("mac-a", sessionAlpha, 1, m53Base))
	const rounds = 16
	results := make(chan int, rounds)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- fn.post(body, testTokenA).Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		if code != http.StatusOK {
			t.Fatalf("concurrent identical duplicate status=%d", code)
		}
	}
	if got := stateGeneratedAt(t, fn.dashboardJSON(), "mac-a"); got != m53Base.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("concurrent duplicates corrupted state: %s", got)
	}
}
