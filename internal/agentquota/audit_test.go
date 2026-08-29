package agentquota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileAuditLogPersistsRedactedRecords(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 7, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "agent-quota-audit.jsonl")
	log, err := NewFileAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	log.now = func() time.Time { return now }
	slot := now.Add(-2 * time.Hour)
	reset := now.Add(3 * time.Hour)
	if err := log.Record(Event{At: now, ScheduledAt: slot, Code: "activation_failed", Reason: "provider_error", Trigger: "scheduled", Attempt: 2, HTTPStatus: 429, ProviderCode: "1316", ResetAt: reset, ResetText: "raw provider message must not persist"}); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := reopened.List(AuditQuery{Limit: 10})
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.EventCode != "activation_failed" || record.HTTPStatus != 429 || record.ProviderCode != "1316" || record.ScheduledAt == nil || record.ResetAt == nil {
		t.Fatalf("record=%+v", record)
	}
	body, err := json.Marshal(record)
	if err != nil || strings.Contains(string(body), "raw provider") {
		t.Fatalf("audit leaked raw provider data: %s err=%v", body, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode=%v err=%v", info.Mode(), err)
	}
}

func TestFileAuditLogRetainsOnlyRecentRecords(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 29, 7, 0, 0, 0, time.UTC)
	log, err := NewFileAuditLog(filepath.Join(dir, "agent-quota-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	log.now = func() time.Time { return now }
	log.retention = 3 * time.Hour
	if err := log.Record(Event{At: now.Add(-2 * time.Hour), Code: "activation_due", Reason: "scheduled_time", Trigger: "scheduled"}); err != nil {
		t.Fatal(err)
	}
	log.retention = time.Hour
	if records, err := log.List(AuditQuery{Limit: 10}); err != nil || len(records) != 0 {
		t.Fatalf("expired records=%+v err=%v", records, err)
	}
	if err := log.Record(Event{At: now, Code: "activation_succeeded", Reason: "response_verified", Trigger: "scheduled"}); err != nil {
		t.Fatal(err)
	}
	records, err := log.List(AuditQuery{Limit: 10})
	if err != nil || len(records) != 1 || records[0].EventCode != "activation_succeeded" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}

func TestEnsureAuditTokenCreatesPrivateIndependentCredential(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-quota-audit.token")
	first, err := EnsureAuditToken(path)
	if err != nil || len(first) != 64 {
		t.Fatalf("token=%q err=%v", first, err)
	}
	second, err := EnsureAuditToken(path)
	if err != nil || string(second) != string(first) {
		t.Fatalf("token changed: first=%q second=%q err=%v", first, second, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode=%v err=%v", info.Mode(), err)
	}
}
