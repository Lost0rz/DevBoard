package agentquota

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const workerFileSchemaVersion = 1

// StatusFile is the private, credential-free handoff from the independent
// activator to the Hub. Keeping it beside config.yaml makes the Hub UI useful
// even while the activator is updated or stopped.
func StatusFile(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "agent-quota-status.json")
}

// ControlFile holds at most one operator-requested manual test. It contains
// no provider configuration or credentials; the activator reads those from
// the private files it already owns.
func ControlFile(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "agent-quota-control.json")
}

type WorkerStatus struct {
	SchemaVersion int       `json:"schemaVersion"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Health        Health    `json:"health"`
	FiredAnchors  []string  `json:"firedAnchors,omitempty"`
}

// ReadWorkerStatus returns an unavailable error when the activator has not
// written its first heartbeat yet. The caller must treat that as a worker
// status, never as permission to run an activation in the Hub process.
func ReadWorkerStatus(path string) (WorkerStatus, error) {
	var status WorkerStatus
	if err := readPrivateJSON(path, &status); err != nil {
		return WorkerStatus{}, err
	}
	if status.SchemaVersion != workerFileSchemaVersion || status.UpdatedAt.IsZero() {
		return WorkerStatus{}, errors.New("agent quota worker status is invalid")
	}
	return status, nil
}

func WriteWorkerStatus(path string, health Health) error {
	return WriteWorkerStatusWithFired(path, health, nil)
}

func WriteWorkerStatusWithFired(path string, health Health, fired []string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("agent quota worker status path is required")
	}
	clean := make([]string, 0, len(fired))
	for _, key := range fired {
		if len(key) > 0 && len(key) <= 96 {
			clean = append(clean, key)
		}
	}
	if len(clean) > 32 {
		clean = clean[len(clean)-32:]
	}
	return writePrivateJSON(path, WorkerStatus{SchemaVersion: workerFileSchemaVersion, UpdatedAt: time.Now().UTC(), Health: health, FiredAnchors: clean})
}

type ManualRequest struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	RequestedAt   time.Time `json:"requestedAt"`
}

// QueueManualRequest queues one manual activation for the independent
// activator. A busy request is deliberately rejected instead of overwriting
// it, so a browser double-click can never create an unaccounted request.
func QueueManualRequest(path string) (ManualRequest, error) {
	if strings.TrimSpace(path) == "" {
		return ManualRequest{}, errors.New("agent quota control path is required")
	}
	if _, err := os.Lstat(path); err == nil {
		return ManualRequest{}, errors.New("agent quota manual test is already queued")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ManualRequest{}, errors.New("agent quota control file is unavailable")
	}
	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return ManualRequest{}, errors.New("generate agent quota manual request")
	}
	request := ManualRequest{SchemaVersion: workerFileSchemaVersion, ID: hex.EncodeToString(rawID), RequestedAt: time.Now().UTC()}
	if err := writePrivateJSON(path, request); err != nil {
		return ManualRequest{}, err
	}
	return request, nil
}

// ClaimManualRequest atomically removes the request from the Hub-visible
// queue. A temporary rename means a newly queued request can safely arrive
// while the worker is processing the prior one.
func ClaimManualRequest(path string) (ManualRequest, bool, error) {
	if strings.TrimSpace(path) == "" {
		return ManualRequest{}, false, errors.New("agent quota control path is required")
	}
	claimed := path + ".processing"
	if err := os.Rename(path, claimed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ManualRequest{}, false, nil
		}
		return ManualRequest{}, false, errors.New("agent quota control file is unavailable")
	}
	defer os.Remove(claimed)
	var request ManualRequest
	if err := readPrivateJSON(claimed, &request); err != nil {
		return ManualRequest{}, false, err
	}
	if request.SchemaVersion != workerFileSchemaVersion || len(request.ID) != 32 || request.RequestedAt.IsZero() {
		return ManualRequest{}, false, errors.New("agent quota manual request is invalid")
	}
	if _, err := hex.DecodeString(request.ID); err != nil {
		return ManualRequest{}, false, errors.New("agent quota manual request is invalid")
	}
	return request, true, nil
}

func readPrivateJSON(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 64<<10 {
		return errors.New("agent quota worker file is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("agent quota worker file is unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) == nil {
		return errors.New("agent quota worker file is invalid")
	}
	return nil
}

func writePrivateJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := requirePrivateDirectory(dir); err != nil {
		return errors.New("agent quota worker directory is unavailable")
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return errors.New("agent quota worker file is unavailable")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("agent quota worker file is unavailable")
	}
	tmp, err := os.CreateTemp(dir, ".agent-quota-worker-*")
	if err != nil {
		return errors.New("create agent quota worker file")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return errors.New("protect agent quota worker file")
	}
	if err := json.NewEncoder(tmp).Encode(value); err != nil {
		tmp.Close()
		return errors.New("write agent quota worker file")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errors.New("write agent quota worker file")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("write agent quota worker file")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit agent quota worker file: %w", err)
	}
	return nil
}
