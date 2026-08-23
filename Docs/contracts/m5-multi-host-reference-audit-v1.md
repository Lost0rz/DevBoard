# DevBoard M5 Multi-Host Read-Only Dashboard — Reference Audit V1

> Date: 2026-08-22
> Engineering base: `codex/m4-task-observability` @ `2d4499dae7543667baa781079efd468ef0532c01`
> Status: **REFERENCE AUDIT COMPLETE**
> Scope: M5 reference and current-code audit only. No M5 runtime implementation.

## 1. Authority and scope

Product authority remains, in order:

1. `Docs/contracts/mvp-monitoring-v1.md`;
2. `Docs/contracts/mvp-feature-freeze-v1.md`;
3. DevBoard state/privacy and display contracts;
4. existing DevBoard implementation;
5. external references.

M5 is the **Multi-Host Read-Only Dashboard** milestone: at least two monitored Macs must be visible in one unified read-only web state/view with explicit host identity.

This audit does not reopen M4. M4 implementation/local validation is at `2d4499dae7543667baa781079efd468ef0532c01`; M4 closure remains `BLOCKED_EXTERNAL_PROVIDER` only because a real Claude completion was not observable while the provider returned HTTP 429. The user explicitly authorized proceeding with this M5 contract work without representing M4 as closed.

M5 does not implement Browser AI Watch, Quota, control/navigation, remote approve/deny, Process Groups, transcript/process polling, event streaming, or a distributed system.

## A. CURRENT DEVBOARD READINESS

### Existing node state

Current `InternalRootState` and `PublicState` are intentionally single-node state models. They have one top-level `Host`, with that host's Agents, Tasks, System, Network, Sources, and existing compatibility fields.

The existing `PublicState` is already the sanitized cross-boundary projection. M5 does not need a second remote-private schema and must not transport `InternalState` between machines.

### Readiness matrix

- **Root HostState / PublicHost — reusable unchanged.** They remain the identity of exactly one node snapshot.
- **PublicState privacy projection — reusable unchanged.** It already contains sanitized Host/System/Network/Agents/Tasks/Sources needed by M5.
- **Store snapshot/deep-copy — reusable unchanged for local authority.** M5 needs a separate additive peer snapshot store; remote data must not be copied into the local Store.
- **`GET /api/state` — reusable unchanged.** It is GET-only sanitized single-host JSON with `Cache-Control: no-store`, and is the preferred remote host interface.
- **Web server — reusable with additive extension.** Current inbound timeouts are bounded; M5 needs its own outbound peer timeout and aggregate handler/view.
- **Mock — reusable with additive two-host aggregate fixture.** `--mock` must perform zero outbound peer polling.
- **System/Network collectors — reusable unchanged per host.** Both use 5-second local collection cadence and cancellable runtimes.
- **M4 Agent/Task lifecycle and retention — reusable unchanged per host.** M5 only adds host scope.
- **Config parser — reusable with a small scalar extension.** Current config is intentionally small; M5 should not add a generic YAML/URL framework.
- **Process Groups compatibility fields — should not be reused for M5 product behavior.** They may remain passive compatibility data but are not revived.

### Readiness conclusion

```text
local collectors / task hooks
        ↓
local InternalRootState
        ↓
existing PublicState projection
        ↓
GET /api/state   ← M5 remote boundary
```

Remaining custom M5 work is limited to peer config/validation, bounded HTTP polling, per-peer source health and last-good retention, a separate aggregate snapshot model, `/api/dashboard`, multi-host `/display`, deterministic mock, and focused tests.

No new agent daemon, database, broker, or streaming protocol is required.

## B. GLANCES

### Reference

- Repository: `https://github.com/nicolargo/glances`
- Audited revision: `3bda428beca0f62993f7c1b79f2e886ea8334678`
- License: LGPL-3.0-only / GNU LGPL v3
- Source copied into DevBoard: **none**

### Files inspected

- `glances/client_browser.py`
- `glances/servers_list.py`
- `docs/api/restful.rst`
- `COPYING`

### Relevant behavior

Glances has a mature client/server and Central Browser model. Its server list supports static configured servers and optional Zeroconf entries. Per-server statistics are updated independently; a live update thread for one server is not overlapped by another. RPC/REST operations use explicit timeouts and mark a failed node offline without invalidating unrelated nodes.

The source also provides a useful trust lesson: dynamically discovered entries are treated as untrusted for saved credentials, while REST documentation warns that unauthenticated network APIs can expose sensitive operational information and discusses DNS-rebinding risk.

### Decision

**BEHAVIORAL REFERENCE ONLY.**

Use the patterns of explicit node lists, independent timeout/failure state, non-overlap, and discovery distrust. Do not import Glances plugins, broad metrics API, autodiscovery, authentication implementation, or application architecture. DevBoard already has the narrower sanitized `/api/state` contract.

## C. PROMETHEUS

### Reference

- Repository: `https://github.com/prometheus/prometheus`
- Audited revision: `d15adb9ad7e5d9fbde3a9a8f30200593a5a14d86`
- License: Apache-2.0
- Source copied into DevBoard: **none**

### Files inspected

- `scrape/target.go`
- `scrape/scrape.go`
- `config/config.go` around scrape interval/timeout semantics
- related target/scrape tests identified during source search
- `LICENSE`

### Relevant behavior

Prometheus is strong evidence for the M5 behavioral model:

- collection is pull-based;
- health is tracked independently per target;
- each target retains last scrape time, last error, duration, and health;
- interval and timeout are explicit;
- one failed target does not redefine unrelated target state.

### Decision

**BEHAVIORAL REFERENCE ONLY.**

Adopt:

```text
PULL
+ PER-PEER TIMEOUT
+ PER-PEER LAST ATTEMPT / LAST SUCCESS
+ FAILURE ISOLATION
```

Do not introduce Prometheus, a metrics exposition format, scrape pools, relabeling, or TSDB.

## D. ADDITIONAL MULTI-NODE REFERENCES

### D.1 Netdata

- Repository: `https://github.com/netdata/netdata`
- Audited revision: `be8941f9270bf8917f194ae319d2911d3323bd7b`
- License: GPL-3.0
- Files inspected: `src/streaming/README.md`, `LICENSE`
- Source copied: **none**

Netdata demonstrates first-class node identity, centralized visualization, and the useful property that child nodes continue local collection when the parent connection is lost. Its actual multi-node architecture uses push streaming, replication, central retention/storage, and a custom protocol.

**Decision: BEHAVIORAL REFERENCE ONLY; STREAMING ARCHITECTURE REJECTED.** Keep host identity/source boundaries/local independence; reject streaming, replication, custom protocol, and central historical storage.

### D.2 Beszel

- Repository: `https://github.com/henrygd/beszel`
- Audited revision: `f1e5797c76a234b1c59a31c815f07edfbee0b0e9`
- License: MIT
- Files inspected: `internal/hub/systems/system_manager.go`, `internal/hub/heartbeat/heartbeat.go`, related Hub/Agent system paths, `LICENSE`
- Source copied: **none**

Beszel provides mature examples of stable system IDs, explicit node status, duplicate-ID rejection, thread-safe collections, cancellable background lifecycle, bounded HTTP timeout, and predictable system-list management. Its real architecture uses a Hub, PocketBase/database state, a dedicated Agent, SSH, and WebSocket paths.

**Decision: BEHAVIORAL REFERENCE ONLY; HUB+AGENT IMPLEMENTATION REJECTED.** Reuse identity/status/lifecycle lessons only.

## E. REUSE DECISIONS

| Source | Decision | Narrow lesson used |
| --- | --- | --- |
| DevBoard `/api/state` | **USE DIRECTLY** | sanitized one-host snapshot transport |
| DevBoard Store/PublicState | **USE DIRECTLY / ADDITIVE WRAPPER** | local authority, deep-copy snapshot |
| Glances | **BEHAVIORAL REFERENCE ONLY** | node polling, timeout, isolation, discovery distrust |
| Prometheus | **BEHAVIORAL REFERENCE ONLY** | pull target health, attempt/success, timeout |
| Netdata | **BEHAVIORAL REFERENCE ONLY** | host identity and local collection independence |
| Beszel | **BEHAVIORAL REFERENCE ONLY** | stable IDs/status, duplicate rejection, cancellation |

No upstream source is copied or linked; M5 gains no new runtime dependency from these references.

## F. LICENSE / PROVENANCE

- Glances @ `3bda428...`: LGPL-3.0-only; no code copied.
- Prometheus @ `d15adb9...`: Apache-2.0; no code copied.
- Netdata @ `be8941f...`: GPL-3.0; no code copied.
- Beszel @ `f1e5797...`: MIT; no code copied.

All references are pinned to immutable commit SHAs and used only as behavioral/architectural evidence.

## G. PULL VS PUSH

**Decision: PULL.**

The aggregator periodically fetches each configured peer's existing `GET /api/state`. This reuses the existing sanitized boundary, isolates failure naturally, requires no registration protocol or broker, and supports bounded last-good snapshots. Push/event streaming is unnecessary for M5.

## H. SERVER VS BROWSER AGGREGATION

**Decision: SERVER-SIDE aggregation.**

Browser-direct fanout is rejected because it makes every client independently reach every Mac, introduces CORS/browser-policy complexity, duplicates failure normalization, and conflicts with old Kindle/no-JavaScript requirements. One DevBoard server request should produce one coherent unified view.

## I. DISCOVERY DECISION

**Decision: explicit configured peers only.**

No mDNS/Bonjour/Zeroconf, broadcast/subnet scan, or automatic enrollment in M5. Explicit enrollment is deterministic, safer for a two-Mac MVP, prevents accidental hosts, and gives stable display order.

## J. SECURITY / TRUST

M5 peer configuration must not become a generic URL fetcher.

Frozen direction:

- trusted private LAN/VPN only;
- explicit peer enrollment;
- peer address is an IP literal plus port, never an arbitrary URL;
- fixed HTTP scheme and fixed `/api/state` path;
- no configurable path/query/headers/credentials/body/method;
- redirects disabled;
- public/global Internet targets rejected;
- loopback/self peers rejected because local state is projected directly;
- allowed address families limited to RFC1918, IPv6 ULA, and CGNAT/Tailscale-style private overlay space;
- no public-Internet safety guarantee;
- no M5 account/auth system or custom TLS/PKI.

For an untrusted network, use a trusted VPN/private overlay and bind DevBoard only on the intended interface. Sanitized task/project state is still private operational metadata.

## K. REJECTED APPROACHES

- **Browser direct fanout — REJECT.** CORS/reachability duplication and Kindle incompatibility.
- **Push/event stream — REJECT.** Current snapshots are sufficient.
- **WebSocket mesh — REJECT.** Adds connection/protocol state without product value.
- **Message queue — REJECT.** No durable work-distribution need.
- **Database/event sourcing — REJECT.** Bounded in-memory current/last-good state is enough.
- **Distributed coordination/consensus — REJECT.** No shared-write/leader problem exists.
- **Automatic arbitrary discovery — REJECT FOR M5.** Explicit configuration is safer.
- **Generic remote-management agent — REJECT.** Existing DevBoard nodes already expose sanitized state.
- **Polling InternalState — REJECT.** Violates privacy/authority boundaries.
- **Recursive aggregate polling — REJECT STRUCTURALLY.** Peers always fetch local-only `/api/state`; aggregate output uses a separate endpoint.
- **Cloud service requirement — REJECT.** Trusted LAN/VPN must work standalone.
- **Netdata streaming/replication — REJECT.** Historical/custom-protocol machinery is outside M5.
- **Beszel Hub+DB+Agent — REJECT.** Duplicates existing node capabilities and adds unnecessary persistence/transport.
- **Prometheus dependency — REJECT.** Target-health behavior is useful; metrics/TSDB infrastructure is not.

## L. REMAINING CUSTOM CODE

The audit leaves only a small DevBoard-specific implementation surface:

1. additive peer config parsing/validation;
2. bounded HTTP peer client with fixed GET `/api/state`, timeout, redirect disable, response-size cap, and private-address validation;
3. serialized per-peer polling with cancellation;
4. separate deep-copy peer snapshot store;
5. payload/schema/host-identity validation;
6. peer source-health and bounded last-good retention;
7. `DashboardState` assembly from local projection plus peer snapshots;
8. `/api/dashboard`;
9. multi-host SSR `/display` view model;
10. deterministic two-host mock and focused failure/security/privacy tests.

No external runtime dependency is justified.

## M. MANDATORY DESIGN QUESTIONS — ANSWERS

1. **Pull or push?** Pull.
2. **Existing `/api/state` or new M5 source endpoint?** Existing `/api/state`, unchanged, as the one-host source endpoint.
3. **Poll individual hosts or stream events?** Poll individual hosts.
4. **Central aggregator or browser-direct fanout?** Central aggregator.
5. **Server-side or JavaScript aggregation?** Server-side.
6. **Manual config or discovery?** Explicit manual configuration; discovery deferred.
7. **One host disappears?** Only that peer source changes; unrelated hosts remain valid and last-good is retained stale for a bounded period.
8. **How long remains visible?** 30 minutes after aggregator `LastSuccessAt`.
9. **Stale vs unavailable?** Unavailable is current peer transport/source reachability; stale is age/currentness of retained state. Both can be true simultaneously.
10. **Duplicate Host IDs?** Explicit conflict/degraded state; never merge, overwrite, or collapse.
11. **How kept read-only?** Fixed GET `/api/state` only; no write/control endpoint or remote action.
12. **MVP security assumption?** Explicit trusted private LAN/VPN peers, strict outbound target validation, disabled redirects, and no public-Internet exposure guarantee.

## N. REFERENCE AUDIT CONCLUSION

```text
LOCAL DevBoard Store
        ↓ direct PublicState projection
        ┐
        │
peer A ─┼─ GET /api/state ─┐
peer B ─┘                  │
                           ↓
                  bounded Peer Snapshot Store
                           ↓
                     DashboardState
                    ↙             ↘
             /api/dashboard     /display SSR

/api/state remains ONE LOCAL HOST ONLY.
```

This preserves source authority/privacy, prevents recursive aggregation structurally, isolates peer failure, and satisfies the frozen two-Mac read-only requirement without introducing a distributed monitoring platform.

**M5 REFERENCE AUDIT V1 COMPLETE.**
