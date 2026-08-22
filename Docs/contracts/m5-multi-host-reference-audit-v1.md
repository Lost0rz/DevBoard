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

---

# A. CURRENT DEVBOARD READINESS

## A.1 Existing node state

Current `InternalRootState` and `PublicState` are intentionally **single-node** state models. They have one top-level `Host`, with that host's Agents, Tasks, System, Network, Sources, and other existing compatibility fields.

The existing `PublicState` is already the sanitized cross-boundary projection. M5 therefore does not need a second remote-private schema and must not transport `InternalState` between machines.

## A.2 Readiness matrix

### Root HostState / PublicHost

**Decision: REUSABLE UNCHANGED as the identity of one node snapshot.**

Each DevBoard node already asserts:

- `host.id`;
- `host.displayName`.

M5 should preserve that meaning instead of converting `PublicState` itself into an aggregate container.

### PublicState shape and privacy allow-list

**Decision: REUSABLE UNCHANGED as the peer transport payload.**

`PublicState` already contains the sanitized representation of the host-scoped domains needed by M5:

- Host;
- System;
- Network;
- Agents;
- Tasks;
- Sources.

Existing compatibility fields may remain present in the per-node payload, but M5 does not promote deferred Process Groups, navigation/control, Browser AI Watch, or Quota into M5 functionality.

### Store snapshot / deep-copy semantics

**Decision: REUSABLE UNCHANGED for local-node authority; ADDITIVE M5 peer snapshot storage is required.**

The existing Store uses `RWMutex`, cloned snapshots, and clone-before/after `Update`. Local System, Network, and Task reducers therefore already have a stable authority boundary.

M5 must not copy remote Agents/Tasks/System/Network into that local Store. Remote snapshots require a separate bounded peer snapshot store so peer polling cannot erase or race local collector state.

### `GET /api/state`

**Decision: REUSABLE UNCHANGED as the remote host interface.**

Current behavior is exactly what M5 needs:

- GET only;
- sanitized `PublicState`;
- one local host;
- JSON;
- `Cache-Control: no-store`.

Keeping `/api/state` local-only also provides the structural anti-recursion boundary required by M5.

### Web server behavior

**Decision: REUSABLE WITH ADDITIVE EXTENSION.**

Current inbound server timeouts are bounded (`ReadHeaderTimeout=5s`, `ReadTimeout=10s`, `WriteTimeout=15s`, `IdleTimeout=60s`). M5 still needs a separate outbound client timeout for peer polling.

`/display` is SSR and is the appropriate M5 multi-host surface. `/display/kindle` remains an old-WebKit SSR appliance and should not be redesigned in M5.

### Mock behavior

**Decision: REUSABLE WITH ADDITIVE M5 DASHBOARD MOCK.**

Current `--mock` correctly avoids live System/Network collectors and agent ingestion. M5 mock must likewise perform **zero outbound peer requests** and create deterministic two-host aggregate fixtures directly.

### System / Network collectors

**Decision: REUSABLE UNCHANGED per host.**

Both current local metric runtimes use a 5-second default sample interval and context-cancellable serialized collection loops. M5 should aggregate their already-sanitized output, not centralize their collection.

### Agent / Task lifecycle and retention

**Decision: REUSABLE UNCHANGED per host.**

M4 already owns lifecycle, attention, checkpoint, completion, and retention semantics. M5 only adds host scope around sanitized M4 output.

### Existing config parser

**Decision: REUSABLE WITH A SMALL ADDITIVE EXTENSION.**

Current configuration is a deliberately small scalar section parser rather than a general YAML implementation. M5 should preserve that simplicity. A scalar ordered peer list is preferable to adding a YAML library or generic URL configuration system.

### Existing Process Groups fields

**Decision: SHOULD NOT BE REUSED for M5 product behavior.**

Legacy compatibility fields may remain in a `PublicState` payload, but M5 must not revive Process Groups in the aggregate UI or runtime.

## A.3 Current readiness conclusion

DevBoard is already well positioned for a thin multi-host layer:

```text
local collectors / task hooks
        ↓
local InternalRootState
        ↓
existing PublicState projection
        ↓
GET /api/state   ← M5 remote boundary
```

The remaining M5 gap is small and custom:

- peer configuration/validation;
- bounded HTTP polling;
- peer source health + last-good retention;
- separate aggregate snapshot state;
- `/api/dashboard`;
- multi-host `/display` SSR view;
- deterministic multi-host mock and tests.

No new agent daemon, database, message broker, or streaming protocol is required.

---

# B. GLANCES

## Reference

- Repository: `https://github.com/nicolargo/glances`
- Audited revision: `3bda428beca0f62993f7c1b79f2e886ea8334678`
- License: LGPL-3.0-only / GNU LGPL v3
- Source copied into DevBoard: **none**

## Files inspected

- `glances/client_browser.py`
- `glances/servers_list.py`
- `docs/api/restful.rst`
- `COPYING`

## Relevant behavior

Glances has a mature client/server and Central Browser model. The server list supports static configured servers and optional dynamic Zeroconf entries. Per-server statistics are updated independently; an existing update thread is not overlapped by another update for that server. Its RPC/REST paths use explicit request timeouts and mark a failed server offline without invalidating unrelated servers.

The current source also contains a useful trust lesson: dynamically discovered entries are treated as untrusted for saved/default credentials, and the REST documentation explicitly warns that unauthenticated network APIs may expose sensitive operational information and discusses DNS-rebinding risk.

## Reuse decision

**BEHAVIORAL REFERENCE ONLY.**

Useful for DevBoard:

- explicit server list as a first-class concept;
- independent per-node polling/failure state;
- bounded request timeouts;
- one in-flight update per node;
- clear distrust boundary around discovery/network sources.

Not adopted:

- Glances plugin model;
- its broad metrics API;
- automatic discovery for M5;
- its authentication implementation;
- its client/server application architecture.

DevBoard already has the narrower sanitized `/api/state` contract, so importing Glances would increase rather than reduce complexity.

---

# C. PROMETHEUS

## Reference

- Repository: `https://github.com/prometheus/prometheus`
- Audited revision: `d15adb9ad7e5d9fbde3a9a8f30200593a5a14d86`
- License: Apache-2.0
- Source copied into DevBoard: **none**

## Files inspected

- `scrape/target.go`
- `scrape/scrape.go`
- `config/config.go` (scrape interval/timeout references)
- related target/scrape tests identified during source search
- `LICENSE`

## Relevant behavior

Prometheus is strong evidence for the behavioral model M5 needs, not for a dependency:

- collection is pull-based;
- health is tracked per target;
- each target retains last scrape time, last error, and scrape duration;
- target health is independently `unknown/up/down` based on the most recent scrape;
- interval and timeout are explicit target/scrape properties;
- one target's failed scrape does not redefine another target's state.

## Reuse decision

**BEHAVIORAL REFERENCE ONLY.**

Adopt the principles:

```text
PULL
+ PER-PEER TIMEOUT
+ PER-PEER LAST ATTEMPT / LAST SUCCESS
+ FAILURE ISOLATION
```

Reject introducing Prometheus itself, metrics exposition, TSDB, service discovery, scrape pools, or metrics relabeling. DevBoard is aggregating a tiny already-normalized JSON state, not constructing a metrics platform.

---

# D. ADDITIONAL MULTI-NODE REFERENCES

## D.1 Netdata

### Reference

- Repository: `https://github.com/netdata/netdata`
- Audited revision: `be8941f9270bf8917f194ae319d2911d3323bd7b`
- License: GPL-3.0
- Source copied into DevBoard: **none**

### Files inspected

- `src/streaming/README.md`
- `LICENSE`

### Relevant behavior

Netdata demonstrates strong first-class node identity and a central view where child nodes continue collecting local data if the parent connection is unavailable. This reinforces DevBoard's rule that local host collection authority must remain independent of the aggregator.

However Netdata's multi-node architecture uses child-to-parent streaming, replication, central retention/storage, and a custom protocol.

### Reuse decision

**BEHAVIORAL REFERENCE ONLY; STREAMING ARCHITECTURE REJECTED.**

Keep:

- explicit node identity;
- source-host boundaries;
- independent local collection during central outages.

Reject:

- push streaming;
- replication;
- custom transport protocol;
- historical central storage;
- parent/child distributed observability machinery.

These solve a much larger problem than M5.

## D.2 Beszel

### Reference

- Repository: `https://github.com/henrygd/beszel`
- Audited revision: `f1e5797c76a234b1c59a31c815f07edfbee0b0e9`
- License: MIT
- Source copied into DevBoard: **none**

### Files inspected

- `internal/hub/systems/system_manager.go`
- `internal/hub/heartbeat/heartbeat.go`
- related Hub/Agent system source paths identified during code search
- `LICENSE`

### Relevant behavior

Beszel provides mature examples of:

- stable configured system IDs;
- explicit `pending/up/down/paused` style node statuses;
- duplicate system ID rejection;
- thread-safe system collections;
- cancellable background lifecycle;
- bounded HTTP timeout in its heartbeat path;
- predictable system list management.

Its actual monitoring architecture is a Hub plus Agent with PocketBase/database state and SSH/WebSocket paths.

### Reuse decision

**BEHAVIORAL REFERENCE ONLY; HUB+AGENT IMPLEMENTATION REJECTED.**

The stable identity/status/lifecycle ideas are useful. The database, custom Agent, SSH/WebSocket transport, and historical storage are unnecessary for DevBoard's two-Mac read-only snapshot MVP.

---

# E. REUSE DECISIONS

| Source | Decision | Narrow lesson used |
| --- | --- | --- |
| Existing DevBoard `/api/state` | **USE DIRECTLY** | sanitized one-host snapshot transport |
| Existing DevBoard Store/PublicState | **USE DIRECTLY / ADDITIVE WRAPPER** | local authority, deep-copy snapshot |
| Glances | **BEHAVIORAL REFERENCE ONLY** | per-node polling, timeout, failure isolation, discovery distrust |
| Prometheus | **BEHAVIORAL REFERENCE ONLY** | pull target health, last attempt/success, bounded timeout |
| Netdata | **BEHAVIORAL REFERENCE ONLY** | host identity and local collection independence |
| Beszel | **BEHAVIORAL REFERENCE ONLY** | stable node IDs/status, duplicate rejection, cancellation |

No upstream source is copied or linked. Therefore no new runtime dependency or attribution obligation is introduced by M5 reference use.

---

# F. LICENSE / PROVENANCE

- Glances @ `3bda428...`: LGPL-3.0-only. No source copied.
- Prometheus @ `d15adb9...`: Apache-2.0. No source copied.
- Netdata @ `be8941f...`: GPL-3.0. No source copied.
- Beszel @ `f1e5797...`: MIT. No source copied.

All references are pinned to immutable commit SHAs. This M5 design uses behavior/architecture evidence only.

---

# G. PULL VS PUSH

**Decision: PULL.**

The aggregator periodically fetches each configured peer's existing `GET /api/state`.

Why:

- the source endpoint already exists;
- it is sanitized and read-only;
- peer failure is naturally isolated;
- no inbound registration protocol is required;
- no new daemon or broker is required;
- polling cadence matches DevBoard's existing 5-second host-health rhythm;
- it is straightforward to retain a last-good snapshot.

Push/event streaming is unnecessary for the M5 business requirement.

---

# H. SERVER VS BROWSER AGGREGATION

**Decision: SERVER-SIDE aggregation.**

Browser-direct fanout is rejected because it would:

- require every client to reach every Mac independently;
- introduce CORS and browser network-policy complexity;
- make failure normalization device-specific;
- conflict with the old Kindle/no-JavaScript constraint;
- duplicate aggregation logic across surfaces.

One DevBoard server request should produce one coherent unified view.

---

# I. DISCOVERY DECISION

**Decision: explicit configured peers only in M5.**

No mDNS/Bonjour/Zeroconf discovery in M5.

This is deliberate:

- two-Mac use case is small;
- deterministic enrollment is easier to audit;
- no accidental host enrollment;
- no discovery daemon or credential/discovery trust problem;
- stable configured order provides stable dashboard order.

Discovery may be reconsidered after M5 if it becomes a real operational need.

---

# J. SECURITY / TRUST

M5 creates a new outbound HTTP boundary, so configuration must not become a generic URL fetcher.

Frozen direction for the implementation contract:

- trusted private LAN/VPN only;
- explicit peer enrollment;
- peer address is an IP literal plus port, not an arbitrary URL;
- fixed `http` scheme for M5 MVP and fixed path `/api/state`;
- no user-configurable path, query, headers, credentials, request body, or method;
- redirects disabled;
- public/global Internet targets rejected;
- loopback/self peers rejected because local state is projected directly;
- permitted address classes limited to private LAN, IPv6 ULA, and CGNAT/Tailscale-style space;
- no public-Internet safety guarantee;
- no M5 account system, shared-secret system, or TLS PKI product.

If the network is not trusted, the supported deployment is a trusted VPN such as Tailscale or an equivalent private network rather than exposing DevBoard directly to the public Internet.

The fact that `/api/state` is sanitized reduces exposure, but project/task/host state remains private operational metadata and must not be treated as public Internet data.

---

# K. REJECTED APPROACHES

## Browser-direct fanout

**REJECT.** CORS/reachability complexity, duplicate client logic, and Kindle incompatibility.

## Push/event stream

**REJECT.** M5 needs current snapshots, not a durable event stream.

## WebSocket mesh

**REJECT.** Adds connection state and protocol complexity without product value for a 5-second status board.

## Message queue

**REJECT.** No durable work-distribution requirement.

## Database / event sourcing

**REJECT.** M5 only needs bounded in-memory current/last-good state.

## Distributed coordination / consensus

**REJECT.** There is no shared-write authority or leader-election problem.

## Automatic arbitrary discovery

**REJECT FOR M5.** Explicit configuration is safer and more deterministic.

## Generic remote-management agent

**REJECT.** Existing DevBoard nodes already expose the required sanitized state. M5 is read-only.

## Polling InternalState

**REJECT.** Violates the existing privacy/authority boundary.

## Recursive aggregated-state polling

**REJECT AND MADE STRUCTURALLY IMPOSSIBLE.** Peers are always fetched from `/api/state`, which remains one-host local state; aggregate output uses a separate endpoint.

## Cloud service requirement

**REJECT.** Two Macs on a trusted LAN/VPN must work without a cloud control plane.

## Netdata-style streaming/replication

**REJECT.** Historical replication and custom push protocols are outside M5.

## Beszel-style Hub + database + dedicated Agent

**REJECT.** Duplicates functionality already present in each DevBoard node and adds persistence/transport machinery M5 does not require.

## Prometheus dependency

**REJECT.** Its target-health semantics are useful evidence; the metrics/TSDB product is not needed.

---

# L. REMAINING CUSTOM CODE

Reference audit leaves a small M5-specific implementation surface:

1. additive peer config parsing/validation;
2. a bounded HTTP peer client with fixed `/api/state`, fixed GET, disabled redirects, response-size limit, timeout, and private-address validation;
3. per-peer serialized polling runtime with cancellation;
4. separate deep-copy peer snapshot store;
5. payload/schema/host-identity validation;
6. peer source-health and bounded last-good retention;
7. `DashboardState` assembly from direct local projection + peer snapshots;
8. `/api/dashboard`;
9. multi-host SSR `/display` view model;
10. deterministic two-host mock and focused failure-isolation/security/privacy tests.

No external runtime dependency is justified by this gap.

---

# M. MANDATORY DESIGN QUESTIONS — ANSWERS

1. **Pull or push?** Pull.
2. **Existing `/api/state` or new M5 source endpoint?** Existing `/api/state`, unchanged, as the one-host source endpoint.
3. **Poll individual hosts or stream events?** Poll individual hosts.
4. **Central aggregator or browser-direct fanout?** Central aggregator.
5. **Server-side aggregation or JavaScript browser aggregation?** Server-side.
6. **Manual host config or discovery?** Explicit manual configuration; discovery deferred.
7. **What happens when one host disappears?** Only that peer source changes state; unrelated hosts remain valid. Last-good content is retained stale for a bounded period.
8. **How long should last-good state remain visible?** 30 minutes after the aggregator's last successful accepted snapshot.
9. **How is stale different from unavailable?** `unavailable` is current peer transport/source reachability; `stale` describes the age/currentness of a retained snapshot. A peer can be transport-unavailable while its last-good snapshot remains visibly stale.
10. **How should duplicate Host IDs behave?** Explicit conflict/degraded state; never merge, overwrite, or silently collapse hosts.
11. **How is aggregation kept read-only?** Peers receive only fixed GET `/api/state`; M5 defines no write/control endpoint and never executes remote actions.
12. **What security assumptions are acceptable for MVP?** Explicitly configured trusted private LAN/VPN peers only; no public-Internet exposure guarantee. Strict outbound target validation and disabled redirects prevent the peer list becoming an arbitrary URL fetcher.

---

# N. REFERENCE AUDIT CONCLUSION

The reference and current-code audit supports one minimal M5 architecture without unresolved product conflict:

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

This preserves source authority and privacy, prevents recursive aggregation structurally, isolates peer failure, and satisfies the frozen two-Mac read-only dashboard requirement without introducing a distributed monitoring platform.

**M5 REFERENCE AUDIT V1 COMPLETE.**
