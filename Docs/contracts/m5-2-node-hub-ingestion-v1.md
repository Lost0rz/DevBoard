# DevBoard M5.2 Node Agent → NAS Hub Ingestion — Technical Contract V1

> Date: 2026-08-22
> Implementation base: `codex/m5-1-always-on-hub` @ `0955c9a581234f56e0925925485df5f4d33e90aa`
> Reference audit: `Docs/contracts/m5-2-node-hub-ingestion-reference-audit-v1.md` @ `7bf7d20c5d7503b9048a5bfc67fc9a89d154c99e`
> Parent technical contract: `Docs/contracts/m5-1-always-on-hub-v1.md`
> Status: **TECHNICAL CONTRACT FROZEN**
> Scope: Node→Hub ingestion transport, authentication, ordering, liveness and migration boundaries only. No DMG UI, final Dashboard redesign, Browser Watch, quota-account dedupe, remote approval/control or public user-auth product.

## 0. Supersession scope

M5.2 supersedes only the M5.1 rules that require the NAS Hub to initiate requests to Node endpoints.

M5.2 formally replaces:

```text
HUB → GET NODE /api/state
```

with:

```text
NODE → POST HUB /api/node/v1/snapshot
```

The following M5.1/M5 behavior remains inherited unless explicitly changed below:

- NODE and HUB authority separation;
- NODE local `state.Store` ownership;
- NODE System and Network collectors;
- NODE Codex/Claude local ingestion;
- PublicState privacy projection;
- DashboardState as the aggregate read model;
- per-node failure isolation;
- deep-copy/atomic accepted-state semantics;
- last-good retention concept;
- stateless NAS Hub MVP;
- no database, Redis, MQ or event sourcing;
- no NAS monitored-host card;
- no remote command/control authority;
- no direct public-Internet trust assumption.

Historical Pull contracts and code remain evidence/regression material but are no longer the production cross-machine authority after M5.4 closes.

## 1. Production topology

M5.2 freezes the formal topology as:

```text
Claude / Codex / local collectors
             ↓
      DevBoard Node Agent
             ↓
       sanitized PublicState
             ↓
 authenticated outbound snapshot
             ↓
       DevBoard NAS Hub
             ↓
 Receiver → NodeStateStore → DashboardState
             ↓
      Web / iPad / Browser
```

Rules:

1. Mac NODEs initiate cross-machine state transport.
2. HUB does not need to know or reach Node LAN IP addresses.
3. NODE A does not know NODE B.
4. NODE identity is not an IP address.
5. HUB remains the only multi-node aggregation authority.
6. NAS is infrastructure, not a monitored Mac host.

## 2. Runtime roles remain NODE and HUB

Exactly two production runtime roles remain:

```text
node
hub
```

### 2.1 NODE authority

NODE owns:

- one local monitored Host;
- local `state.Store`;
- System collector;
- Network collector;
- local Agent/Task reducers;
- Claude/Codex local ingestion;
- PublicState projection;
- Node Uplink runtime.

NODE MUST NOT own:

- multi-node Dashboard authority;
- another Node's state;
- Node Registry for other machines;
- Hub Receiver;
- remote control authority.

### 2.2 HUB authority

HUB owns:

- Node Registry;
- Node authentication;
- snapshot receiver;
- NodeStateStore;
- Hub-clock receive timestamps;
- connection status classification;
- last-good retention;
- DashboardState assembly;
- Dashboard/Web read APIs.

HUB MUST NOT run:

- Mac System collector;
- Mac Network collector;
- Claude/Codex local ingest socket;
- local monitored Host state;
- Node Uplink.

## 3. Local application boundary remains Unix socket

M5.2 does not replace the existing local provider ingestion model.

Formal local path:

```text
Claude Hook / Codex Hook / future local adapter
        ↓
local Unix Domain Socket
        ↓
Agent normalization/reducer
        ↓
state.Store
```

The local socket is the preferred machine-local integration boundary.

No local AI adapter needs to know:

- Hub URL;
- Node token;
- remote retry policy;
- other Node identities.

Only Node Uplink owns cross-machine transport.

## 4. PublicState is the nested monitoring payload

The Node sends only an already-sanitized `state.PublicState` nested inside the M5.2 envelope.

The Hub MUST NOT accept InternalState as a Node snapshot.

Nested state requirements:

```text
state.schemaVersion == 1
state.stateKind == "public"
state.host.id == envelope.nodeId
```

Existing PublicState privacy rules remain authoritative.

M5.2 does not introduce a parallel second public domain state model.

## 5. NodeSnapshot wire contract

The machine-to-machine request body is `NodeSnapshotV1`:

```json
{
  "schemaVersion": 1,
  "stateKind": "nodeSnapshot",
  "nodeId": "mac-a",
  "sessionId": "0123456789abcdef0123456789abcdef",
  "sequence": 42,
  "sentAt": "2026-08-22T10:00:00Z",
  "state": {
    "schemaVersion": 1,
    "stateKind": "public"
  }
}
```

Required top-level fields:

```text
schemaVersion
stateKind
nodeId
sessionId
sequence
sentAt
state
```

No unknown top-level field is required for V1 behavior. Implementations may reject unknown top-level fields to keep the machine contract strict.

### 5.1 Envelope version/kind

Exactly:

```text
schemaVersion = 1
stateKind = "nodeSnapshot"
```

### 5.2 Node ID

`nodeId`:

- length 1–64 bytes;
- ASCII letters, digits, `.`, `_`, `-` only;
- no leading/trailing whitespace;
- case-sensitive;
- stable for one configured Node identity.

Examples:

```text
mac-a
mac-b
studio-mac
```

### 5.3 Session ID

`sessionId` identifies one Node Uplink process session.

V1 format:

```text
32 lowercase hexadecimal characters
```

It represents 16 cryptographically random bytes generated when the Uplink runtime starts.

It is not a credential.

A new Node Uplink process MUST create a new random `sessionId`.

### 5.4 Sequence

`sequence` is a positive integer:

```text
sequence >= 1
```

It is monotonically increasing within one `sessionId`.

The first snapshot in a new session uses sequence `1`.

Retries of the same logical request MUST reuse the same session ID, sequence and payload.

A new snapshot MUST use a strictly higher sequence than the previous new snapshot in the same session.

### 5.5 sentAt and nested generatedAt

Node Uplink projects PublicState at send construction time.

V1 freezes:

```text
snapshot.sentAt == snapshot.state.generatedAt
```

using the same UTC instant.

Hub clock remains authority for receive age and connection status.

## 6. Receiver endpoint

HUB exposes exactly this ingestion route for M5.2:

```text
POST /api/node/v1/snapshot
```

Normal requirements:

```http
Content-Type: application/json
Authorization: Bearer <node-token>
```

No query string is part of the V1 machine contract.

No Node identity is taken from a path parameter.

The receiver MUST NOT accept snapshot state through `/api/dashboard` or `/api/state`.

## 7. Request size and bounded parsing

Maximum request body size:

```text
256 KiB
```

This inherits the proven M5 cross-machine bound.

Receiver behavior MUST ensure:

1. size bound is enforced before unbounded allocation;
2. JSON is decoded only inside the bound;
3. rejected bodies do not mutate NodeStateStore;
4. request body is never copied into logs;
5. raw body is not included in error responses.

Oversized request:

```text
HTTP 413
```

## 8. Node Registry

HUB maintains a configured Node Registry.

Each registered Node has at least:

```text
node_id
safe display_name
enabled
token credential/verifier
```

MVP registry is local Hub configuration. No database is introduced.

The exact config-file serialization is an implementation detail of M5.3, not part of the network wire contract, provided the semantic model above is preserved.

### 8.1 Registry identity authority

`node_id` is the machine identity authority.

Hub registry `display_name` is the aggregate/dashboard label authority.

Node-provided `state.host.displayName` MUST NOT be allowed to rename a different registered Node or create a new Node identity.

Dashboard wrappers should use registry display name as the trusted cross-node label.

### 8.2 Enabled flag

If a registered Node is disabled:

- its token does not authorize new ingestion;
- incoming requests are rejected;
- previously retained state may remain visible according to normal retention policy, clearly non-live;
- disabling one Node does not affect others.

## 9. Node token

Each Node has an independent opaque bearer token.

Required security properties:

- generated from at least 32 cryptographically random bytes;
- not derived from node ID, hostname or password-like user text;
- one active token per Node in M5.2 MVP;
- reset/replacement invalidates the previous active token;
- compared without ordinary early-exit string comparison where practical;
- never returned by Dashboard/Web APIs;
- never logged;
- never included in generic receiver error bodies.

A token is a machine credential, not a human Web login session.

## 10. Authentication and identity binding

Receiver authorization is valid only when all of these are true:

```text
Bearer token
  ↔ one enabled registered node
  == envelope.nodeId
  == state.host.id
```

Required outcomes:

### Missing or invalid bearer token

```text
HTTP 401
```

### Valid token but wrong/disabled Node binding

```text
HTTP 403
```

### Identity mismatch examples

All must be rejected:

```text
Token A + envelope.nodeId=mac-b
Token A + state.host.id=mac-b
Token A + envelope.nodeId=mac-a + state.host.id=mac-b
```

No rejected request advances last received time or accepted state.

## 11. Receiver validation order

The logical validation order is frozen as:

```text
method/path
→ bounded body/content checks
→ bearer authentication
→ envelope JSON/schema validation
→ registry enabled/node binding
→ nested PublicState schema/kind/host validation
→ timestamp validation
→ session/sequence/order validation
→ deep-copy accepted state
→ atomic NodeStateStore update
→ bounded acknowledgement
```

Implementation may combine internal pure validation steps, but MUST NOT mutate accepted Node state before all acceptance checks succeed.

## 12. PublicState structural validation

At minimum the Hub validates:

```text
state.schemaVersion == 1
state.stateKind == "public"
state.host.id == nodeId
state.generatedAt != zero
no duplicate task IDs
no empty task IDs
no duplicate agent IDs
no empty agent IDs
```

Existing M5-safe host ID validation is inherited.

The Hub is not required to re-run local InternalState projection logic; it validates the public contract it receives.

## 13. Timestamp acceptance

Hub clock owns acceptance windows.

For a new non-duplicate snapshot:

```text
state.generatedAt <= hubNow + 2 minutes
state.generatedAt >= hubNow - 30 seconds
```

Reason for tightening old M5 stale-input acceptance:

- Push Node can regenerate a fresh projection after reconnect;
- old transport packets should not refresh liveness;
- retained last-good state already handles disconnected visibility.

Therefore a newly arriving snapshot older than 30 seconds is not promoted to current accepted state.

A snapshot more than 2 minutes in the future is rejected.

Timestamp rejection does not advance `lastReceivedAt`.

## 14. Ordering and idempotency

Push transport introduces retries and possible delayed delivery. V1 freezes explicit rules.

For each registered node, Hub stores enough ingestion metadata to evaluate at least:

```text
active sessionId
last accepted sequence
last accepted payload digest
last accepted PublicState.generatedAt
lastReceivedAt
```

### 14.1 First accepted snapshot

If the Hub has no accepted state for a Node, any otherwise-valid session/sequence is accepted.

Normal Node behavior starts at sequence 1.

### 14.2 Same session, higher sequence

If:

```text
sessionId == active session
sequence > last sequence
```

then the snapshot may be accepted if all other validation passes.

### 14.3 Exact retry duplicate

If:

```text
sessionId == active session
sequence == last sequence
payload digest == last accepted digest
```

then the request is an idempotent duplicate.

Hub returns success without replacing domain state with a different value.

The duplicate MAY refresh `lastReceivedAt` because it proves the authenticated Node path is currently alive and represents a retry of the already accepted snapshot.

### 14.4 Same tuple, different body

If:

```text
same nodeId + sessionId + sequence
but different accepted payload digest
```

receiver rejects it as a conflict.

```text
HTTP 409
```

It does not mutate state.

### 14.5 Same session, lower sequence

Lower sequence is out-of-order/replayed input:

```text
HTTP 409
```

No state/liveness advancement.

### 14.6 New session

A different valid `sessionId` may become active if the snapshot is otherwise valid and does not regress accepted snapshot time.

For a Node with retained current state:

```text
new state.generatedAt >= currently accepted state.generatedAt
```

is required.

This prevents a delayed packet from an older process session from rewinding newer Node state.

Once a new session is accepted, later packets from the previous session are treated as non-active-session conflicts and MUST NOT replace current state.

### 14.7 Hub restart

Hub state is in memory.

After Hub restart there is no remembered active session/sequence.

The first valid authenticated snapshot repopulates the Node record regardless of its process-local sequence value.

## 15. Successful acknowledgement

Accepted new snapshot and exact idempotent duplicate both return:

```text
HTTP 200
Content-Type: application/json
Cache-Control: no-store
```

with a bounded body equivalent to:

```json
{"ok":true}
```

The response MUST NOT echo:

- token;
- Node registry secret data;
- nested PublicState;
- raw request body.

## 16. Error status classes

M5.2 freezes these externally observable classes:

```text
400 malformed JSON / invalid envelope / invalid nested public schema
401 missing or invalid bearer credential
403 valid credential but disabled or identity-binding failure
405 wrong method
409 ordering/session/payload conflict or stale snapshot admission conflict
413 request body too large
415 unsupported content type
500 internal receiver failure after request acceptance could not be safely completed
```

Error bodies must be bounded, generic and non-sensitive.

Implementation must not expose whether another Node ID exists when the requester is unauthenticated.

## 17. NodeStateStore

M5.3 introduces push-native `NodeStateStore` semantics.

Each configured Node record conceptually contains:

```text
nodeId
registry displayName
enabled
latest accepted PublicState (optional)
lastReceivedAt (optional)
active session metadata (optional)
connection status
snapshot freshness
bounded generic health/error metadata
```

Rules:

- independent synchronization/isolation per Node or equivalent safe locking;
- accepted PublicState is deep-copied on store ingress/egress;
- replacement is atomic per Node;
- rejection never partially overwrites last-good;
- Node A failure/rejection never clears Node B;
- registry order is stable for Dashboard assembly;
- no raw credential is copied into public state.

## 18. Connection status

Hub clock and `lastReceivedAt` define Node connection status.

M5.2 freezes:

```text
ONLINE  = age <= 5 seconds
STALE   = age > 5 seconds and <= 30 seconds
OFFLINE = age > 30 seconds
```

A registered Node that has never produced an accepted snapshot is represented as:

```text
OFFLINE
lastReceivedAt = null
state = null
```

Presentation may label this as "never seen", but it does not introduce a fourth wire status.

## 19. Snapshot freshness

Connection status and snapshot freshness are separate.

For an existing retained snapshot:

```text
FRESH only while connection status == ONLINE
STALE otherwise
```

Task/Agent internal freshness fields inside PublicState remain independently meaningful.

Therefore:

```text
Node OFFLINE
+ last-good PublicState retained
→ snapshotFreshness = STALE
```

The UI must never present retained stale data as current live state.

## 20. Last-good retention

M5.2 preserves the M5/M5.1 last-good retention duration:

```text
30 minutes from Hub last accepted/received success
```

During retention:

- stale/offline Node wrapper remains visible;
- last-good state may remain visible;
- state is explicitly stale when not ONLINE.

After retention expiry:

- nested PublicState is discarded;
- registered Node wrapper remains;
- Node remains OFFLINE until a new accepted snapshot arrives.

No disk persistence is required.

## 21. Hub restart behavior

Hub Monitoring MVP remains stateless.

On restart:

```text
Node Registry reloads from configuration
NodeStateStore has no accepted snapshots
registered Nodes appear OFFLINE / never seen
Node Uplink continues/reconnects
next accepted snapshots repopulate state
```

No migration database is introduced in M5.2–M5.4.

## 22. State-change wake-up contract

Current local `state.Store` has no change notification. M5.4 may extend it minimally.

Required behavior:

1. every successful `Replace`/`Update` that commits state advances an internal revision or equivalent change signal;
2. a Node Uplink can wait for a change notification without polling every producer separately;
3. producers do not invoke Hub HTTP directly;
4. notifications are coalescing wake-up hints, not a lossless event queue;
5. notification delivery must not block state writers.

Exact Go API naming is implementation detail.

The semantic contract is:

```text
local state changes
→ wake uplink
→ uplink snapshots latest state
```

## 23. Snapshot coalescing

The uplink transports current snapshots, not a history/event log.

If multiple local state updates occur while an upload is pending/in flight:

- do not enqueue every intermediate state;
- preserve at most the pending request plus knowledge that newer local state exists;
- after the pending request completes or is abandoned, generate the newest PublicState;
- intermediate superseded snapshots may be skipped.

At most one HTTP snapshot request may be in flight per Node Uplink.

## 24. Heartbeat cadence

When no newer state change requires immediate delivery, Node sends a fresh projected snapshot every:

```text
1 second
```

This is both heartbeat and state refresh for MVP.

No separate heartbeat endpoint is introduced.

Each heartbeat is a newly constructed snapshot with:

- fresh `sentAt`/`state.generatedAt`;
- same current session ID;
- next sequence.

Healthy operational target:

```text
local state change
→ Uplink wake-up
→ Hub accepted state typically < 1 second
→ Web visible according to dashboard refresh cadence
```

This remains a target, not hard realtime.

## 25. Immediate-change scheduling

A local change signal SHOULD wake the Uplink immediately.

M5.4 may coalesce a short burst before constructing the next state, but any coalescing delay must be bounded to:

```text
<= 100 ms
```

This prevents task/checkpoint/attention updates from waiting for the next 1-second heartbeat while avoiding network amplification from a burst of store mutations.

## 26. Request timeout

Node HTTP client per-request timeout:

```text
5 seconds maximum
```

Only one request is in flight, so a slow request cannot create an accumulating goroutine/request fan-out.

A timeout is a transport failure, not a local state failure.

## 27. Retry and reconnect

Transient transport failures include:

- DNS/connect failure;
- TLS/transport failure;
- timeout;
- HTTP 5xx.

For transient failures, the Node retries with bounded exponential backoff:

```text
1s → 2s → 4s → 8s → 15s max
```

Small bounded jitter may be added to avoid synchronized multi-node retry.

Rules:

- retry the exact pending envelope while it remains timestamp-valid;
- do not change sequence/body for a retry of the same logical request;
- if pending envelope becomes older than the 30-second receiver admission window, discard it and build the newest fresh snapshot with the next sequence;
- any local state change during backoff is remembered as "newer state available" but does not create concurrent requests;
- after success, return to normal change/1-second cadence.

## 28. Permanent/configuration error handling

### 401 / 403

Treat as authentication/configuration failure.

Node MUST NOT retry at 1-second heartbeat rate.

It may re-attempt at a slow bounded interval:

```text
30 seconds
```

or immediately after local configuration changes/restart.

### 400 / 413 / 415

Treat as protocol/payload failure.

The offending envelope is not retried indefinitely.

Node records a bounded generic local connection error and waits for a fresh snapshot/config/runtime correction.

### 409

Ordering/session conflict is not retried with the same conflicting envelope.

Node MUST:

1. abandon the conflicting pending envelope;
2. create a new random session ID;
3. reset sequence to 1;
4. build a fresh latest snapshot;
5. make one immediate resynchronization attempt;
6. if conflict persists, enter bounded slow retry/error behavior rather than loop rapidly.

## 29. Node local connection health

M5.4 runtime must internally expose enough state for later Settings UI to display:

```text
connected / disconnected
last attempt
last successful sync
bounded error class/message
```

This operational health is Node-local and need not be part of cross-node PublicState V1.

It MUST NOT contain the bearer token.

## 30. Hub address and transport security

Formal product configuration uses a stable Hub address rather than a Node LAN address.

Preferred/production scheme:

```text
https://<hub-address>
```

Node token must not be sent over untrusted cleartext transport.

M5.3/M5.4 deterministic tests and trusted-LAN engineering acceptance MAY use injected/explicit HTTP transport, but packaged production defaults must not silently downgrade HTTPS to HTTP.

External human Web access and Node machine authentication remain separate deployment concerns.

## 31. Node `/api/state` migration boundary

M5.1 required Hub access to Node `/api/state`.

M5.2 removes that production dependency.

For M5.3/M5.4 migration, NODE may retain:

```text
GET /api/state
```

for local diagnostics/regression compatibility only.

Formal packaged Node configuration must bind its local HTTP surface to loopback by default.

M5.4 must not require `0.0.0.0` or a fixed LAN IP for normal Hub operation.

Historical Pull polling must not remain a hidden fallback production path.

## 32. Hub read APIs

M5.2 retains Hub read-side behavior:

```text
GET /health
GET /api/dashboard
GET /display
```

HUB still has no local monitored PublicState:

```text
GET /api/state
→ 404
```

The new machine write route is independent:

```text
POST /api/node/v1/snapshot
```

## 33. DashboardState migration

M5.3 may evolve the aggregate wrapper from peer terminology to Node terminology, but the semantic read model remains:

```text
DashboardState
  hosts[]
    configured/registered node identity
    trusted display name
    source/connection status
    last receive/success time
    snapshot freshness
    optional sanitized PublicState
```

The Dashboard must never expose:

- bearer token;
- token verifier;
- raw Authorization header;
- private Node config secret;
- raw receiver error;
- full request body.

## 34. Privacy boundary

Node Uplink may send only already-sanitized PublicState plus the M5.2 envelope metadata.

Explicitly prohibited Node→Hub payload additions:

- cwd absolute path;
- raw repo/worktree absolute root;
- raw prompt;
- raw transcript;
- raw provider event;
- raw tool payload;
- shell command;
- API key/token other than the transport bearer in the HTTP Authorization header;
- private correlation IDs not already approved in PublicState;
- InternalState.

Hub logs and Dashboard must preserve the same boundary.

## 35. Logging

### Node logs may include

- node ID;
- Hub host (without credential/query secrets);
- connection state;
- attempt/success time;
- HTTP status class;
- retry/backoff classification;
- session ID/sequence only if useful and not treated as secret.

### Hub logs may include

- authenticated node ID after authentication succeeds;
- generic validation class;
- accepted sequence/session diagnostic metadata;
- receive time;
- bounded generic errors.

### Logs MUST NOT include

- bearer token;
- Authorization header;
- raw NodeSnapshot JSON;
- raw PublicState body;
- prompt/transcript/tool payload secrets.

## 36. Failure isolation

The following failures for Node A must not mutate, erase or block Node B:

- missing token;
- bad token;
- disabled registry entry;
- identity mismatch;
- malformed JSON;
- oversized body;
- stale/future timestamp;
- duplicate conflict;
- out-of-order sequence;
- Node A network loss;
- Node A process stop/restart.

NodeStateStore and receiver concurrency must preserve independent-node progress.

## 37. No database / queue

M5.2–M5.4 explicitly do not add:

- PostgreSQL;
- SQLite persistence for Hub snapshots;
- Redis;
- Kafka/NATS/RabbitMQ;
- event sourcing;
- durable telemetry history.

Current-state monitoring remains an in-memory latest-state product.

## 38. M5.3 implementation boundary — Hub Receiver Runtime

After this Contract Freeze, M5.3 is authorized to implement only the Hub side needed for deterministic ingestion.

Expected production concepts:

```text
internal/hub/
  registry
  receiver
  validation
  NodeStateStore
  Dashboard assembly adapter
```

M5.3 SHOULD retire `PeerSnapshotStore` from production Hub authority rather than stretch peer/poller vocabulary into Push semantics.

Historical multihost tests may remain for regression evidence while migration is in progress.

## 39. M5.3 Fake Node acceptance

Before real Node Uplink work is authorized, deterministic tests/fake-client validation must prove at least:

1. valid mac-a token + mac-a envelope accepted;
2. missing token rejected;
3. invalid token rejected;
4. token A + nodeId mac-b rejected;
5. token A + nested host mac-b rejected;
6. disabled Node rejected;
7. invalid JSON rejected;
8. invalid envelope version/kind rejected;
9. nested non-public state rejected;
10. oversized body rejected;
11. future timestamp rejected;
12. >30s-old new snapshot rejected;
13. sequence advances in one session;
14. exact duplicate is idempotent success;
15. same tuple/different body rejected;
16. lower sequence rejected;
17. new session with non-regressing generatedAt accepted;
18. old session cannot overwrite newer accepted session;
19. Node A rejection does not change Node B;
20. ONLINE→STALE→OFFLINE thresholds work from Hub clock;
21. last-good remains stale through 30-minute retention;
22. retention expiry drops nested state but keeps registry wrapper;
23. Hub starts with no NAS monitored Host;
24. Hub restart/new store can repopulate from next valid snapshot;
25. Dashboard/API contain no token or request body leakage.

Required closure marker:

```text
M5_3_HUB_RECEIVER_FAKE_NODE = PASS
```

Only then should M5.4 real Node Uplink begin.

## 40. M5.4 implementation boundary — Node Uplink Runtime

M5.4 may implement:

- minimal state revision/change notification;
- NodeSnapshot builder;
- Node Uplink HTTP client;
- one-in-flight scheduler;
- <=100ms change coalescing;
- 1-second heartbeat;
- session/sequence semantics;
- retry/backoff;
- auth/protocol error classification;
- loopback-only packaged local HTTP default;
- real Mac A → Hub runtime connection.

M5.4 must not add DMG Settings UI; that remains M5.5.

## 41. M5.4 real Mac A acceptance

Required real acceptance:

1. Mac A Node starts local collectors and agent ingest without Hub dependence;
2. Node can reach configured Hub address without Hub reaching Mac LAN IP;
3. valid token authenticates mac-a;
4. Hub shows only registered mac-a, no NAS host;
5. real Claude/Codex task state reaches Hub;
6. checkpoint/attention change wakes Uplink before normal heartbeat where observable;
7. completion reaches Hub;
8. local System/Network updates continue;
9. stopping Node leads ONLINE→STALE→OFFLINE;
10. stale last-good remains clearly stale;
11. restarting Node creates a new session and recovers without Hub restart;
12. temporary Node network interruption reconnects automatically;
13. Hub restart repopulates from Node heartbeat;
14. no fixed Mac LAN IP is required;
15. no token/raw sensitive state appears in Dashboard/logs;
16. historical Hub poller is not required for production success.

Required closure marker:

```text
M5_4_MAC_A_NODE_HUB_E2E = PASS
```

## 42. Explicit non-goals

Not in M5.2–M5.4:

- macOS Settings UI;
- DMG packaging;
- LaunchAgent/login-item packaging closure;
- Mac B onboarding closure;
- final new Dashboard visual redesign;
- Browser AI Watch;
- quota/account canonical dedupe;
- remote approval;
- arbitrary remote command execution;
- user accounts/sessions/OAuth;
- database/history analytics;
- WebSocket/SSE requirement;
- auto-update.

Those remain later phases.

## 43. Stage sequence after freeze

The authorized order is fixed:

```text
M5.2 Contract Freeze
        ↓
M5.3 Hub Receiver Runtime
        ↓
Fake Node acceptance
        ↓
M5.3 audit/closure
        ↓
M5.4 Node Uplink Runtime
        ↓
Mac A real E2E
        ↓
M5.4 audit/closure
        ↓
M5.5 Node.app + DMG
```

Do not develop M5.3 and M5.4 simultaneously before Fake Node receiver closure.

## 44. Freeze result

The business decisions are frozen as:

```text
TOPOLOGY                    = NODE_PUSH_TO_HUB
NODE_PACKAGE_MODEL          = ONE_GENERIC_NODE_PRODUCT
IDENTITY                    = NODE_ID_PLUS_PER_NODE_TOKEN
LOCAL_APP_BOUNDARY          = UNIX_SOCKET_LOCAL_ONLY
HUB_STATE                   = IN_MEMORY_LATEST_STATE
HEARTBEAT                    = 1_SECOND_FRESH_SNAPSHOT
CHANGE_DELIVERY             = STORE_WAKEUP_PLUS_COALESCING
MAX_IN_FLIGHT_PER_NODE       = 1
BODY_LIMIT                   = 256_KIB
CONNECTION_ONLINE            = <=5_SECONDS
CONNECTION_STALE             = >5_AND_<=30_SECONDS
CONNECTION_OFFLINE           = >30_SECONDS
LAST_GOOD_RETENTION          = 30_MINUTES
ORDERING                     = SESSION_PLUS_SEQUENCE_PLUS_TIME_NON_REGRESSION
HUB_CLOCK                    = RECEIVE_LIVENESS_AUTHORITY
NODE_LAN_IP                  = NOT_IDENTITY_NOT_REQUIRED
DATABASE                     = NONE
PULL_FALLBACK                = NOT_PRODUCTION_AUTHORITY
```

Final status:

```text
M5_2_TECHNICAL_CONTRACT = FROZEN
UNRESOLVED_MATERIAL_DECISIONS = NONE
M5_3_IMPLEMENTATION = AUTHORIZED
M5_4_IMPLEMENTATION = BLOCKED_UNTIL_M5_3_FAKE_NODE_PASS
```
