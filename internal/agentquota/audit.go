package agentquota

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	auditSchemaVersion = 1
	auditRetention     = 7 * 24 * time.Hour
	auditCapacity      = 10_000
	auditMaxBytes      = 8 << 20
)

var (
	safeAuditCode    = regexp.MustCompile(`^activation_(due|attempt|succeeded|failed|retry_scheduled|skipped|deferred)$`)
	safeAuditReason  = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	safeProviderCode = regexp.MustCompile(`^[0-9]{1,16}$`)
)

// AuditRecord is the credential-free, durable projection of one scheduled or
// manual activation event. It deliberately excludes request bodies, endpoint
// URLs, API keys, and raw provider messages.
type AuditRecord struct {
	SchemaVersion int        `json:"schemaVersion"`
	RecordedAt    time.Time  `json:"recordedAt"`
	ScheduledAt   *time.Time `json:"scheduledAt,omitempty"`
	EventCode     string     `json:"eventCode"`
	Reason        string     `json:"reason"`
	Trigger       string     `json:"trigger"`
	Attempt       int        `json:"attempt,omitempty"`
	HTTPStatus    int        `json:"httpStatus,omitempty"`
	ProviderCode  string     `json:"providerCode,omitempty"`
	ResetAt       *time.Time `json:"resetAt,omitempty"`
}

// AuditQuery bounds the read-only audit API. Times are inclusive and always
// interpreted as RFC3339 instants by the HTTP layer.
type AuditQuery struct {
	Since *time.Time
	Until *time.Time
	Limit int
}

// AuditLog is shared by the scheduler, the Admin page and the read-only API.
type AuditLog interface {
	Record(Event) error
	List(AuditQuery) ([]AuditRecord, error)
}

// FileAuditLog stores a small, atomic JSONL audit trail beside the private Hub
// configuration. Writes are intentionally infrequent (only scheduler events),
// so rewriting the bounded file is simpler and safer than a long-lived append
// handle across container restarts.
type FileAuditLog struct {
	path      string
	retention time.Duration
	capacity  int
	now       func() time.Time
	mu        sync.Mutex
}

func AuditLogFile(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "agent-quota-audit.jsonl")
}

func AuditTokenFile(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "agent-quota-audit.token")
}

func NewFileAuditLog(path string) (*FileAuditLog, error) {
	if err := requirePrivateDirectory(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("agent quota audit directory: %w", err)
	}
	log := &FileAuditLog{path: path, retention: auditRetention, capacity: auditCapacity, now: time.Now}
	if _, err := log.readLocked(); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *FileAuditLog) Record(event Event) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records, err := l.readLocked()
	if err != nil {
		return err
	}
	recordedAt := event.At.UTC()
	if recordedAt.IsZero() {
		recordedAt = l.now().UTC()
	}
	records = append(records, auditRecordFromEvent(event, recordedAt))
	cutoff := l.now().UTC().Add(-l.retention)
	filtered := records[:0]
	for _, record := range records {
		if !record.RecordedAt.Before(cutoff) {
			filtered = append(filtered, record)
		}
	}
	if len(filtered) > l.capacity {
		filtered = append([]AuditRecord(nil), filtered[len(filtered)-l.capacity:]...)
	}
	return l.writeLocked(filtered)
}

func (l *FileAuditLog) List(query AuditQuery) ([]AuditRecord, error) {
	if l == nil {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records, err := l.readLocked()
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	result := make([]AuditRecord, 0, minInt(limit, len(records)))
	retentionCutoff := l.now().UTC().Add(-l.retention)
	for i := len(records) - 1; i >= 0 && len(result) < limit; i-- {
		record := records[i]
		if record.RecordedAt.Before(retentionCutoff) {
			continue
		}
		if query.Since != nil && record.RecordedAt.Before(query.Since.UTC()) {
			continue
		}
		if query.Until != nil && record.RecordedAt.After(query.Until.UTC()) {
			continue
		}
		result = append(result, record)
	}
	return result, nil
}

func auditRecordFromEvent(event Event, recordedAt time.Time) AuditRecord {
	record := AuditRecord{SchemaVersion: auditSchemaVersion, RecordedAt: recordedAt, EventCode: safeEventCode(event.Code), Reason: safeReason(event.Reason), Trigger: safeTrigger(event.Trigger), Attempt: event.Attempt}
	if !event.ScheduledAt.IsZero() {
		at := event.ScheduledAt.UTC()
		record.ScheduledAt = &at
	}
	if event.HTTPStatus >= 100 && event.HTTPStatus <= 599 {
		record.HTTPStatus = event.HTTPStatus
	}
	if safeProviderCode.MatchString(event.ProviderCode) {
		record.ProviderCode = event.ProviderCode
	}
	if !event.ResetAt.IsZero() {
		at := event.ResetAt.UTC()
		record.ResetAt = &at
	}
	return record
}

func safeEventCode(value string) string {
	if safeAuditCode.MatchString(value) {
		return value
	}
	return "activation_failed"
}

func safeReason(value string) string {
	if safeAuditReason.MatchString(value) {
		return value
	}
	return "unspecified"
}

func safeTrigger(value string) string {
	if value == "scheduled" || value == "manual" {
		return value
	}
	return "scheduled"
}

func (l *FileAuditLog) readLocked() ([]AuditRecord, error) {
	info, err := os.Lstat(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > auditMaxBytes {
		return nil, errors.New("agent quota audit file is unavailable")
	}
	file, err := os.Open(l.path)
	if err != nil {
		return nil, errors.New("agent quota audit file is unavailable")
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, auditMaxBytes))
	scanner.Buffer(make([]byte, 1024), 64<<10)
	records := make([]AuditRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record AuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil || !validAuditRecord(record) {
			return nil, errors.New("agent quota audit file is invalid")
		}
		records = append(records, record)
		if len(records) > auditCapacity {
			return nil, errors.New("agent quota audit file is too large")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("agent quota audit file is unavailable")
	}
	return records, nil
}

func validAuditRecord(record AuditRecord) bool {
	return record.SchemaVersion == auditSchemaVersion && !record.RecordedAt.IsZero() && safeAuditCode.MatchString(record.EventCode) && safeAuditReason.MatchString(record.Reason) && (record.Trigger == "scheduled" || record.Trigger == "manual") && (record.HTTPStatus == 0 || record.HTTPStatus >= 100 && record.HTTPStatus <= 599) && (record.ProviderCode == "" || safeProviderCode.MatchString(record.ProviderCode))
}

func (l *FileAuditLog) writeLocked(records []AuditRecord) error {
	if err := requirePrivateDirectory(filepath.Dir(l.path)); err != nil {
		return errors.New("agent quota audit directory is unavailable")
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".agent-quota-audit-*")
	if err != nil {
		return errors.New("create agent quota audit")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return errors.New("protect agent quota audit")
	}
	encoder := json.NewEncoder(tmp)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			tmp.Close()
			return errors.New("write agent quota audit")
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errors.New("sync agent quota audit")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("close agent quota audit")
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		return errors.New("commit agent quota audit")
	}
	return nil
}

// EnsureAuditToken creates a separate read-only API credential. It is never
// displayed by the web UI and is intentionally distinct from the Node
// provisioning token and the GLM provider key.
func EnsureAuditToken(path string) ([]byte, error) {
	if err := requirePrivateDirectory(filepath.Dir(path)); err != nil {
		return nil, errors.New("agent quota audit token directory is unavailable")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("agent quota audit token is unavailable")
		}
		return readAuditToken(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("agent quota audit token is unavailable")
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, errors.New("generate agent quota audit token")
	}
	value := hex.EncodeToString(bytes)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-quota-audit-token-*")
	if err != nil {
		return nil, errors.New("create agent quota audit token")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, errors.New("protect agent quota audit token")
	}
	if _, err := tmp.WriteString(value + "\n"); err != nil {
		tmp.Close()
		return nil, errors.New("write agent quota audit token")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, errors.New("write agent quota audit token")
	}
	if err := tmp.Close(); err != nil {
		return nil, errors.New("write agent quota audit token")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, errors.New("commit agent quota audit token")
	}
	return []byte(value), nil
}

func readAuditToken(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("agent quota audit token is unavailable")
	}
	value := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("agent quota audit token is invalid")
	}
	return []byte(value), nil
}

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
