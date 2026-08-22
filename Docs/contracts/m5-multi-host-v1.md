# DevBoard M5 Multi-Host Read-Only Dashboard — Technical Contract V1

> Date: 2026-08-22  
> Engineering base: `codex/m4-task-observability` @ `2d4499dae7543667baa781079efd468ef0532c01`  
> Parent business contracts: `mvp-monitoring-v1.md`, `mvp-feature-freeze-v1.md`  
> Reference audit: `m5-multi-host-reference-audit-v1.md`  
> Status: **TECHNICAL CONTRACT FROZEN**  
> Scope: M5 only. Contract/documentation branch; runtime implementation is not part of this commit.

## 0. M4 dependency status

M4 implementation and local validation are complete at:

`2d4499dae7543667baa781079efd468ef0532c01`

M4 closure is still **BLOCKED_EXTERNAL_PROVIDER**, because real Claude completion validation could not be observed while the provider returned HTTP 429. M5 is allowed to proceed because the user explicitly authorized the next read-only contract milestone without changing M4's closure status.

M5 must not reinterpret M4 as closed and must not change M4 task semantics.

---

# 1. TOPOLOGY

M5 freezes a **SERVER-SIDE PULL AGGREGATOR**.

```text
Mac A DevBoard
  local Store
     ↓
  PublicState
  GET /api/state  ←──────────────┐
                                 │ bounded GET polling
                                 │
                         Aggregator DevBoard
                                 │
                                 ├─ direct local PublicState projection
                                 │
                                 └─ bounded Peer Snapshot Store
                                              ↓
                                         DashboardState
                                         ↙          ↘
                              GET /api/dashboard    /display

Mac B DevBoard
  local Store
     ↓
  PublicState
  GET /api/state  ←──────────────┘
```

A DevBoard node remains independently authoritative for its own local collectors and tasks. The aggregator does not become a collector-of-record for another Mac.

No push agent, message broker, WebSocket mesh, database, event stream, distributed coordination, or cloud service is part of M5.

---

# 2. LOCAL VS REMOTE AUTHORITY

## Local host

The local DevBoard Store remains authority for:

- local Host;
- local System;
- local Network;
- local Agents;
- local Tasks;
- local Sources;
- existing local compatibility fields.

The aggregator obtains its own local snapshot by calling the existing PublicState projector directly over `Store.Snapshot()`.

It MUST NOT HTTP-poll itself.

## Remote host

The remote DevBoard node is authority for the content of its own sanitized `PublicState`.

The aggregator is authority only for **peer transport/aggregation metadata**, such as:

- expected peer identity;
- LastAttemptAt;
- LastSuccessAt;
- peer source status;
- whether the retained snapshot is fresh or stale.

The aggregator MUST NOT rewrite remote task/system/network facts as though it observed them locally.

---

# 3. PEER CONFIGURATION

M5 extends the existing scalar config style rather than introducing a generic YAML library.

Frozen conceptual/config syntax:

```yaml
multi_host:
  enabled: false
  peers: macbook=192.168.1.50:8787,mac-mini-2=100.64.0.12:8787
```

`multi_host.enabled`:

- default: `false`;
- `false`: no peer runtime is started;
- `true` with zero peers: valid and behaves as a one-host aggregator.

`multi_host.peers`:

- an ordered comma-separated scalar;
- each item is `expected_host_id=ip_literal:port`;
- whitespace around entries is ignored;
- order is preserved and becomes dashboard peer order.

## Expected host ID

`expected_host_id`:

- 1–64 UTF-8 bytes;
- ASCII letters, digits, `.`, `_`, `-` only for M5 config simplicity;
- must be unique within peer configuration;
- must not equal the local configured `host.id`;
- is not a replacement for the remote node's asserted `PublicState.host.id`.

## Address

Peer address:

- MUST be an IP literal plus port;
- IPv4 form: `192.168.1.50:8787`;
- IPv6 ULA form uses bracketed host-port syntax;
- port range: 1–65535;
- no scheme;
- no path;
- no query;
- no userinfo;
- no fragment;
- no headers;
- no credentials.

The implementation constructs exactly:

`http://<configured-ip:port>/api/state`

No arbitrary URL template exists in M5.

## Duplicate config

Startup/config validation rejects:

- duplicate expected host IDs;
- duplicate normalized peer endpoints;
- expected host ID equal to local host ID;
- invalid IP/port;
- disallowed network address classes.

A configuration error is fail-fast before peer polling starts. It does not partially enroll an ambiguous peer set.

---

# 4. DISCOVERY POLICY

M5 uses **explicit configured peers only**.

Not in M5:

- mDNS;
- Bonjour;
- Zeroconf;
- broadcast scanning;
- subnet scanning;
- automatic peer enrollment.

Discovery is deferred unless later operational evidence justifies it.

---

# 5. LOCAL-HOST INCLUSION

The aggregator always includes itself as the first dashboard host.

Local state path:

```text
Store.Snapshot()
→ existing PublicState projection
→ DashboardHostSnapshot
```

There is no localhost HTTP dependency.

At the aggregation boundary, local and remote nodes use the same `DashboardHostSnapshot` shape, but source kind records whether the snapshot came from `local` direct projection or a `peer` fetch.

---

# 6. HOST IDENTITY / MISMATCH / COLLISION

Host identity is first-class and must never be silently rewritten.

## Authority invariant

For a remote snapshot to be accepted:

```text
configured expected_host_id
==
remote PublicState.host.id
```

The configured ID identifies which machine the operator intended to enroll.

The remote `PublicState.host.id` is the identity asserted by that DevBoard node.

Both must agree.

## Mismatch

If a reachable peer returns a different `host.id`:

- mark that peer **degraded**;
- do not promote the mismatching response into last-good state;
- retain a previously accepted matching snapshot only according to normal stale retention;
- expose a bounded identity-mismatch diagnostic without the peer endpoint;
- never relabel the returned machine as the expected machine.

## Duplicate observed host IDs

If two configured peers return the same remote `host.id`, or a peer returns the local host's ID:

- this is an explicit host-identity conflict;
- conflicting remote response(s) are degraded;
- no duplicate snapshots are merged or collapsed;
- local identity remains local authority;
- conflict must be visible in peer source state;
- no task/agent from the rejected conflicting response enters the aggregate dashboard state.

The implementation must not rely solely on config validation; runtime duplicate detection remains mandatory.

---

# 7. API ENDPOINT SEMANTICS

## `GET /api/state`

**Frozen unchanged.**

`/api/state` always means:

> the sanitized PublicState of exactly one local DevBoard node.

It MUST NOT return an aggregate dashboard merely because the node is also configured as an aggregator.

Properties preserved:

- GET only;
- `stateKind = "public"`;
- PublicState `schemaVersion = 1`;
- `Cache-Control: no-store`;
- current PublicState privacy contract.

## `GET /api/dashboard`

M5 adds a separate read-only aggregate endpoint:

> sanitized server-side aggregation of the local PublicState plus configured peer PublicState snapshots and bounded peer-source metadata.

Frozen top-level shape:

```json
{
  "schemaVersion": 1,
  "stateKind": "dashboard",
  "generatedAt": "2026-08-22T06:00:00Z",
  "hosts": []
}
```

The Dashboard schema version is independent from the nested PublicState schema, even though both begin at version 1.

`/api/dashboard` is GET only and MUST use `Cache-Control: no-store`.

It does not accept peer definitions, actions, filters that trigger outbound arbitrary fetches, request bodies, or control commands.

---

# 8. ANTI-RECURSION GUARANTEE

Aggregation loops are made **structurally impossible** by endpoint semantics:

- peer collectors fetch only fixed `/api/state`;
- `/api/state` is permanently one local `PublicState`;
- aggregate state exists only at `/api/dashboard` and `/display`.

Therefore even if A and B are both aggregators:

```text
A → B /api/state = B local only
B → A /api/state = A local only
```

No nested DashboardState is fetched or decoded as a peer snapshot.

Payload validation additionally requires `stateKind == "public"`, so an aggregate `stateKind == "dashboard"` response is rejected.

---

# 9. AGGREGATION STATE MODEL

M5 chooses a **separate `DashboardState`** rather than mutating `PublicState` into a multi-host root and rather than creating ambiguous `root.host + remoteHosts` authority.

Frozen conceptual model:

```text
DashboardState
- SchemaVersion = 1
- StateKind = "dashboard"
- GeneratedAt
- Hosts []DashboardHostSnapshot

DashboardHostSnapshot
- ConfiguredHostID
- Source
- SnapshotFreshness?   // present only when State exists
- State *PublicState   // exactly one accepted sanitized host snapshot

DashboardHostSource
- Kind                 // local | peer
- Status               // unknown | available | degraded | unavailable
- LastAttemptAt?
- LastSuccessAt?
- Message              // bounded generic diagnostic
```

## Public properties

`ConfiguredHostID` is safe operator-configured host identity, not an endpoint.

No peer IP address, port, raw URL, transport error string, DNS material, or credentials are exposed through DashboardState.

For an accepted remote state:

`ConfiguredHostID == State.Host.ID`.

For an unobserved/unavailable peer with no accepted state, the configured host ID allows a stable host card without inventing System/Network/Task facts.

## Why nested PublicState

Keeping an accepted node snapshot as one `PublicState`:

- reuses the existing privacy contract;
- keeps host source boundaries explicit;
- avoids merging remote entities into local collections;
- preserves future additive M6 host-scoped fields without redesigning M5 transport;
- preserves per-node SourceHealth semantics.

---

# 10. POLLING INTERVAL

Remote peers are polled every **5 seconds**.

Reason:

- current local System and Network runtimes already operate on a 5-second default cadence;
- task events remain event-driven locally and become visible at the next bounded peer poll;
- 5 seconds is sufficiently responsive for a persistent status board without creating a high-frequency distributed transport.

No configurable sub-second or arbitrary polling is introduced in M5.

---

# 11. PEER REQUEST TIMEOUT

Each peer GET has a hard **1500 ms** timeout.

This matches the current default network probe timeout and ensures one dead peer does not hold dashboard freshness hostage.

The timeout covers the entire HTTP request/response operation for the small state payload.

---

# 12. OVERLAP PREVENTION

Each configured peer has at most **one in-flight poll**.

Frozen runtime rule:

```text
poll
→ complete / timeout / cancel
→ atomic peer-state update
→ wait for next cadence
→ next poll
```

A slow or stuck request never causes accumulating poll goroutines.

No poll for the same peer is started while its prior poll is alive.

Random jitter is not required for the M5 two-Mac use case. Correctness must not depend on synchronized timing, and peers remain independent.

---

# 13. STARTUP BEHAVIOR

HTTP server startup MUST NOT wait for remote peers.

When M5 is enabled:

1. local Store/server initializes normally;
2. configured peers appear immediately as `unknown` host entries with no fabricated snapshot;
3. peer polling begins asynchronously;
4. each peer performs its first poll promptly after runtime start;
5. `/display` and `/api/dashboard` remain available even if every peer is offline.

No peer is required for DevBoard startup.

---

# 14. SHUTDOWN CANCELLATION

The peer runtime owns a root cancellation context and a completion barrier.

Shutdown/Close:

- cancels all per-peer request contexts;
- stops timers/tickers;
- waits for peer loops to finish;
- does not leave background goroutines alive.

The pattern should remain consistent with current System/Network runtime `Close()` behavior.

M5 does not require redesigning the whole process signal/shutdown model; it requires that its own runtime be correctly cancellable when closed.

---

# 15. PEER SOURCE HEALTH

M5 peer source status is distinct from the remote host's own System/Network/Agent source health.

Frozen peer status enum:

- `unknown` — no completed poll yet;
- `available` — latest completed peer response was accepted and current enough;
- `degraded` — peer responded/reached transport but response is stale, malformed, incompatible, oversized, identity-conflicting, or otherwise not trustworthy as a current snapshot;
- `unavailable` — current poll cannot obtain a usable HTTP response because of connect/route/timeout/transport/non-success HTTP failure.

Peer source metadata:

- `LastAttemptAt`: aggregator receive/attempt clock; advances on every completed/failed attempt;
- `LastSuccessAt`: advances only after a fully accepted matching PublicState snapshot;
- `Message`: bounded, sanitized operational category;
- raw transport error: private only.

Examples of safe public messages:

- `Peer snapshot available.`
- `Peer unavailable.`
- `Peer response invalid.`
- `Peer host identity mismatch.`
- `Peer snapshot is stale.`

Raw URLs, IPs, payloads, and error strings are not required in the public dashboard state.

---

# 16. LAST-GOOD SNAPSHOT

M5 retains the most recent fully accepted remote `PublicState` when later polling fails.

This is mandatory for operational usefulness.

A failed poll does not erase a machine and all of its last-known tasks immediately.

Example:

```text
MACBOOK
PEER UNAVAILABLE · LAST SEEN 42s AGO
STALE SNAPSHOT
  SYSTEM ...
  NETWORK ...
  TASK ...
```

The UI must label retained content stale and must not present it as current fact.

A failing response never partially overwrites the last-good snapshot.

---

# 17. STALE VS UNAVAILABLE SEMANTICS

These are independent dimensions:

- **peer source status** says whether the aggregator can currently obtain/accept the peer response;
- **snapshot freshness** says whether displayed state is current enough to be treated as fresh.

Examples:

### Peer unavailable + stale snapshot

The Mac cannot currently be reached, but a retained last-good snapshot exists.

### Peer degraded + stale snapshot

HTTP succeeds, but the returned PublicState is too old or otherwise cannot be treated as current.

### Peer available + fresh snapshot

Latest poll succeeded and the accepted snapshot is current.

### Peer unavailable + no snapshot

The peer has never produced an accepted snapshot or retention has expired. Show only the configured host card/source status; do not invent domain data.

---

# 18. STALE RETENTION

Frozen last-good retention: **30 minutes from aggregator `LastSuccessAt`**.

A retained state is considered fresh only when both are true:

- aggregator `now - LastSuccessAt <= 15 seconds`;
- remote `generatedAt` is no more than 30 seconds old, subject to the clock-skew allowance below.

Otherwise the state is displayed as `stale`.

After **30 minutes** without an accepted snapshot:

- discard the retained remote `PublicState` content;
- keep the explicitly configured host entry;
- show peer unavailable/degraded with no System/Network/Task facts.

Configured hosts do not silently disappear merely because they are offline.

Removing the peer from configuration is what removes its configured host entry.

---

# 19. CLOCK SKEW / TIMESTAMP AUTHORITY

Remote timestamps remain source-host facts and are not rewritten by the aggregator.

Aggregator timestamps (`LastAttemptAt`, `LastSuccessAt`, `DashboardState.GeneratedAt`) use the aggregator clock.

## Future timestamp allowance

A remote `generatedAt` up to **2 minutes in the future** relative to aggregator receive time is tolerated as ordinary clock skew.

For presentation:

- raw remote timestamp remains unchanged;
- negative relative ages/elapsed durations are clamped to zero for display only.

If `generatedAt > aggregator_now + 2 minutes`:

- response is degraded for clock skew;
- it does not replace last-good state;
- `LastSuccessAt` does not advance.

## Old source snapshot

If accepted response `generatedAt` is:

- `<= 30 seconds` old: current enough to be fresh;
- `> 30 seconds` and `<= 30 minutes` old: structurally acceptable but snapshot is stale and peer source is degraded;
- `> 30 minutes` old: too old to become a new last-good snapshot; reject it as current data and retain any prior last-good snapshot under normal retention.

No NTP/distributed clock synchronization subsystem is introduced.

---

# 20. PEER PAYLOAD VALIDATION

HTTP 200 alone is insufficient.

A peer response must pass all of the following before becoming last-good state:

1. body is within the hard size limit;
2. valid JSON;
3. `stateKind == "public"`;
4. `schemaVersion == 1`;
5. required Host object is present;
6. `host.id` is non-empty and within the existing/configured identity bounds;
7. `host.id == configured expected_host_id`;
8. no runtime duplicate/collision with local/other accepted peers;
9. structural required PublicState fields decode safely;
10. timestamps pass the bounded clock rules;
11. task IDs are unique within the snapshot;
12. agent IDs are unique within the snapshot.

Unknown additive fields inside `schemaVersion=1` are allowed/ignored by the current typed decoder. M5 must not use strict unknown-field rejection that would defeat additive schema evolution.

A `stateKind == "dashboard"` response is invalid as a peer snapshot.

Malformed/invalid payload:

- affects only that peer;
- does not crash the aggregator;
- never partially mutates last-good state;
- is not logged in full.

---

# 21. PAYLOAD BODY LIMIT

Frozen response body hard limit: **256 KiB per peer response**.

Reason:

- current PublicState snapshots are small;
- 256 KiB gives substantial headroom for future M6/M7 additive public fields;
- the bound prevents an accidental or malicious peer from allocating unbounded memory.

Implementation behavior:

- optionally reject declared `Content-Length > 256 KiB` early;
- always enforce the limit while reading, independent of Content-Length;
- detect one byte beyond the limit;
- mark the peer degraded;
- do not log the oversized body.

---

# 22. REDIRECTS

HTTP redirects are **disabled** for peer polling.

A 3xx response is not followed to another address/path and is treated as an unusable peer response.

This prevents a configured safe endpoint from becoming an indirect arbitrary fetch target.

---

# 23. SSRF BOUNDARY

M5 peer configuration is intentionally narrower than a URL.

Allowed address classes:

### IPv4

- RFC1918 private ranges:
  - `10.0.0.0/8`;
  - `172.16.0.0/12`;
  - `192.168.0.0/16`;
- CGNAT `100.64.0.0/10` for Tailscale/private overlay use.

### IPv6

- ULA `fc00::/7`.

Rejected:

- public/global Internet IPs;
- loopback;
- unspecified addresses;
- multicast;
- IPv4/IPv6 link-local in M5;
- hostnames/DNS names;
- arbitrary schemes;
- arbitrary paths/query;
- embedded credentials;
- redirects.

Requiring IP literals eliminates DNS-rebinding behavior from the M5 peer fetch path.

Local state is included directly, so loopback is unnecessary.

---

# 24. TRUST / AUTHENTICATION / TLS MODEL

M5 MVP trust boundary is:

**EXPLICITLY CONFIGURED TRUSTED PRIVATE LAN OR VPN ONLY.**

M5 does not claim to make DevBoard safe for direct public-Internet exposure.

No new M5 user/account system, shared token service, certificate management, or custom TLS PKI is required for closure.

Transport for the M5 host:port contract is fixed HTTP. When network confidentiality/authentication is needed, use a trusted private overlay/VPN such as Tailscale or equivalent and bind DevBoard only on the intended interface.

M5 does not automatically widen the current HTTP server bind address. Each monitored Mac must be deliberately configured to listen on the trusted LAN/VPN interface used by the peer.

Although `/api/state` is sanitized, task/project/system state remains private operational metadata. Trusted-network scope is therefore a real product boundary, not an assertion that the state is non-sensitive.

A future Internet-facing mode would require a separate authenticated/TLS contract and is not implied by M5.

---

# 25. HOST-SCOPED DATA

M5 aggregates existing **host-scoped** sanitized state:

- Host;
- System;
- Network;
- Agents;
- Tasks;
- Sources associated with those host-local collectors;
- other existing PublicState compatibility fields as passive nested state.

M5 UI priority is specifically Host/System/Network/Tasks/Agent/source status.

## Process Groups

Legacy ProcessGroups compatibility fields may still exist inside a nested `PublicState`, but M5 MUST NOT revive, expand, or feature them.

## Navigation/control

Nested existing navigation compatibility fields do not grant remote action authority. M5 performs no remote navigation/action and its aggregate UI adds no control.

---

# 26. FUTURE BROWSER AI WATCH EXTENSION POINT

M6 Browser AI Watch is future **host-scoped** state.

By retaining each host as a nested sanitized PublicState in DashboardState, M6 can later add a browser-watch public collection to each node's PublicState and M5 aggregation can carry it without changing host identity or peer transport semantics.

M5 does not define or implement the Browser AI Watch field itself.

---

# 27. QUOTA BOUNDARY

Quota is provider/account-scoped, not machine capacity.

Current `PublicState.quota` compatibility data may remain present in nested per-host snapshots, but the M5 unified host UI MUST NOT present identical account quota on two machines as two independent capacities.

M5 does not invent cross-host quota deduplication/account identity.

M7 owns:

- quota source truth;
- provider/account identity;
- cross-host dedupe/aggregation;
- remaining/reset presentation.

Until M7, per-host quota is not promoted as a multi-host capacity metric.

---

# 28. CROSS-HOST TASK / AGENT IDENTITY

Existing task and agent IDs are scoped by host in the aggregate view.

## Tasks

Global presentation/view key:

```text
(host.id, task.id)
```

The nested `PublicTask.id` itself is unchanged.

M5 does not require task IDs to be globally unique across machines.

## Agents

Global presentation/view key:

```text
(host.id, agent.id)
```

An Agent ID or session identifier observed on Mac A is never assumed to refer to the same entity on Mac B.

Composite view keys are presentation/aggregation identity only; they do not change M4 task identity authority.

---

# 29. HOST ORDERING

Dashboard host order is deterministic:

1. local host first;
2. configured peers in `multi_host.peers` order.

Host cards do not reorder based on health, activity, or polling completion time.

Within a host, existing task priority/ordering semantics remain applicable.

A global attention strip may prioritize items across hosts, but every global item MUST include the host label and use host-scoped composite identity.

---

# 30. STORE / UPDATE AUTHORITY

M5 introduces a **separate peer snapshot store**, not remote writes into the local `state.Store`.

Frozen concurrency model:

```text
Local collectors / M4 reducer
        ↓
existing local state.Store

Peer poll A ─┐
Peer poll B ─┼─ atomic per-peer replacement → PeerSnapshotStore
Peer poll C ─┘

Dashboard assembly
  = local Store.Snapshot()
  + PeerSnapshotStore.Snapshot()
```

PeerSnapshotStore requirements:

- own `RWMutex` or equivalent bounded synchronization;
- keyed by configured peer identity;
- one atomic record replacement per completed peer poll;
- deep-copy PublicState on write/snapshot;
- never expose mutable shared slices/maps/pointers;
- one peer update cannot erase another peer record;
- a failed poll updates only that peer's source metadata and retains its prior last-good state if within retention.

This avoids lost updates with local System/Network/Task reducers and preserves source-host boundaries.

No central DB/event log is required.

---

# 31. MOCK SEMANTICS

`--mock` performs **no outbound peer polling**.

M5 adds a deterministic exactly-two-host DashboardState fixture:

### Host 1 — local synthetic Mac

- distinct System values;
- distinct Network values;
- active task;
- recent completion.

### Host 2 — synthetic configured remote Mac

- distinct System values;
- distinct Network values;
- task requiring attention;
- peer source shown as stale/degraded with a retained last-good snapshot.

Mock output must be deterministic for a fixed request time and must not depend on network, random discovery, hostname, machine ID, or current external state.

Mock continues to avoid real System/Network/agent collectors.

---

# 32. DESKTOP INFORMATION HIERARCHY

M5 changes only enough presentation to make host scope immediately obvious.

Default `/display` conceptual hierarchy:

```text
ATTENTION (optional global summary)
  MACBOOK · CLAUDE · ...

HOST · MAC MINI
  SYSTEM
  NETWORK
  TASKS

HOST · MACBOOK
  PEER STALE · LAST SEEN 42s AGO
  SYSTEM
  NETWORK
  TASKS
```

Every task/problem is visually attached to a host or explicitly carries its host label.

M4 task card hierarchy remains:

```text
PROVIDER · PROJECT / BRANCH
TASK TITLE
LIFECYCLE · ELAPSED
CHECKPOINT
ACTION REQUIRED
COMPLETION
```

M5 is not the final M8 responsive/density redesign.

The richer desktop surface may show bounded peer source health. It must not show peer endpoint/IP, raw transport errors, or remote private state.

---

# 33. KINDLE BOUNDARY

M5 freezes Kindle as **local-host-only** until M8 display closure.

Reasons:

- M5 product acceptance requires one unified web state/view, which `/api/dashboard` and default `/display` satisfy;
- the frozen Kindle is a highly constrained old-WebKit appliance with a carefully tuned Agent Deck;
- M8 explicitly owns final Kindle/tablet/phone/desktop adapted multi-domain presentation;
- forcing multi-host rotation/density into M5 would silently expand M8 scope.

M5 implementation MUST NOT redesign `internal/web/templates/kindle.html`.

Existing `/display/kindle` continues to use local PublicState and existing selection/rotation semantics.

A future M8 contract may consume DashboardState on Kindle after explicit display-density design.

---

# 34. BACKWARD COMPATIBILITY

M5 preserves:

- M3.1 System semantics;
- M3.2 Network semantics;
- M4 Agent/Task semantics and retention;
- existing local `/api/state` contract;
- PublicState `schemaVersion = 1`;
- local mock semantics, extended additively for DashboardState;
- current `/display/kindle` behavior;
- PublicState privacy projection;
- `safeNavigationEnabled=false` authority;
- single-host startup with no peer dependency.

When `multi_host.enabled=false` or there are zero peers:

- `/api/state` is unchanged;
- `/api/dashboard` contains exactly the local host;
- `/display` remains functionally a single-host dashboard;
- `/display/kindle` remains unchanged;
- no outbound peer goroutine is required when disabled.

No second Mac is required merely to start DevBoard.

---

# 35. PRIVACY

Remote transport consumes **only already-sanitized `PublicState`**.

M5 MUST NOT request or transport remote:

- InternalState;
- cwd / worktree root;
- raw provider events;
- raw prompts;
- raw final responses;
- shell commands;
- tool input/output;
- transcripts;
- private correlation IDs;
- credentials/secrets.

M5 public additions are limited to:

- configured safe host identity;
- source kind;
- bounded peer status/freshness timestamps;
- bounded generic source message;
- nested already-sanitized PublicState.

M5 MUST NOT expose:

- configured peer endpoint/IP/port;
- raw request URL;
- raw network error;
- raw malformed response;
- response headers;
- private polling implementation data.

No new privacy exception is required for M5.

---

# 36. DETERMINISTIC TEST PLAN

M5 implementation is not closable without focused tests for at least:

## Config/security

1. M5 disabled + zero peers;
2. ordered peer scalar parsing;
3. duplicate expected ID rejected;
4. duplicate endpoint rejected;
5. expected peer ID equal local ID rejected;
6. malformed host:port rejected;
7. public IP rejected;
8. loopback rejected;
9. RFC1918 allowed;
10. CGNAT/Tailscale range allowed;
11. IPv6 ULA allowed;
12. hostname/DNS input rejected;
13. no path/query/userinfo syntax accepted.

## Peer HTTP client

14. GET method only;
15. path fixed to `/api/state`;
16. 1500 ms timeout;
17. redirects not followed;
18. non-2xx isolated;
19. 256 KiB limit enforced without trusting Content-Length;
20. response body not logged on failure;
21. request cancellation stops active poll.

## Payload validation

22. valid PublicState accepted;
23. malformed JSON rejected;
24. wrong `stateKind` rejected;
25. unsupported schema rejected;
26. missing/empty host identity rejected;
27. expected/observed host mismatch degraded;
28. duplicate runtime Host ID conflict degraded/no collapse;
29. duplicate task ID rejected;
30. duplicate agent ID rejected;
31. future timestamp <=2m tolerated;
32. future timestamp >2m does not replace last-good;
33. old snapshot becomes stale;
34. >30m source snapshot not promoted.

## Poll/runtime isolation

35. first poll asynchronous and server remains available;
36. one peer poll never overlaps itself;
37. slow peer times out without blocking others;
38. one failed peer does not mutate another peer;
39. recovery replaces last-good atomically;
40. Close cancels loops and waits;
41. no goroutine accumulation across repeated timeouts.

## Aggregation/store

42. local state comes from direct Store projection, not HTTP;
43. remote state never enters local InternalRootState;
44. DashboardState local-first/config-order deterministic;
45. `(host.id, task.id)` composite keys do not collide;
46. `(host.id, agent.id)` composite keys do not collide;
47. deep-copy snapshot prevents alias mutation;
48. `/api/state` remains one local host on aggregator;
49. `/api/dashboard` rejects/never nests dashboard responses;
50. failure of remote System source does not erase valid remote Tasks;
51. failure of remote polling does not erase local System/Network/Tasks.

## Last-good/freshness

52. failed poll retains previous accepted snapshot stale;
53. LastAttempt advances on failure;
54. LastSuccess does not advance on failure;
55. retained content expires at 30m;
56. configured host card remains after content expiration;
57. never-successful peer has no fabricated host domain state.

## UI/mock/privacy

58. exactly two deterministic mock hosts;
59. distinct System/Network values;
60. attention includes host identity;
61. completion includes host identity;
62. stale peer clearly labeled;
63. host order stable across refreshes;
64. desktop does not expose peer endpoint/IP;
65. desktop does not expose raw transport errors;
66. Kindle regression remains local-host-only and template behavior unchanged;
67. no M6 Browser Watch runtime added;
68. no M7 Quota aggregation added;
69. no control/navigation action added;
70. no Process Groups revival.

Full implementation validation should include normal tests, race tests, vet, diff/scope/privacy audit, and real two-Mac acceptance.

---

# 37. REAL TWO-MAC ACCEPTANCE PLAN

M5 closure requires future validation using **two real Macs** on the trusted configured LAN/VPN.

Example identities:

- Mac A: `host.id = mac-mini`;
- Mac B: `host.id = macbook`.

At least one node runs the aggregate `/display`; both continue running their own local System/Network/M4 sources.

Acceptance scenarios:

1. **Both online** — aggregator shows both hosts with distinct System, Network, and task state.
2. **Remote starts after aggregator** — configured remote host begins unknown/unavailable, then becomes available without restarting aggregator.
3. **Remote stops** — only that peer becomes unavailable; local remains live.
4. **Peer recovers** — same configured host returns available and atomically refreshes last-good snapshot.
5. **Stale last-good** — remote data remains visibly stale during bounded outage instead of disappearing.
6. **Wrong expected host ID** — response is degraded/rejected; no relabel/merge.
7. **Duplicate host ID** — explicit conflict; no collapse into one host.
8. **Slow peer timeout** — controlled slow peer crosses 1500 ms; unrelated host remains responsive.
9. **Malformed response** — controlled fake peer returns malformed/oversized/wrong-state payload; only that peer degrades.
10. **Remote M4 active task** — active remote Codex/Claude task appears under correct host.
11. **Remote attention** — approval/question/elicitation attention appears with host identity.
12. **Remote completion** — retained bounded completion appears under correct host.
13. **Local + remote tasks simultaneously** — no identity collision or state erasure.
14. **Local System/Network refresh** — local metrics continue updating while remote polling succeeds/fails.
15. **No cross-host state erasure** — repeated remote failures/recoveries never overwrite local or another peer's state.
16. **Privacy** — inspect `/api/dashboard` and `/display`; no InternalState, cwd, raw prompt/final/tool payload, peer endpoint, credentials, or raw errors.
17. **Restart** — aggregator restarts with configured peers; server is immediately usable and peers repopulate asynchronously.
18. **Clean shutdown** — peer request contexts/timers/goroutines terminate cleanly.

Real acceptance contains **no control actions**.

---

# 38. EXPLICIT M5 SCOPE BOUNDARY

M5 includes only:

- explicit trusted peer configuration;
- remote `GET /api/state` polling;
- peer source health;
- bounded last-good state;
- separate DashboardState;
- `/api/dashboard`;
- multi-host default `/display` hierarchy;
- deterministic multi-host mock;
- tests and real two-Mac validation.

M5 explicitly excludes:

- M6 Browser AI Watch;
- M7 Quota collection/account aggregation;
- M8 final responsive/display-density closure;
- Kindle multi-host redesign;
- Process Groups;
- remote approve/deny;
- answering provider questions;
- stop/retry/continue;
- Safe Navigation activation;
- generic remote control;
- arbitrary command execution;
- arbitrary URL proxying;
- public-Internet monitoring guarantee;
- account/auth product;
- custom TLS/PKI;
- mDNS/Bonjour discovery;
- push/event streaming;
- WebSockets for M5 peer transport;
- message queue;
- central database/event sourcing;
- historical metrics replication;
- distributed consensus.

---

# 39. FAILURE ISOLATION INVARIANTS

The implementation must preserve these exact outcomes:

### Mac A available / Mac B unavailable

Mac A remains fully valid. Mac B shows peer unavailable and, if available, stale last-good state.

### Mac A System source degraded / Mac A Tasks available

Valid Mac A tasks remain visible. A host-local System source failure does not invalidate unrelated host-local task facts.

### Mac B polling fails

Mac A local state and any other peer state remain unchanged.

### Malformed peer

Only that peer degrades. No process crash and no aggregate-wide invalidation.

### Duplicate Host ID

Explicit conflict/degraded state. No merge/collapse.

### Slow peer

Only that peer hits its 1500 ms timeout. Other host updates/display proceed.

---

# 40. IMPLEMENTATION GUIDANCE — NO NEW FRAMEWORK

The expected implementation can remain standard-library oriented and small:

- `net/http` client;
- `context` cancellation;
- `net/netip` or equivalent IP-range validation;
- `io.LimitReader`-style bounded body reading;
- existing `encoding/json`;
- explicit structs and `RWMutex` snapshot storage;
- existing SSR templates/view-model approach.

No reference project has justified a new runtime dependency.

---

# 41. MATERIAL DECISION REVIEW

**UNRESOLVED_MATERIAL_DECISIONS: NONE.**

The audit found no requirement to:

- break `/api/state` semantics;
- add an auth/account product for the trusted LAN/VPN MVP;
- redesign Kindle before M8;
- change PublicState schemaVersion;
- introduce a new user-facing host-identity system.

Existing configurable `host.id` is sufficient when combined with an explicit expected peer ID and collision handling.

---

# 42. FREEZE CONCLUSION

M5 is frozen as:

```text
MULTI-HOST READ-ONLY DASHBOARD

Each DevBoard node
  owns its local InternalState
  exposes one sanitized PublicState at GET /api/state

Aggregator
  includes local PublicState directly
  pulls configured remote /api/state every 5s
  times each peer out at 1500ms
  validates <=256 KiB PublicState
  keeps peer health separately
  keeps last-good state up to 30m
  never merges remote state into local Store

/api/state
  = ONE LOCAL HOST ONLY

/api/dashboard
  = DashboardState(local + peer snapshots)

/display
  = server-side multi-host view

/display/kindle
  = existing local-host-only appliance until M8

Trust
  = explicitly configured private LAN/VPN peers
  = IP literal + port only
  = no redirects
  = no arbitrary URL fetching

Control
  = NONE
```

**M5 MULTI-HOST READ-ONLY DASHBOARD TECHNICAL CONTRACT V1 FROZEN.**

**M5 RUNTIME IMPLEMENTATION NOT STARTED.**
