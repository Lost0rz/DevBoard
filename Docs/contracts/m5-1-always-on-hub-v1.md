# DevBoard M5.1 Always-On NAS Hub — Technical Contract V1

> Date: 2026-08-22
> Implementation base: `codex/m5-multi-host` @ `dbc3861266a9bcedde03b4477bca323d5efb37c6`
> Historical M5 contract: `Docs/contracts/m5-multi-host-v1.md`
> Delta audit: `Docs/contracts/m5-1-always-on-hub-reference-audit-v1.md`
> Parent business contracts: `mvp-monitoring-v1.md`, `mvp-feature-freeze-v1.md`
> Status: **TECHNICAL CONTRACT FROZEN**
> Scope: M5.1 topology/runtime refactor only. No M6, M7, control, public-auth product or final M8 device redesign.

## 0. Supersession scope

`m5-multi-host-v1.md` remains immutable historical evidence of the first M5 topology.

M5.1 supersedes only the old contract sections whose meaning depended on a monitored Mac also acting as aggregator:

- topology;
- local-host inclusion in aggregate state;
- runtime role/lifecycle;
- peer polling cadence;
- role-aware route behavior;
- normal dashboard refresh;
- deployment/acceptance sequence.

All compatible M5 peer validation, privacy, SSRF, retention, failure-isolation, host-identity and data-boundary semantics remain inherited unless this document explicitly changes them.

M4 remains `BLOCKED_EXTERNAL_PROVIDER`; M5.1 does not change M4 semantics or status.

## 1. Production topology

M5.1 freezes the production topology as:

```text
Mac A NODE ─┐
            │
Mac B NODE ─┼─ fixed GET /api/state ─→ NAS HUB
            │                          ↓
future NODE ┘                    DashboardState
                                  ↙         ↘
                         /api/dashboard   /display
                                  ↓
                         iPad / phone / browser
```

The HUB pulls NODE PublicState. NODEs do not push.

The NAS Hub is infrastructure, not a monitored AI-work host.

## 2. Runtime roles

Exactly two documented production roles exist:

```text
NODE
HUB
```

Default role: `node`.

NODE and HUB are authority-distinct. M5.1 does not document or endorse combined node+hub operation as the production topology.

### 2.1 NODE authority

NODE owns exactly one monitored machine and may run:

- local `state.Store`;
- local System collector;
- local Network collector;
- local Codex/Claude lifecycle ingestion;
- local reducer maintenance;
- local sanitized PublicState projection.

NODE exposes:

- `/health`;
- `/api/state`;
- local `/display`;
- `/display/kindle`.

NODE does not create `PeerSnapshotStore`, does not poll peers and does not depend on HUB availability. If the NAS Hub is stopped, local collection and Codex/Claude monitoring continue.

### 2.2 HUB authority

HUB owns only aggregation infrastructure:

- configured peer identities/endpoints privately;
- `PeerSnapshotStore`;
- peer transport attempt/success/status metadata;
- peer last-good retention;
- `DashboardState` assembly;
- `/api/dashboard`;
- multi-node `/display`.

HUB MUST NOT create or imply a monitored NAS host.

HUB MUST NOT run:

- Mac System collector;
- Mac Network collector;
- Codex/Claude ingest server;
- local task reducer/maintenance;
- node-local monitored state authority.

## 3. Role configuration

M5.1 extends the current scalar config parser minimally:

```yaml
runtime:
  role: node
```

Allowed values:

- `node`;
- `hub`.

Default: `node`.

No YAML framework is introduced.

### NODE config

NODE uses existing local fields:

```yaml
runtime:
  role: node
server:
  host: "127.0.0.1"
  port: 8787
host:
  id: "mac-a"
  display_name: "Mac A"
```

Existing display/agent/network local configuration remains applicable.

### HUB config

Example:

```yaml
runtime:
  role: hub
server:
  host: "0.0.0.0"
  port: 8787
multi_host:
  peers: mac-a=192.168.1.50:8787,mac-b=192.168.1.51:8787
```

HUB must not require a fake monitored `host.id`. Shared host fields may remain syntactically present but have no dashboard authority in HUB role.

Historical `multi_host.enabled` no longer selects production aggregation behavior. M5.1 must not silently combine NODE authority with HUB polling because `multi_host.enabled=true` appears in an old config. Implementation may retain parser compatibility only if it fails safe and does not re-enable combined production topology.

## 4. NODE `/api/state`

NODE:

```text
GET /api/state
→ 200
→ Content-Type: application/json
→ Cache-Control: no-store
→ schemaVersion=1
→ stateKind="public"
→ exactly one local monitored host
```

Existing PublicState privacy projection remains authority.

## 5. HUB `/api/state`

A HUB has no local monitored PublicState.

Therefore:

```text
GET /api/state
→ 404 Not Found
```

Response is bounded and non-sensitive.

HUB MUST NOT fabricate NAS Host/System/Network/Task state merely to satisfy the old one-host API assumption.

Because peer polling requires HTTP 200 + `stateKind="public"`, a HUB accidentally configured as a peer is structurally rejected.

## 6. HUB `/api/dashboard`

HUB:

```text
GET /api/dashboard
→ 200
→ Cache-Control: no-store
→ DashboardState
```

`DashboardState.hosts` contains configured monitored NODE wrappers only, in configured peer order.

Examples:

```text
zero peers → hosts=[]
one peer   → hosts=[Mac A]
two peers  → hosts=[Mac A, Mac B]
```

No implicit local NAS host is inserted.

Configured peers may remain visible without accepted state, but no System/Network/Task facts may be fabricated.

## 7. NODE dashboard/display behavior

For backward compatibility, NODE may retain the existing local `/display` presentation using only its own PublicState.

NODE is not an aggregator and must not show configured remote nodes.

NODE does not need `/api/dashboard` for M5.1 production behavior. If retained internally for compatibility/testing, it must represent local-only state and must not create peer polling authority.

## 8. Peer interface and anti-recursion

The only peer fetch target remains:

```text
http://<configured-ip:port>/api/state
```

Collector method: GET only.

Peer acceptance still requires:

- HTTP 200;
- valid JSON;
- `stateKind="public"`;
- `schemaVersion=1`;
- valid expected host identity;
- required timestamp and uniqueness checks.

The collector never fetches `/api/dashboard`.

A HUB returns 404 for `/api/state`, so Hub-to-Hub recursion is structurally impossible.

## 9. Peer configuration/security inheritance

M5.1 preserves M5 explicit peer enrollment and private target restrictions:

Allowed address classes:

- IPv4 RFC1918 `10/8`, `172.16/12`, `192.168/16`;
- IPv4 CGNAT `100.64/10`;
- IPv6 ULA `fc00::/7`.

Rejected:

- public/global IPs;
- loopback;
- unspecified;
- multicast;
- link-local;
- hostnames/DNS;
- arbitrary schemes, paths, queries, fragments or userinfo.

Also preserved:

- redirects disabled;
- fixed `/api/state` path;
- hard 256 KiB response-body bound;
- no configurable headers/credentials/body/method;
- no discovery.

## 10. Polling cadence

HUB default peer poll interval: **1 second**.

Request timeout: **1500 ms**.

At most one in-flight request exists per peer.

Loop semantics:

```text
poll immediately
→ complete / timeout / cancel
→ atomic peer record update
→ wait 1 second
→ next poll
```

Different peers use independent loops.

No overlap, accumulating goroutines or sub-second event stream is permitted.

The poll interval is fixed at 1 second for M5.1 unless implementation requires a scalar for testability/operations. If made configurable, allowed range is **500–5000 ms**, default 1000 ms; normal product defaults and acceptance use 1000 ms.

## 11. Peer store / last-good

Preserve `PeerSnapshotStore` semantics:

- independent record per configured peer;
- own synchronization;
- deep-copy PublicState on write/read;
- atomic per-peer replacement;
- one peer failure cannot erase another peer;
- failure never partially overwrites accepted state;
- no remote domain state is written into a NODE `state.Store`.

HUB uses a peer-only Dashboard assembly path; it does not prepend local state.

## 12. Peer health

Preserve M5 statuses:

```text
unknown
available
degraded
unavailable
```

Preserve bounded public diagnostics and private raw transport details.

`LastAttemptAt` and `LastSuccessAt` use Hub clock.

## 13. Freshness / retention / clock authority

Unchanged from M5:

- fresh requires Hub `LastSuccessAt` age <=15 seconds and remote `generatedAt` age <=30 seconds;
- retained stale snapshots remain visible after later peer failure;
- last-good retention is 30 minutes from Hub `LastSuccessAt`;
- after retention expiry, remote PublicState is discarded but configured wrapper remains;
- remote `generatedAt` up to +2 minutes future is tolerated;
- beyond +2 minutes, current response is rejected/degraded and does not advance `LastSuccessAt`;
- >30 seconds and <=30 minutes old may be accepted stale/degraded;
- >30 minutes old is not promoted as new last-good;
- negative presentation ages are clamped to zero only for display.

## 14. Host identity / collisions

Preserve:

```text
configured expected_host_id == remote PublicState.host.id
```

Mismatch rejects current response and does not relabel it.

Two configured peers asserting the same observed host identity are a conflict. Conflicting data does not enter aggregate state.

In HUB role there is no local monitored host identity to collide with. NODE role performs no peer acceptance.

Cross-host presentation identities remain:

```text
(host.id, task.id)
(host.id, agent.id)
```

## 15. Normal `/display` live refresh

Initial `/display` remains SSR.

HUB normal dashboard adds a simple automatic **full-page refresh every 2 seconds**.

The response keeps no-cache/no-store behavior.

No frontend framework, WebSocket, SSE, event stream or complex client-side state is introduced.

Operational latency budget:

```text
NODE event available in PublicState
→ HUB sees it on next ~1s poll
→ page sees it on next <=2s refresh
```

Healthy LAN target: typically about 1–2 seconds, approximately bounded within about 3 seconds. This is an operational target, not a hard realtime guarantee.

The refresh interval may be represented by `display.dashboard_refresh_seconds`; if configurable, M5.1 allows 1–2 seconds and defaults to 2.

## 16. Kindle

NODE `/display/kindle`: unchanged existing behavior and refresh semantics.

HUB `/display/kindle`:

```text
404 Not Found
```

HUB must not silently render one arbitrary peer as the entire dashboard.

M8 still owns multi-host Kindle adaptation.

## 17. Mock

Mock never performs outbound peer polling.

NODE mock preserves deterministic existing local semantics.

HUB mock contains **synthetic peer hosts only**, no NAS local host. It must exercise at least two distinguishable synthetic peer wrappers and retain host-scoped task/status behavior without endpoint leakage.

## 18. NAS/Docker deployment

M5.1 HUB must be deployable as a small stateless Docker service.

Requirements:

- one DevBoard binary/image;
- `runtime.role: hub` selected by mounted config;
- one exposed HTTP port;
- config mount read-only where practical;
- no DB;
- no persistent last-good storage;
- restart repopulates by polling NODEs;
- no Docker socket;
- no privileged mode requirement;
- no host PID namespace;
- no broad NAS filesystem mount;
- no peer addresses baked into image.

Target Linux architectures:

```text
linux/amd64
linux/arm64
```

Validation must attempt `CGO_ENABLED=0` builds for both. A concrete cross-build failure is a blocker to that architecture and must be reported, not hidden.

## 19. Process signals / shutdown

The production executable must support graceful SIGTERM/SIGINT shutdown suitable for Docker/NAS lifecycle.

Shutdown must:

1. stop accepting/serving HTTP cleanly;
2. cancel active Hub peer requests;
3. stop peer timers;
4. wait for peer loops;
5. stop role-specific NODE runtimes when in NODE role.

No background peer goroutines may be intentionally abandoned.

## 20. External access boundary

DevBoard Hub itself remains:

```text
TRUSTED LAN / PRIVATE OVERLAY SERVICE
```

Direct public Internet exposure is unsupported.

External access must be protected outside DevBoard by deployment-layer controls, for example:

- Tailscale/private overlay; or
- authenticated reverse proxy/access gateway.

M5.1 does not implement:

- username/password database;
- sessions;
- OAuth;
- TLS certificate management;
- Cloudflare-specific runtime code.

## 21. Privacy

HUB consumes only already-sanitized NODE PublicState.

Dashboard additions remain limited to safe configured host identity, source kind/status/freshness timestamps, bounded generic source messages and nested sanitized PublicState.

HUB MUST NOT expose:

- peer IP/port;
- raw URL;
- raw network error;
- credentials;
- remote InternalState;
- cwd/worktree root;
- raw provider event;
- prompt/transcript;
- raw final response;
- command/tool payload;
- private correlation ID.

## 22. Phase-1 real acceptance: NAS HUB + Mac A NODE

M5.1 first real deployment acceptance intentionally requires only one Mac NODE.

Required validation:

1. NAS HUB container starts without node-local collectors.
2. Dashboard has no NAS monitored-host card.
3. Mac A exposes exactly one local PublicState.
4. HUB polls Mac A at about 1-second cadence.
5. `/api/dashboard` contains Mac A.
6. `/display` renders Mac A.
7. active Mac A task reaches HUB.
8. completion reaches HUB.
9. Mac A stop retains stale last-good.
10. Mac A recovery requires no Hub restart.
11. endpoint privacy remains intact.
12. Hub restart repopulates Mac A from polling.
13. normal dashboard auto-refresh works.
14. healthy observed latency is acceptable under the M5.1 budget.

If all pass:

```text
M5_HUB_MAC_A_ACCEPTANCE = PASS
```

M5 closure still remains:

```text
M5_CLOSURE = PENDING_SECOND_NODE
```

## 23. Phase-2 second-node acceptance

Later add Mac B using the same NODE binary/config pattern.

Mac B must require no new architectural/runtime feature.

Final M5 closure requires Mac A and Mac B simultaneously visible through the NAS Hub with existing failure isolation/privacy/security semantics intact.

## 24. Explicit non-goals

Not in M5.1:

- M6 Browser AI Watch;
- M7 quota source/account dedupe;
- remote approve/deny;
- remote control;
- arbitrary commands;
- node push telemetry;
- message queue;
- WebSocket/SSE;
- database/event sourcing;
- automatic discovery;
- public Internet authentication product;
- final multi-host Kindle design;
- M8 responsive closure.

## 25. Implementation direction

M5.1 is a minimal refactor of validated M5, not a rewrite.

Primary implementation changes:

1. explicit runtime role;
2. NODE-only local monitored authority;
3. HUB-only peer store/polling authority;
4. peer-only Hub Dashboard assembly;
5. Hub role `/api/state` and Kindle 404 behavior;
6. 1-second poll cadence;
7. 2-second normal-dashboard refresh;
8. graceful process shutdown;
9. minimal Docker/NAS packaging;
10. role-specific deterministic tests.

## 26. Freeze result

```text
M5_1_TECHNICAL_CONTRACT = FROZEN
UNRESOLVED_MATERIAL_DECISIONS = NONE
```

Phase B implementation is authorized only from the exact commit containing this frozen contract.
