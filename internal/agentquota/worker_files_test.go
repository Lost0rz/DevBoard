package agentquota

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerStatusRoundTripIsPrivate(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-status.json")
	next := time.Now().UTC().Add(time.Hour).Round(time.Second)
	manual := Health{Enabled: true, Provider: "glm", State: "healthy", LastHTTPStatus: 200, LastUsageSummary: "total_tokens=9"}
	if err := WriteWorkerStatusSnapshot(path, Health{Enabled: true, Provider: "glm", State: "waiting", NextRunAt: &next}, []string{"2026-08-30T10:00:00+08:00|10:00"}, &manual); err != nil {
		t.Fatal(err)
	}
	status, err := ReadWorkerStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Health.Enabled || status.Health.Provider != "glm" || len(status.FiredAnchors) != 1 || status.ManualTest == nil || status.ManualTest.LastUsageSummary != "total_tokens=9" {
		t.Fatalf("status=%+v", status)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("status permissions: info=%v err=%v", info, err)
	}
}

func TestManualRequestIsSingleConsumerQueue(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-control.json")
	queued, err := QueueManualRequest(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := QueueManualRequest(path); err == nil {
		t.Fatal("second manual request overwrote pending request")
	}
	claimed, ok, err := ClaimManualRequest(path)
	if err != nil || !ok || claimed.ID != queued.ID {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err := ClaimManualRequest(path); err != nil || ok {
		t.Fatalf("empty queue claim ok=%v err=%v", ok, err)
	}
}

func TestWorkerFilesRejectSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-status.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := WriteWorkerStatus(path, Health{}); err == nil {
		t.Fatal("worker status followed a symlink")
	}
}

func TestWorkerFilesRejectTrailingContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-status.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"updatedAt":"2026-08-30T00:00:00Z","health":{}}garbage`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWorkerStatus(path); err == nil {
		t.Fatal("worker status accepted trailing invalid content")
	}
}

func TestManualRequestClaimsUseUniqueProcessingPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-control.json")
	stale := path + ".processing.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := QueueManualRequest(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := ClaimManualRequest(path); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("stale processing marker was overwritten or removed: %v", err)
	}
}
