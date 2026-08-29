package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Lost0rz/DevBoard/internal/navigation"
	"github.com/Lost0rz/DevBoard/internal/state"
)

const (
	ActionsRoute     = "/api/node/v1/actions"
	ActionsAckRoute  = "/api/node/v1/actions/ack"
	maxQueuedActions = 32
	maxActionBody    = 8 * 1024
)

type navigationActionQueue struct {
	mu      sync.Mutex
	byNode  map[string][]navigation.Action
	results map[string]navigation.Result
}

func newNavigationActionQueue() *navigationActionQueue {
	return &navigationActionQueue{byNode: make(map[string][]navigation.Action), results: make(map[string]navigation.Result)}
}

func (s *NodeStateStore) EnqueueNavigation(hostID, targetID string, action state.NavigationAction, now time.Time) (navigation.Action, error) {
	return s.EnqueueNavigationForTask(hostID, targetID, "", action, now)
}

// EnqueueNavigationForTask is the task-aware path used by display cards. The
// task ID is optional for compatibility with older display clients.
func (s *NodeStateStore) EnqueueNavigationForTask(hostID, targetID, taskID string, action state.NavigationAction, now time.Time) (navigation.Action, error) {
	s.mu.RLock()
	node, ok := s.registry.Lookup(hostID)
	rec := s.records[hostID]
	var pub *state.PublicState
	if rec != nil && rec.state != nil {
		copyState, err := clonePublicState(*rec.state)
		if err == nil {
			pub = &copyState
		}
	}
	s.mu.RUnlock()
	if !ok || node == nil || !node.Enabled {
		return navigation.Action{}, fmt.Errorf("target host is unavailable")
	}
	if pub == nil {
		return navigation.Action{}, fmt.Errorf("target host has no current state")
	}
	if !pub.Meta.SafeNavigationEnabled {
		return navigation.Action{}, fmt.Errorf("safe navigation is not enabled for target host")
	}
	var target *state.PublicNavigationTarget
	for i := range pub.NavigationTargets {
		if pub.NavigationTargets[i].TargetID == targetID {
			target = &pub.NavigationTargets[i]
			break
		}
	}
	if target == nil || !containsNavigationAction(target.AllowedActions, action) {
		return navigation.Action{}, fmt.Errorf("navigation target or action is unavailable")
	}
	if taskID != "" {
		validTask := false
		for _, task := range pub.Tasks {
			if task.ID == taskID && task.Navigation != nil && task.Navigation.TargetID == targetID {
				validTask = true
				break
			}
		}
		if !validTask {
			return navigation.Action{}, fmt.Errorf("navigation task is unavailable")
		}
	}
	id := newActionID()
	item := navigation.Action{ID: id, TargetID: targetID, TaskID: taskID, Action: action, IssuedAt: now.UTC()}
	s.actions.mu.Lock()
	defer s.actions.mu.Unlock()
	queue := s.actions.byNode[hostID]
	if len(queue) >= maxQueuedActions {
		return navigation.Action{}, fmt.Errorf("navigation queue is full")
	}
	s.actions.byNode[hostID] = append(queue, item)
	return item, nil
}

func (s *NodeStateStore) PendingNavigation(nodeID string) []navigation.Action {
	s.actions.mu.Lock()
	defer s.actions.mu.Unlock()
	queue := s.actions.byNode[nodeID]
	out := make([]navigation.Action, len(queue))
	copy(out, queue)
	return out
}

func (s *NodeStateStore) AckNavigation(nodeID, actionID string, result navigation.Result) bool {
	_, ok := s.AckNavigationItem(nodeID, actionID, result)
	return ok
}

// AckNavigationItem returns the consumed action so the receiver can persist a
// task acknowledgement after a successful host-side navigation.
func (s *NodeStateStore) AckNavigationItem(nodeID, actionID string, result navigation.Result) (navigation.Action, bool) {
	s.actions.mu.Lock()
	defer s.actions.mu.Unlock()
	queue := s.actions.byNode[nodeID]
	for i, item := range queue {
		if item.ID != actionID {
			continue
		}
		s.actions.byNode[nodeID] = append(queue[:i], queue[i+1:]...)
		s.actions.results[actionID] = result
		return item, true
	}
	return navigation.Action{}, false
}

func (s *NodeStateStore) NavigationResult(actionID string) (navigation.Result, bool) {
	s.actions.mu.Lock()
	defer s.actions.mu.Unlock()
	result, ok := s.actions.results[actionID]
	return result, ok
}

func (rt *Runtime) serveActions(w http.ResponseWriter, r *http.Request) {
	node := rt.receiver.authenticate(r)
	if node == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !node.Enabled {
		http.Error(w, "node disabled", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == ActionsRoute && r.Method == http.MethodGet:
		if r.URL.RawQuery != "" {
			http.Error(w, "query string not allowed", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Actions []navigation.Action `json:"actions"`
		}{Actions: rt.store.PendingNavigation(node.ID)})
	case r.URL.Path == ActionsAckRoute && r.Method == http.MethodPost:
		var ack struct {
			ID      string `json:"id"`
			OK      bool   `json:"ok"`
			Message string `json:"message,omitempty"`
		}
		if err := decodeActionJSON(r, &ack); err != nil || strings.TrimSpace(ack.ID) == "" {
			http.Error(w, "invalid action acknowledgement", http.StatusBadRequest)
			return
		}
		item, ok := rt.store.AckNavigationItem(node.ID, ack.ID, navigation.Result{OK: ack.OK, Message: ack.Message})
		if !ok {
			http.Error(w, "action not found", http.StatusNotFound)
			return
		}
		if ack.OK && item.TaskID != "" {
			rt.store.MarkTaskRead(node.ID, item.TargetID, item.TaskID, rt.receiver.now())
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func decodeActionJSON(r *http.Request, dst any) error {
	if r.ContentLength > maxActionBody {
		return fmt.Errorf("action body too large")
	}
	media := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	if media != "application/json" {
		return fmt.Errorf("action content type invalid")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxActionBody+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing action data")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func containsNavigationAction(actions []state.NavigationAction, want state.NavigationAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func newActionID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "nav-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("nav-%d", time.Now().UnixNano())
}
