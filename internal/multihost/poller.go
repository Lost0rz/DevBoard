package multihost

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	PollInterval   = 1 * time.Second
	RequestTimeout = 1500 * time.Millisecond
	MaxBodyBytes   = 256 * 1024
)

type Runtime struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newPeerHTTPClient() *http.Client {
	return &http.Client{
		Timeout: RequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func Start(peers []config.PeerConfig, store *PeerSnapshotStore, localHostID string, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{cancel: cancel}
	client := newPeerHTTPClient()
	for _, peer := range peers {
		peer := peer
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			pollLoop(ctx, client, store, peer, localHostID, logger, PollInterval, time.Now)
		}()
	}
	return r
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

func pollLoop(ctx context.Context, client *http.Client, store *PeerSnapshotStore, peer config.PeerConfig, localHostID string, logger *slog.Logger, interval time.Duration, now func() time.Time) {
	for {
		pollPeer(ctx, client, store, peer, localHostID, logger, now)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func pollPeer(ctx context.Context, client *http.Client, store *PeerSnapshotStore, peer config.PeerConfig, localHostID string, logger *slog.Logger, now func() time.Time) {
	attemptAt := now().UTC()
	addrPort, err := config.ParsePeerEndpoint(peer.Endpoint)
	if err != nil {
		_ = store.MarkFailure(peer.ExpectedHostID, attemptAt, PeerDegraded, "Peer response invalid.")
		return
	}
	url := "http://" + addrPort.String() + "/api/state"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		_ = store.MarkFailure(peer.ExpectedHostID, attemptAt, PeerDegraded, "Peer response invalid.")
		return
	}
	resp, err := client.Do(req)
	completedAt := now().UTC()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerUnavailable, "Peer unavailable.")
		logger.Debug("peer poll unavailable", "peer", peer.ExpectedHostID)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerUnavailable, "Peer unavailable.")
		return
	}
	if resp.ContentLength > MaxBodyBytes {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerDegraded, "Peer response invalid.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerUnavailable, "Peer unavailable.")
		return
	}
	if len(body) > MaxBodyBytes {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerDegraded, "Peer response invalid.")
		return
	}
	var pub state.PublicState
	if err := json.Unmarshal(body, &pub); err != nil {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, PeerDegraded, "Peer response invalid.")
		return
	}
	status, message, accept := validatePeerState(pub, peer.ExpectedHostID, localHostID, store, completedAt)
	if !accept {
		_ = store.MarkFailure(peer.ExpectedHostID, completedAt, status, message)
		return
	}
	_ = store.MarkSuccess(peer.ExpectedHostID, pub, completedAt, status, message)
}

func validatePeerState(pub state.PublicState, expectedHostID, localHostID string, store *PeerSnapshotStore, now time.Time) (PeerStatus, string, bool) {
	if pub.StateKind != "public" || pub.SchemaVersion != 1 || !validObservedHostID(pub.Host.ID) {
		return PeerDegraded, "Peer response invalid.", false
	}
	if pub.Host.ID != expectedHostID {
		if pub.Host.ID == localHostID || store.HasAcceptedHostID(expectedHostID, pub.Host.ID) {
			return PeerDegraded, "Peer host identity conflict.", false
		}
		return PeerDegraded, "Peer host identity mismatch.", false
	}
	if pub.Host.ID == localHostID || store.HasAcceptedHostID(expectedHostID, pub.Host.ID) {
		return PeerDegraded, "Peer host identity conflict.", false
	}
	if duplicateTaskIDs(pub.Tasks) || duplicateAgentIDs(pub.Agents) {
		return PeerDegraded, "Peer response invalid.", false
	}
	if pub.GeneratedAt.IsZero() || pub.GeneratedAt.After(now.Add(FutureTolerance)) {
		return PeerDegraded, "Peer clock is outside tolerance.", false
	}
	age := now.Sub(pub.GeneratedAt)
	if age < 0 {
		age = 0
	}
	if age > RetentionWindow {
		return PeerDegraded, "Peer snapshot is stale.", false
	}
	if age > RemoteFreshWindow {
		return PeerDegraded, "Peer snapshot is stale.", true
	}
	return PeerAvailable, "Peer snapshot available.", true
}

func validObservedHostID(id string) bool {
	if len(id) < 1 || len(id) > 64 || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func duplicateTaskIDs(tasks []state.PublicTask) bool {
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			return true
		}
		if _, ok := seen[task.ID]; ok {
			return true
		}
		seen[task.ID] = struct{}{}
	}
	return false
}

func duplicateAgentIDs(agents []state.PublicAgent) bool {
	seen := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		if agent.ID == "" {
			return true
		}
		if _, ok := seen[agent.ID]; ok {
			return true
		}
		seen[agent.ID] = struct{}{}
	}
	return false
}
