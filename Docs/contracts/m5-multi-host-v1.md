# DevBoard M5 Multi-Host Read-Only Dashboard — Technical Contract V1

> Date: 2026-08-22
> Engineering base: `codex/m4-task-observability` @ `2d4499dae7543667baa781079efd468ef0532c01`
> Parent business contracts: `mvp-monitoring-v1.md`, `mvp-feature-freeze-v1.md`
> Reference audit: `m5-multi-host-reference-audit-v1.md`
> Status: **TECHNICAL CONTRACT FROZEN**
> Scope: M5 only. Contract/documentation branch; runtime implementation is not part of this commit.

## 0. M4 dependency status

M4 implementation and local validation are complete at `2d4499dae7543667baa781079efd468ef0532c01`.

M4 closure remains **BLOCKED_EXTERNAL_PROVIDER** because real Claude completion validation could not be observed while the provider returned HTTP 429. The user explicitly authorized M5 read-only contract work without changing M4 closure status.

M5 must not reinterpret M4 as closed and must not change M4 task semantics.

## 1. TOPOLOGY

M5 freezes a **SERVER-SIDE PULL AGGREGATOR**.

```text
Mac A DevBoard
  GET /api/state ───────────────┐
                                │ bounded GET polling
                                ↓
                         Aggregator DevBoard
                          ├─ local Store → PublicState directly
                          └─ Peer Snapshot Store
                                   ↓
                              DashboardState
                              ↙          ↘
                   GET /api/dashboard    /display
                                ↑
                                │ bounded GET polling
Mac B DevBoard                  │
  GET /api/state ───────────────┘
```

Each DevBoard node remains independently authoritative for its own local collectors and tasks. The aggregator does not become collector-of-record for another Mac.

No push agent, broker, WebSocket mesh, database, event stream, distributed coordination, or cloud service is part of M5.

## 2. LOCAL VS REMOTE AUTHORITY

### Local host

The existing local Store remains authority for local Host, System, Network, Agents, Tasks, Sources, and compatibility fields. The aggregator obtains its own local `PublicState` by calling the existing projector directly over `Store.Snapshot()`.

It MUST NOT HTTP-poll itself.

### Remote host

The remote DevBoard node is authority for the content of its sanitized `PublicState`.

The aggregator is authority only for peer transport/aggregation metadata:

- expected peer identity;
- LastAttemptAt;
- LastSuccessAt;
- peer source status;
- retained snapshot freshness.

The aggregator MUST NOT rewrite remote task/system/network facts as local observations.

## 3. PEER CONFIGURATION

M5 extends the existing scalar config style rather than introducing a general YAML framework.

Frozen config shape:

```yaml
multi_host:
  enabled: false
  peers: macbook=192.168.1.50:8787,mac-mini-2=100.64.0.12:8787
```

`multi_host.enabled`:

- default `false`;
- `false`: no peer runtime;
- `true` with zero peers: valid one-host aggregator.

`multi_host.peers`:

- ordered comma-separated scalar;
- each item is `expected_host_id=ip_literal:port`;
- surrounding whitespace ignored;
- order preserved as dashboard peer order.

`expected_host_id`:

- 1–64 bytes;
- ASCII letters, digits, `.`, `_`, `-` only;
- unique in peer config;
- must not equal local configured `host.id`;
- identifies the operator's expected machine but does not override remote `PublicState.host.id`.

Peer address:

- IP literal plus port only;
- IPv4 `192.168.1.50:8787`;
- IPv6 ULA uses bracketed host-port syntax;
- port 1–65535;
- no scheme/path/query/userinfo/fragment/headers/credentials.

The implementation constructs exactly:

`http://<configured-ip:port>/api/state`

Startup/config validation rejects duplicate expected IDs, duplicate normalized endpoints, expected ID equal local ID, invalid ports/addresses, and disallowed network classes. Invalid config is fail-fast before peer polling begins.

## 4. DISCOVERY POLICY

M5 uses **explicit configured peers only**.

No mDNS, Bonjour, Zeroconf, broadcast/subnet scan, or automatic enrollment. Discovery is deferred unless later operational evidence justifies it.

## 5. LOCAL-HOST INCLUSION

The aggregator always includes itself first.

```text
Store.Snapshot()
→ existing PublicState projection
→ DashboardHostSnapshot
```

There is no localhost HTTP dependency.

Local and remote nodes share the same aggregate host-snapshot shape; source kind records `local` or `peer`.

## 6. HOST IDENTITY / MISMATCH / COLLISION

Remote snapshot acceptance requires:

```text
configured expected_host_id == remote PublicState.host.id
```

Configuration identifies the intended peer; remote `host.id` is source identity asserted by that node. They must agree.

### Mismatch

If they differ:

- peer becomes `degraded`;
- response does not become last-good state;
- prior matching last-good may remain under normal retention;
- bounded identity-mismatch diagnostic is exposed without endpoint details;
- returned machine is never relabeled as the expected machine.

### Duplicate observed IDs

If two peers assert the same host ID, or a peer asserts the local host ID:

- explicit conflict/degraded state;
- conflicting remote response is not accepted;
- snapshots are never merged/collapsed;
- local identity retains local authority;
- no task/agent from rejected conflicting data enters aggregate state.

Runtime collision detection is mandatory even after config duplicate checks.

## 7. API CONTRACT

### `GET /api/state`

**Unchanged.** It always means the sanitized `PublicState` of exactly one local DevBoard node.

It MUST NOT become aggregate state on an aggregator.

Preserved properties:

- GET only;
- `stateKind = "public"`;
- PublicState `schemaVersion = 1`;
- `Cache-Control: no-store`;
- existing PublicState privacy contract.

### `GET /api/dashboard`

M5 adds a separate GET-only aggregate endpoint with `Cache-Control: no-store`.

Top-level frozen shape:

```json
{
  "schemaVersion": 1,
  "stateKind": "dashboard",
  "generatedAt": "2026-08-22T06:00:00Z",
  "hosts": []
}
```

Dashboard schema version is independent from nested PublicState schema even though both begin at 1.

`/api/dashboard` accepts no peer definitions, arbitrary outbound target, request body, or control action.

## 8. ANTI-RECURSION GUARANTEE

Aggregation loops are structurally impossible:

- peer collector fetches only fixed `/api/state`;
- `/api/state` is permanently one local `PublicState`;
- aggregate output exists only at `/api/dashboard` and `/display`;
- payload validation requires `stateKind == "public"`.

Thus if A and B are both aggregators, A polling B obtains only B local state and B polling A obtains only A local state. A `stateKind:"dashboard"` payload is never accepted as a peer snapshot.

## 9. AGGREGATION STATE MODEL

M5 chooses a **separate `DashboardState`** rather than changing PublicState into `hosts[]` or creating ambiguous `root.host + remoteHosts` authority.

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
- SnapshotFreshness?   // only when State exists
- State *PublicState   // exactly one accepted sanitized host snapshot

DashboardHostSource
- Kind                 // local | peer
- Status               // unknown | available | degraded | unavailable
- LastAttemptAt?
- LastSuccessAt?
- Message              // bounded generic diagnostic
```

`ConfiguredHostID` is safe operator-configured identity, never the endpoint.

No peer IP/port/raw URL/raw error/DNS material/credentials are exposed in DashboardState.

For accepted remote state: `ConfiguredHostID == State.Host.ID`.

For never-observed peers, ConfiguredHostID permits a stable host card without fabricating domain facts.

Nested `PublicState` preserves existing privacy, host boundaries, future additive host-scoped fields, and per-node SourceHealth semantics.

## 10. POLLING INTERVAL

Remote peers are polled every **5 seconds**.

This matches current local System and Network default sample cadence and is responsive enough for a persistent status board without high-frequency distributed transport.

## 11. REQUEST TIMEOUT

Each remote GET has a hard **1500 ms** whole-request timeout.

This matches the current default network-probe timeout and prevents one dead peer from blocking dashboard freshness.

## 12. OVERLAP PREVENTION

At most **one in-flight poll per peer**.

```text
poll
→ complete / timeout / cancel
→ atomic peer-state update
→ wait cadence
→ next poll
```

No accumulating goroutines. A slow peer cannot spawn overlapping polls. Random jitter is not required for the two-Mac MVP.

## 13. STARTUP BEHAVIOR

HTTP startup does not wait for peers.

When enabled:

1. local server initializes normally;
2. configured peers immediately exist as `unknown` entries with no fabricated snapshot;
3. polling starts asynchronously;
4. first peer polls occur promptly after runtime start;
5. `/display` and `/api/dashboard` remain usable even if all peers are offline.

No second machine is required for startup.

## 14. SHUTDOWN CANCELLATION

Peer runtime owns a root cancellation context and completion barrier.

Close/shutdown:

- cancels active request contexts;
- stops timers/tickers;
- waits for peer loops;
- leaves no peer goroutines alive.

This follows current System/Network `Close()` semantics. M5 does not require a broad process signal-handling redesign.

## 15. PEER SOURCE HEALTH

M5 peer source state is distinct from the remote host's own System/Network/Agent source health.

Peer status:

- `unknown` — no completed poll;
- `available` — latest response was accepted and current enough;
- `degraded` — peer responded/reached transport but payload is stale, malformed, incompatible, oversized, identity-conflicting, or otherwise not trustworthy as current state;
- `unavailable` — connect/route/timeout/transport/non-success HTTP prevents usable response.

Metadata:

- `LastAttemptAt`: aggregator clock; advances on every attempt result;
- `LastSuccessAt`: advances only for a fully accepted matching snapshot;
- `Message`: bounded generic category;
- raw error: private only.

Safe messages include `Peer snapshot available.`, `Peer unavailable.`, `Peer response invalid.`, `Peer host identity mismatch.`, `Peer snapshot is stale.`

## 16. LAST-GOOD SNAPSHOT

The most recent fully accepted remote PublicState remains visible after later poll failure.

A failed poll does not immediately erase all last-known tasks/health. Retained data must be explicitly labeled stale.

A failed/malformed response never partially overwrites last-good state.

## 17. STALE VS UNAVAILABLE

These are separate dimensions:

- peer source status = can aggregator currently obtain/accept peer data?
- snapshot freshness = how current is retained/displayed state?

Valid combinations include unavailable+stale (offline now, last-good retained), degraded+stale (reachable but stale/invalid current response), available+fresh, and unavailable+no snapshot.

## 18. STALE RETENTION

Last-good retention is **30 minutes from aggregator `LastSuccessAt`**.

Retained state is `fresh` only when both are true:

- `now - LastSuccessAt <= 15 seconds`;
- remote `generatedAt` is no more than 30 seconds old, subject to clock-skew allowance.

Otherwise retained state is `stale`.

After 30 minutes without accepted state:

- discard remote PublicState content;
- keep configured host entry;
- show no invented System/Network/Task facts.

Configured peers disappear only when removed from config.

## 19. CLOCK SKEW / TIMESTAMP AUTHORITY

Remote timestamps remain source-host facts and are not rewritten. Aggregator `LastAttemptAt`, `LastSuccessAt`, and Dashboard `GeneratedAt` use aggregator time.

Remote `generatedAt` up to **2 minutes in the future** is tolerated. Raw timestamp remains unchanged; negative relative ages/elapsed values are clamped to zero for presentation only.

If `generatedAt > now + 2m`:

- peer degrades for clock skew;
- response does not replace last-good;
- LastSuccessAt does not advance.

If remote `generatedAt` is:

- <=30s old: eligible fresh;
- >30s and <=30m old: structurally acceptable but stale and peer degraded;
- >30m old: too old to promote as new last-good.

No distributed clock synchronization subsystem is introduced.

## 20. PAYLOAD VALIDATION

HTTP 200 is insufficient. A response must pass:

1. hard body size limit;
2. valid JSON;
3. `stateKind == "public"`;
4. `schemaVersion == 1`;
5. required Host present;
6. non-empty bounded `host.id`;
7. observed host ID equals expected ID;
8. no runtime collision with local/other accepted peer;
9. required PublicState structure decodes safely;
10. bounded timestamp rules;
11. unique task IDs within snapshot;
12. unique agent IDs within snapshot.

Unknown additive fields within schemaVersion 1 are allowed/ignored. Do not use strict unknown-field rejection that would defeat additive schema evolution.

Malformed payload affects only that peer, never crashes the aggregator, never partially replaces state, and is not logged in full.

## 21. PAYLOAD SIZE LIMIT

Hard response limit: **256 KiB**.

Implementation may reject declared Content-Length above the limit early, but must independently enforce the actual body limit and detect one byte over. Oversize response degrades only that peer and is not logged.

## 22. REDIRECTS

HTTP redirects are **disabled**. A 3xx is not followed and is treated as unusable peer response.

## 23. SSRF BOUNDARY

Allowed peer IP classes:

- IPv4 RFC1918: `10/8`, `172.16/12`, `192.168/16`;
- IPv4 CGNAT `100.64/10` for Tailscale/private overlay use;
- IPv6 ULA `fc00::/7`.

Rejected:

- public/global Internet IPs;
- loopback;
- unspecified;
- multicast;
- link-local in M5;
- hostnames/DNS;
- arbitrary scheme/path/query/userinfo;
- redirects.

IP-literal-only config removes DNS-rebinding behavior from the M5 fetch path. Local state is projected directly, so loopback is unnecessary.

## 24. TRUST / AUTHENTICATION / TLS

M5 MVP trust boundary is **explicitly configured trusted private LAN or VPN only**.

M5 does not claim public-Internet safety and does not add a user/account system, shared-token service, certificate management, or custom TLS/PKI.

M5 host:port transport is fixed HTTP. Where confidentiality/authentication is required, use a trusted private overlay/VPN such as Tailscale or equivalent and bind DevBoard only on the intended interface.

M5 does not automatically widen the current server bind address.

Sanitized `/api/state` still contains private operational metadata. Trusted-network scope is a real product boundary.

Future Internet-facing use requires a separate authenticated/TLS contract.

## 25. HOST-SCOPED DATA

M5 aggregates existing sanitized host-scoped state:

- Host;
- System;
- Network;
- Agents;
- Tasks;
- relevant local Sources;
- passive compatibility fields already inside PublicState.

M5 UI priority is Host/System/Network/Tasks/Agent/source status.

Legacy ProcessGroups may remain structurally present but MUST NOT be revived or featured.

Existing navigation compatibility fields grant no remote action authority; M5 adds no control.

## 26. FUTURE BROWSER WATCH EXTENSION

M6 Browser AI Watch is future host-scoped state. Because each Dashboard host retains a nested sanitized PublicState, a future browser-watch public collection can remain source-host scoped without redesigning M5 transport/identity.

M5 does not define or implement Browser Watch.

## 27. QUOTA BOUNDARY

Quota is provider/account-scoped, not machine capacity.

Current nested PublicState quota compatibility data may remain, but M5 unified host UI MUST NOT show identical account quota on two Macs as two independent capacities.

M5 does not invent quota account identity or deduplication. M7 owns quota source truth, account identity, cross-host dedupe, remaining/reset semantics, and presentation.

## 28. CROSS-HOST TASK / AGENT IDENTITY

Task global presentation key:

```text
(host.id, task.id)
```

Agent global presentation key:

```text
(host.id, agent.id)
```

Nested IDs are unchanged. M5 does not require globally unique task or agent IDs across hosts and never treats equal IDs on different Macs as the same entity.

## 29. HOST ORDERING

Deterministic order:

1. local host first;
2. peers in config order.

Hosts do not reorder by health/activity/poll completion.

A global attention summary may prioritize across hosts, but every item retains host label and host-scoped identity.

## 30. STORE / UPDATE AUTHORITY

M5 adds a **separate PeerSnapshotStore**; it never writes remote domain data into local `state.Store`.

```text
Local collectors/M4 reducer → local state.Store
Peer poll A ─┐
Peer poll B ─┼→ atomic per-peer replacement → PeerSnapshotStore
Peer poll C ─┘
Dashboard = local Store.Snapshot + PeerSnapshotStore.Snapshot
```

PeerSnapshotStore requirements:

- own RWMutex/equivalent;
- key by configured peer identity;
- one atomic record replacement per poll;
- deep-copy PublicState on write/snapshot;
- no shared mutable slices/maps/pointers;
- one peer update cannot erase another;
- failure updates only that peer metadata and retains valid last-good under retention.

No DB/event log.

## 31. MOCK

`--mock` performs **zero outbound peer polling**.

M5 creates deterministic exactly-two-host DashboardState:

- local synthetic host: distinct System/Network, active task, recent completion;
- synthetic remote host: distinct System/Network, task requiring attention, peer source stale/degraded with retained last-good snapshot.

Output is deterministic for fixed request time and independent of network/random discovery/hostname/machine ID.

## 32. DESKTOP INFORMATION HIERARCHY

M5 changes only enough `/display` presentation to make host scope immediately obvious:

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

Every task/problem is attached to a host or explicitly carries its host label.

M4 task hierarchy remains:

```text
PROVIDER · PROJECT / BRANCH
TASK TITLE
LIFECYCLE · ELAPSED
CHECKPOINT
ACTION REQUIRED
COMPLETION
```

M5 is not final M8 responsive/density closure. Desktop may show bounded peer health but never endpoint/IP/raw transport error/private remote state.

## 33. KINDLE BOUNDARY

M5 freezes Kindle as **local-host-only until M8**.

`/api/dashboard` + default `/display` satisfy the M5 unified web state/view. The old-WebKit Kindle Agent Deck has a separately frozen presentation contract, while M8 owns final adapted Kindle/tablet/phone/desktop closure.

M5 implementation MUST NOT redesign `internal/web/templates/kindle.html`.

`/display/kindle` continues to consume local PublicState and existing selection/rotation semantics.

## 34. BACKWARD COMPATIBILITY

M5 preserves:

- M3.1 System;
- M3.2 Network;
- M4 Agent/Task lifecycle and retention;
- local `/api/state`;
- PublicState schemaVersion 1;
- current Kindle behavior;
- PublicState privacy projection;
- `safeNavigationEnabled=false` authority;
- one-host startup with no peer requirement.

When disabled or zero peers:

- `/api/state` unchanged;
- `/api/dashboard` contains local host only;
- `/display` behaves as single-host dashboard;
- `/display/kindle` unchanged;
- disabled mode starts no peer runtime.

## 35. PRIVACY

Remote transport consumes only already-sanitized PublicState.

M5 MUST NOT request/transport remote InternalState, cwd/worktree root, raw provider events, prompts, raw final responses, shell commands, tool payloads, transcripts, private correlation IDs, or credentials.

M5 public additions are limited to safe configured host identity, source kind, bounded peer status/freshness timestamps, bounded generic source message, and nested sanitized PublicState.

M5 MUST NOT publicly expose peer endpoint/IP/port, request URL, raw network error, malformed payload, response headers, or private polling data.

No new privacy exception is required.

## 36. DETERMINISTIC TEST PLAN

Implementation tests must cover at least:

### Config/security

1. disabled + zero peers;
2. ordered scalar parsing;
3. duplicate expected ID rejected;
4. duplicate endpoint rejected;
5. expected ID equal local ID rejected;
6. malformed host:port rejected;
7. public IP rejected;
8. loopback rejected;
9. RFC1918 allowed;
10. CGNAT allowed;
11. IPv6 ULA allowed;
12. hostname/DNS rejected;
13. path/query/userinfo syntax rejected.

### HTTP client

14. GET only;
15. fixed `/api/state`;
16. 1500ms timeout;
17. redirect disabled;
18. non-2xx isolated;
19. 256KiB cap enforced without trusting Content-Length;
20. response body not logged;
21. request cancellation works.

### Payload

22. valid PublicState accepted;
23. malformed JSON rejected;
24. wrong stateKind rejected;
25. unsupported schema rejected;
26. empty host ID rejected;
27. identity mismatch degraded;
28. runtime duplicate Host ID conflict degraded/no collapse;
29. duplicate task ID rejected;
30. duplicate agent ID rejected;
31. future <=2m tolerated;
32. future >2m not last-good;
33. old response stale;
34. >30m source snapshot not promoted.

### Poll/runtime isolation

35. first poll async/server available;
36. no same-peer overlap;
37. slow peer timeout does not block others;
38. failed peer does not mutate another;
39. recovery replaces last-good atomically;
40. Close cancels and waits;
41. repeated timeouts do not accumulate goroutines.

### Aggregation/store

42. local uses direct projection, not HTTP;
43. remote never enters local InternalRootState;
44. local-first/config-order deterministic;
45. host+task key collision-free;
46. host+agent key collision-free;
47. deep-copy prevents alias mutation;
48. aggregator `/api/state` remains local-only;
49. `/api/dashboard` never recursively nests dashboard state;
50. degraded remote System does not erase valid remote Tasks;
51. failed peer does not erase local domains.

### Last-good/freshness

52. failed poll retains stale last-good;
53. LastAttempt advances on failure;
54. LastSuccess does not;
55. content expires at 30m;
56. configured card remains after expiry;
57. never-successful peer has no fabricated domain state.

### UI/mock/privacy/scope

58. exactly two deterministic mock hosts;
59. distinct System/Network;
60. attention carries host identity;
61. completion carries host identity;
62. stale peer visibly labeled;
63. stable host order;
64. no endpoint/IP in desktop/public aggregate;
65. no raw transport errors;
66. Kindle remains local-only/regression intact;
67. no M6 runtime;
68. no M7 quota aggregation;
69. no control/navigation action;
70. no Process Groups revival.

Implementation validation also requires normal tests, race tests, vet, diff/scope/privacy audit, and real two-Mac acceptance.

## 37. REAL TWO-MAC ACCEPTANCE PLAN

Use two real Macs on the configured trusted LAN/VPN, with unique host IDs such as `mac-mini` and `macbook`. Both keep local System/Network/M4 sources active; at least one serves aggregate `/display`.

Validate:

1. both online;
2. remote starts after aggregator;
3. remote stops;
4. peer recovers;
5. stale last-good retained;
6. wrong expected host ID;
7. duplicate host ID;
8. slow peer timeout;
9. malformed/oversized controlled fake peer;
10. remote M4 active task;
11. remote attention;
12. remote completion;
13. local + remote tasks simultaneously;
14. local System/Network continue refreshing;
15. no cross-host state erasure;
16. `/api/dashboard` and `/display` privacy audit;
17. restart with async peer repopulation;
18. clean shutdown/cancellation.

No control action is part of acceptance.

## 38. EXPLICIT M5 SCOPE BOUNDARY

M5 includes only:

- explicit trusted peer config;
- remote GET `/api/state` polling;
- peer source health;
- bounded last-good state;
- separate DashboardState;
- `/api/dashboard`;
- multi-host default `/display` hierarchy;
- deterministic multi-host mock;
- tests and real two-Mac validation.

M5 excludes:

- M6 Browser AI Watch;
- M7 Quota collection/account aggregation;
- M8 final responsive/display-density closure;
- Kindle multi-host redesign;
- Process Groups;
- remote approve/deny/question response/stop/retry/continue;
- Safe Navigation activation;
- generic remote control/command execution;
- arbitrary URL proxying;
- public-Internet monitoring guarantee;
- user/account auth product;
- custom TLS/PKI;
- mDNS/Bonjour;
- push/event streaming;
- M5 WebSocket peer transport;
- message queue;
- central DB/event sourcing;
- historical replication;
- distributed consensus.

## 39. FAILURE ISOLATION INVARIANTS

- Mac A available / Mac B unavailable → Mac A fully valid; Mac B unavailable with stale last-good if retained.
- Mac A System degraded / Mac A Tasks available → valid tasks remain visible.
- Mac B poll fails → local/other peer state unchanged.
- Malformed peer → only that peer degraded; no crash.
- Duplicate Host ID → conflict/degraded; no collapse.
- Slow peer → only that peer hits 1500ms timeout.

## 40. IMPLEMENTATION GUIDANCE — NO NEW FRAMEWORK

Expected implementation remains standard-library oriented:

- `net/http`;
- `context`;
- `net/netip` or equivalent IP validation;
- bounded `io` reader;
- existing `encoding/json`;
- explicit structs and RWMutex snapshot storage;
- existing SSR template/view-model approach.

No audited reference justifies a new runtime dependency.

## 41. MATERIAL DECISION REVIEW

**UNRESOLVED_MATERIAL_DECISIONS: NONE.**

The audit found no need to break `/api/state`, add an auth/account product for trusted LAN/VPN MVP, redesign Kindle before M8, change PublicState schemaVersion, or invent a new user-facing host identity system.

Existing configurable `host.id` is sufficient with explicit expected peer ID and collision handling.

## 42. FREEZE CONCLUSION

```text
MULTI-HOST READ-ONLY DASHBOARD

Each DevBoard node
  owns local InternalState
  exposes one sanitized PublicState at GET /api/state

Aggregator
  includes local PublicState directly
  pulls configured remote /api/state every 5s
  times each peer out at 1500ms
  validates <=256KiB PublicState
  keeps peer health separately
  retains last-good up to 30m
  never merges remote state into local Store

/api/state
  = ONE LOCAL HOST ONLY

/api/dashboard
  = DashboardState(local + peer snapshots)

/display
  = server-side multi-host view

/display/kindle
  = local-host-only until M8

Trust
  = explicit private LAN/VPN peers
  = IP literal + port only
  = no redirects or arbitrary URL fetching

Control
  = NONE
```

**M5 MULTI-HOST READ-ONLY DASHBOARD TECHNICAL CONTRACT V1 FROZEN.**

**M5 RUNTIME IMPLEMENTATION NOT STARTED.**
