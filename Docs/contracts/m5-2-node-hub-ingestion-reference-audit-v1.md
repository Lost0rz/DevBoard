# DevBoard M5.2 Node Agent → Hub Ingestion — Reference Audit V1

> Date: 2026-08-22
> Audited source: `codex/m5-1-always-on-hub`
> Purpose: define the exact implementation delta required to replace the validated M5.1 Hub-pull transport with the formal Node-push transport, without reopening already-valid M1–M5.1 state semantics.
> Status: **REFERENCE AUDIT COMPLETE**

## 0. Audit result

M5.2 is a transport-authority supersession, not a product rewrite.

The current branch already contains a valid separation between NODE and HUB runtime authority. The M5.2 implementation should preserve that separation and replace only the cross-machine transport and peer-enrollment semantics that depend on Hub polling a Node LAN endpoint.

Primary result:

```text
KEEP
  NODE local authority
  local Unix-socket agent ingestion
  state.Store
  PublicState projection/privacy
  System/Network collectors
  Task/Agent reducers
  DashboardState concept
  per-node last-good/failure isolation semantics
  HUB-only aggregation authority
  NAS stateless runtime

SUPERSEDE
  HUB → NODE GET polling
  private-IP peer endpoint enrollment
  PeerSnapshotStore naming/transport metadata
  poll-attempt driven availability semantics
  NODE LAN state exposure as production cross-machine path

ADD
  NODE outbound uplink
  HUB receiver
  node registry
  per-node bearer token binding
  NodeSnapshot envelope
  push acknowledgement semantics
  upload ordering/idempotency rules
  change-triggered wake-up + heartbeat
  ONLINE / STALE / OFFLINE connection classification
```

No material evidence supports rewriting the existing local task, system, network, projection or dashboard domain models as part of M5.2.

## 1. Existing runtime authority is already correct

Current `cmd/devboard/main.go` plans runtime behavior by `runtime.role`:

- NODE owns local monitored state;
- NODE starts local System and Network collectors;
- NODE starts Codex/Claude ingestion;
- HUB owns peer aggregation state;
- HUB does not create local monitored NAS state;
- HUB does not run agent ingestion or Mac collectors.

This matches the new business topology requirement:

```text
Mac = state producer
NAS = center service
```

M5.2 MUST preserve this authority split.

## 2. Existing local app boundary is already suitable

`internal/agent/runtime.go` already uses a Unix Domain Socket for local provider events.

Existing properties include:

- local filesystem runtime directory;
- restrictive directory/socket permissions;
- bounded IPC event payloads;
- explicit event validation;
- short local deadlines;
- stale-socket safety checks;
- refusal to replace ambiguous/non-socket paths.

Therefore M5.2 does not need a new LAN/local HTTP integration protocol.

Formal local direction remains:

```text
Codex / Claude / future local adapter
        ↓
Unix Domain Socket
        ↓
Node Agent reducer/state.Store
```

## 3. PublicState remains the privacy boundary

Current `state.ProjectPublic` creates a sanitized `PublicState` from internal state.

Current PublicState includes safe monitoring fields for:

- host identity;
- agents/tasks/alerts;
- system metrics;
- network metrics;
- project-safe presentation fields;
- quota/source health;
- safe navigation target identifiers;
- display metadata.

The projector does not forward the raw InternalState object as a transport payload.

M5.2 SHOULD use `PublicState` as the nested Node snapshot state and MUST NOT introduce a second parallel public domain state model unless a concrete incompatibility is demonstrated.

## 4. Current cross-machine transport is the superseded component

`internal/multihost/poller.go` currently implements:

```text
HUB
  ↓ every ~1 second
GET http://<private-node-ip>:<port>/api/state
  ↓
validate PublicState
  ↓
PeerSnapshotStore
```

Key current transport properties:

- fixed GET method;
- fixed `/api/state` path;
- one polling loop per peer;
- 1 second poll interval;
- 1500 ms request timeout;
- 256 KiB response-body bound;
- redirects disabled;
- explicit expected host ID validation;
- peer identity collision rejection.

These are valid controls for the historical Pull prototype, but the direction is incompatible with the formal Node Agent product because it requires the Hub to reach each Mac endpoint.

M5.2 supersedes the polling direction with:

```text
NODE
  ↓ authenticated outbound POST
HUB Receiver
```

## 5. Current peer endpoint configuration is obsolete for production

`internal/config/config.go` currently models HUB enrollment using:

```text
multi_host.peers = expected_host_id=ip:port
```

and restricts peers to private/CGNAT/ULA IP literals.

Those restrictions correctly protected the historical Hub poller from arbitrary SSRF targets, but the new product no longer needs Hub-originated requests to Node addresses.

M5.2 SHOULD replace production enrollment with a Hub-side Node Registry containing at least:

```text
node_id
safe display_name
enabled
token verifier/material
```

and a Node-side uplink configuration containing at least:

```text
node_id
display_name
hub_address
node_token
```

No Node LAN IP is an identity or required production enrollment field.

## 6. Existing store semantics are reusable; naming is not

`PeerSnapshotStore` already provides useful behavior:

- independent records per configured host;
- synchronized access;
- deep-copy state on read/write;
- atomic replacement of accepted last-good state;
- one peer failure does not erase another peer;
- failed input does not partially overwrite accepted state;
- retained last-good state;
- aggregate DashboardState construction.

M5.2 SHOULD preserve these semantics but introduce a push-native `NodeStateStore` (or equivalent) rather than extending `PeerSnapshotStore` indefinitely with concepts that no longer represent the product.

The implementation may share private helper functions during migration, but the production domain language after M5.2 should use `node`, `registry`, `receiver`, `last received`, and `connection status`, not `peer polling`.

## 7. Availability semantics must change from poll-attempt status to receive-age status

M5.1 statuses are driven by peer transport attempts:

```text
unknown
available
degraded
unavailable
```

M5.2 Push transport has no Hub polling attempt to classify.

The business design instead requires Hub-owned connection classification from the latest accepted receive time:

```text
ONLINE
STALE
OFFLINE
```

Recommended frozen thresholds from the approved business design:

```text
age <= 5s      ONLINE
5s < age <=30s STALE
age > 30s      OFFLINE
```

Snapshot freshness and connection status MUST remain conceptually distinct.

A node can be OFFLINE while a retained last-good snapshot remains visible and explicitly stale.

## 8. Current last-good retention remains useful

M5.1 retains an accepted remote snapshot for up to 30 minutes after last success.

The new business design also explicitly requires last-good behavior during Node interruption.

Therefore M5.2 SHOULD preserve a 30-minute last-good retention unless the technical contract identifies a concrete reason to change it.

Hub restart remains stateless for the Monitoring MVP:

```text
Hub restart
→ in-memory node state empty
→ Node heartbeat/reconnect
→ state repopulates
```

No database is required by the current scope.

## 9. State change notification is not present in the current store

Current `state.Store` exposes:

```text
Snapshot
Replace
Update
```

and protects state with an RWMutex, but it does not expose a revision counter or subscription/change channel.

Therefore "push immediately on every state change" cannot be implemented cleanly by simply attaching HTTP calls to the existing store without either:

1. coupling every producer to network transport; or
2. adding a small store-level change notification/revision mechanism.

M5.2 SHOULD freeze the second direction.

The Uplink must be the only network authority. Collectors and reducers should continue to write only to local state.

Recommended flow:

```text
producer
  ↓
state.Store.Update/Replace
  ↓
revision/change signal
  ↓
Node Uplink wake-up
```

The signal is a wake-up hint, not an unbounded event queue.

## 10. Push scheduling should coalesce state, not queue every mutation

System and Network collectors already operate periodically (currently 5-second sampling defaults), while Agent/task changes may happen in bursts.

The Node→Hub transport carries snapshots, not an event log.

Therefore the correct MVP scheduling model is:

```text
state change
→ wake uplink
→ generate newest PublicState
→ send newest snapshot
```

If multiple changes occur while one upload is in flight, they SHOULD coalesce into a subsequent newest snapshot rather than creating an unbounded FIFO of obsolete snapshots.

At most one snapshot request should be in flight per Node.

## 11. Heartbeat and state delivery can use the same snapshot endpoint

The business design requests:

```text
state change → immediate push
no change    → periodic heartbeat/snapshot
```

A separate heartbeat endpoint is not required for MVP if the same bounded `NodeSnapshot` can safely be resent.

Recommended default:

```text
heartbeat/snapshot interval = 1 second
```

This preserves the current user-visible latency target while removing Hub→Mac reachability requirements.

## 12. Authentication must bind credential to node identity

M5.1 expected-host validation protects against a peer returning the wrong Host ID, but it has no Node bearer credential because the transport is private-LAN polling.

M5.2 requires a per-node token.

The receiver must enforce the identity chain:

```text
Bearer token
  ↔ registered node_id
  == envelope.nodeId
  == state.host.id
```

A valid token for one node must never authorize replacement of another node's state.

Token values must not appear in:

- DashboardState;
- receiver error response bodies;
- application logs;
- normal settings readback;
- diagnostic payloads.

## 13. Receiver validation must be bounded before state mutation

The new receiver needs explicit ordering:

```text
request method/path/content-type checks
→ body size bound
→ authentication
→ JSON decode
→ envelope version/kind checks
→ node identity binding
→ nested PublicState validation
→ timestamp/order policy
→ atomic state replace
→ bounded acknowledgement
```

No rejected request may partially advance accepted node state or last-good timestamps.

The historical 256 KiB cross-machine bound is a reasonable inherited maximum for M5.2 unless the formal contract freezes a different bounded value.

## 14. Idempotency and ordering must be frozen before implementation

Push introduces replay/retry behavior that did not exist in the same form under polling.

The technical contract must define how Hub handles:

- duplicate snapshot delivery;
- retry after uncertain ACK;
- older snapshot arriving after newer snapshot;
- Node clock skew;
- Hub receive timestamps;
- Node restart.

Recommended MVP rule:

- Hub clock owns `receivedAt` and connection classification;
- Node provides an uplink session ID and monotonically increasing sequence within that session;
- exact duplicate `(nodeId, sessionId, sequence)` is idempotently accepted;
- lower sequence in the same session is rejected/ignored without replacing state;
- a new session may restart sequence from 1;
- nested PublicState `generatedAt` still receives bounded future/staleness validation.

This is safer than relying only on wall-clock timestamps for ordering.

## 15. HTTP response semantics should remain small and non-sensitive

Recommended success response:

```json
{"ok":true}
```

The receiver does not need to echo accepted state or token/registry metadata.

Recommended status classes:

```text
200 accepted / idempotent duplicate
400 malformed or schema-invalid
401 missing/invalid credential
403 credential/node identity mismatch
405 wrong method
413 body too large
409 stale/out-of-order within active session (if implementation exposes this distinction)
```

Exact status mapping is a Contract decision; response text must remain generic and bounded.

## 16. Node `/api/state` is no longer a production inter-machine dependency

M5.1 NODE currently exposes `/api/state` for Hub polling.

With M5.2, formal production topology no longer requires LAN access to this endpoint.

The technical contract should freeze one of these safe migration choices:

- retain `/api/state` only on loopback for local diagnostics/backward compatibility; or
- remove it from the packaged Node app surface after migration.

The business design explicitly prefers loopback/local socket only for local interfaces.

M5.2 must not preserve LAN exposure merely to keep the superseded Pull path alive.

## 17. Hub Web read APIs remain separate from Node write API

Existing Hub read behavior remains useful:

```text
GET /api/dashboard
GET /display
GET /health
```

M5.2 adds a write-side machine endpoint:

```text
POST /api/node/v1/snapshot
```

Human Web authentication and Node bearer authentication are separate concerns.

M5.2 does not create a user account/session system.

## 18. Failure isolation remains mandatory

A rejected or missing Node A update must not affect Node B.

Required per-node isolation includes:

- authentication failures;
- invalid JSON/schema;
- future/old/out-of-order state;
- temporary Node disconnect;
- Node reconnect;
- token mismatch;
- oversized payload.

Hub process failure remains outside per-node isolation; Hub restart repopulation is handled by Node heartbeat.

## 19. Migration target package boundaries

Recommended implementation direction after Contract Freeze:

```text
internal/hub/
  registry.go
  receiver.go
  validation.go
  store.go

internal/uplink/
  runtime.go
  client.go
  snapshot.go
```

Existing `internal/multihost` should remain historical/regression code during migration and then be retired from production runtime authority.

M5.2 Contract Freeze itself does not authorize runtime code changes.

## 20. Phase split confirmed by audit

The safest implementation sequence is:

### M5.2 — Contract only

Freeze NodeSnapshot, auth, ordering, retry, heartbeat, status, privacy and migration behavior.

### M5.3 — Hub Receiver Runtime

Implement Hub registry/receiver/store first and validate with a deterministic fake Node.

### M5.4 — Node Uplink Runtime

Implement state revision wake-up, PublicState snapshot client, heartbeat, reconnect and real Mac A → Hub validation.

This ordering localizes cross-machine failures: after Fake Node acceptance passes, remaining E2E failures are attributable to Node uplink/config/network rather than an unverified receiver.

## 21. Audit conclusion

```text
M5_2_REFERENCE_AUDIT = COMPLETE
ARCHITECTURE_REWRITE_REQUIRED = NO
TRANSPORT_SUPERSESSION_REQUIRED = YES
MATERIAL_CONTRACT_DECISIONS_REMAINING = NodeSnapshot ordering/idempotency + exact HTTP/auth/status bounds
IMPLEMENTATION_AUTHORIZED = NO
```

The next authorized action is to freeze `m5-2-node-hub-ingestion-v1.md`.