package hub

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/diagnostics"
)

// SnapshotRoute is the single frozen M5.2 machine write route.
const SnapshotRoute = "/api/node/v1/snapshot"

// Receiver implements the hub side of the node push ingestion contract.
type Receiver struct {
	registry    *Registry
	store       *NodeStateStore
	now         func() time.Time
	logger      *slog.Logger
	diagnostics diagnostics.Recorder
}

func NewReceiver(registry *Registry, store *NodeStateStore, logger *slog.Logger, now func() time.Time) *Receiver {
	return NewReceiverWithDiagnostics(registry, store, logger, now, nil)
}

// NewReceiverWithDiagnostics wires the push receiver to the same narrow
// observer used by the Operator Console. The receiver never emits request
// data; the implementation is responsible for cataloguing and redaction.
func NewReceiverWithDiagnostics(registry *Registry, store *NodeStateStore, logger *slog.Logger, now func() time.Time, observer diagnostics.Recorder) *Receiver {
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Receiver{registry: registry, store: store, now: now, logger: logger, diagnostics: observer}
}

func (rcv *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rej := rcv.ingest(w, r)
	if rej == nil {
		return
	}
	if rcv.diagnostics != nil {
		rcv.diagnostics.Record("warn", "hub", "snapshot_rejected")
	}
	rcv.reject(w, rej.status, rej.class)
}

// ingest runs the frozen validation order:
//
//	method/path -> bounded body/content checks -> bearer authentication ->
//	envelope JSON/schema validation -> registry enabled/node binding ->
//	nested PublicState validation -> timestamp validation ->
//	session/sequence/order validation -> atomic store update -> bounded ack.
//
// No accepted node state is mutated before every check succeeds.
func (rcv *Receiver) ingest(w http.ResponseWriter, r *http.Request) *rejection {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		return &rejection{http.StatusMethodNotAllowed, "method"}
	}
	if r.URL.RawQuery != "" {
		return &rejection{http.StatusBadRequest, "query_string"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &rejection{http.StatusUnsupportedMediaType, "content_type"}
	}
	if r.ContentLength > MaxBodyBytes {
		return &rejection{http.StatusRequestEntityTooLarge, "body_size"}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return &rejection{http.StatusBadRequest, "body_read"}
	}
	if len(body) > MaxBodyBytes {
		return &rejection{http.StatusRequestEntityTooLarge, "body_size"}
	}

	node := rcv.authenticate(r)
	if node == nil {
		rcv.logger.Info("node snapshot rejected", "status", http.StatusUnauthorized, "reason", "credentials")
		return &rejection{http.StatusUnauthorized, "credentials"}
	}

	snap, err := decodeNodeSnapshot(body)
	if err != nil {
		rcv.logReject(node, http.StatusBadRequest, "envelope_json")
		return &rejection{http.StatusBadRequest, "envelope_json"}
	}
	if rej := validateEnvelope(&snap); rej != nil {
		rcv.logReject(node, rej.status, rej.class)
		return rej
	}
	if !node.Enabled {
		rcv.logReject(node, http.StatusForbidden, "node_disabled")
		return &rejection{http.StatusForbidden, "node_disabled"}
	}
	if snap.NodeID != node.ID {
		rcv.logReject(node, http.StatusForbidden, "identity_binding")
		return &rejection{http.StatusForbidden, "identity_binding"}
	}
	now := rcv.now().UTC()
	if rej := validateNestedState(&snap, now); rej != nil {
		rcv.logReject(node, rej.status, rej.class)
		return rej
	}

	outcome, rej := rcv.store.Apply(node, snap, payloadDigest(body), now)
	if rej != nil {
		rcv.logReject(node, rej.status, rej.class)
		return rej
	}
	if outcome.Accepted {
		rcv.logger.Debug("node snapshot accepted", "node", node.ID, "session", snap.SessionID, "sequence", snap.Sequence)
		if rcv.diagnostics != nil {
			rcv.diagnostics.Record("info", "hub", "snapshot_accepted")
		}
	} else if outcome.Duplicate {
		rcv.logger.Debug("node snapshot duplicate accepted", "node", node.ID, "session", snap.SessionID, "sequence", snap.Sequence)
	}
	rcv.writeAccepted(w)
	return nil
}

// authenticate resolves the bearer credential to a registered node without
// depending on the peer address: node identity is never taken from the
// network layer.
func (rcv *Receiver) authenticate(r *http.Request) *Node {
	header := r.Header.Get("Authorization")
	const scheme = "bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return nil
	}
	return rcv.registry.Authenticate(header[len(scheme):])
}

func (rcv *Receiver) writeAccepted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		OK bool `json:"ok"`
	}{OK: true})
}

func (rcv *Receiver) reject(w http.ResponseWriter, status int, class string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, rejectionText(status), status)
}

// logReject records a bounded generic rejection class. Logs never carry the
// bearer credential, the Authorization header or the request body.
func (rcv *Receiver) logReject(node *Node, status int, class string) {
	rcv.logger.Info("node snapshot rejected", "node", node.ID, "status", status, "reason", class)
}

// Runtime bundles the hub receiver authority: push-native node state store
// and the machine write route. It owns no collectors, no local monitored
// state and no background pollers; statuses are derived at read time from the
// hub clock.
type Runtime struct {
	store    *NodeStateStore
	receiver *Receiver
}

func NewRuntime(entries []NodeConfig, logger *slog.Logger, now func() time.Time) (*Runtime, error) {
	return NewRuntimeWithDiagnostics(entries, logger, now, nil)
}

// NewRuntimeWithDiagnostics creates the push runtime and records its startup
// through the shared diagnostics observer. Keeping the old constructor above
// preserves the package's test and compatibility seam.
func NewRuntimeWithDiagnostics(entries []NodeConfig, logger *slog.Logger, now func() time.Time, observer diagnostics.Recorder) (*Runtime, error) {
	registry, err := NewRegistry(entries)
	if err != nil {
		return nil, err
	}
	store := NewNodeStateStore(registry)
	if observer != nil {
		observer.Record("info", "hub", "runtime_started")
	}
	return &Runtime{store: store, receiver: NewReceiverWithDiagnostics(registry, store, logger, now, observer)}, nil
}

func (rt *Runtime) Store() *NodeStateStore { return rt.store }

func (rt *Runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.receiver.ServeHTTP(w, r)
}
