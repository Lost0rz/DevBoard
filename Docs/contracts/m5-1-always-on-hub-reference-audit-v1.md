# DevBoard M5.1 Always-On NAS Hub — Delta Reference Audit V1

> Date: 2026-08-22
> Current M5 implementation base: `codex/m5-multi-host` @ `dbc3861266a9bcedde03b4477bca323d5efb37c6`
> Historical M5 contract: `Docs/contracts/m5-multi-host-v1.md` @ `279cff16e39f19d82dffd0a4d4c645037744b291`
> Status: **DELTA REFERENCE AUDIT COMPLETE**
> Scope: M5.1 topology/runtime-role delta only. No runtime implementation in this commit.

## 1. Why M5 topology is reopened

The first M5 implementation proved the peer polling, validation, last-good, privacy and host-scoped dashboard mechanisms, but its deployment topology makes the aggregating Mac a single availability dependency:

```text
Mac A = monitored host + aggregator
Mac B = peer
```

If Mac A is powered off, the dashboard disappears even if Mac B is still active.

The user-confirmed M5.1 production topology is therefore:

```text
Mac A NODE ─┐
Mac B NODE ─┼─ GET /api/state pull ─→ always-on NAS HUB
future NODE ┘                         ↓
                                  DashboardState
                                  ↙          ↘
                           /api/dashboard   /display
```

The NAS is infrastructure, not an AI-work host.

## 2. Current implementation delta

Audit base: `dbc3861266a9bcedde03b4477bca323d5efb37c6`.

### Reusable unchanged or nearly unchanged

- `PublicState` remains the sanitized one-node transport boundary.
- peer configuration remains explicit and ordered.
- `PeerSnapshotStore` already provides independent per-peer records, deep-copy snapshots, last-good retention and deterministic order.
- peer HTTP polling already uses fixed GET `/api/state`, 1500 ms timeout, redirect denial and a 256 KiB body bound.
- peer payload validation already requires `stateKind=public`, schema v1, expected host identity, bounded timestamps, and unique task/agent IDs.
- RFC1918 / CGNAT / IPv6 ULA restrictions, no DNS and no arbitrary URL remain suitable.
- `/api/dashboard` and the multi-host desktop view already understand host wrappers.
- Kindle remains separately local-only under the historical M5 contract.

### Topology-specific code that must change

Current `cmd/devboard/main.go` always constructs a local `state.Store`, starts local System/Network collection, starts local agent ingestion in non-mock mode, then optionally also starts peer polling. This is the combined node+aggregator model and is not valid HUB authority.

Current `PeerSnapshotStore.Dashboard(local, now)` always prepends the local host. M5.1 needs a peer-only dashboard assembly path for HUB.

Current `web.Server` assumes a local Store exists, `/api/state` always projects it, `/display/kindle` always projects it, and `/api/dashboard` always includes it. Server routing must become role-aware.

Current peer cadence is five seconds. M5.1 changes the default HUB pull cadence to one second while preserving the 1500 ms request timeout and per-peer non-overlap.

Current normal `/display` is SSR only and has no automatic refresh. One-second Hub polling would therefore not make an already-open page live. M5.1 needs a minimal automatic full-page refresh.

## 3. Explicit runtime roles

The smallest clear role model is:

```text
NODE
HUB
```

Default = NODE.

This preserves existing single-Mac behavior and prevents the production topology from silently remaining node+aggregator.

### NODE

Owns exactly one monitored Mac:

- local `state.Store`;
- System collector;
- Network collector;
- Codex/Claude lifecycle ingestion and maintenance;
- local PublicState projection;
- GET `/api/state`;
- local `/display` and `/display/kindle` compatibility.

NODE does not poll peers.

### HUB

Owns aggregation infrastructure only:

- `PeerSnapshotStore`;
- per-peer pull loops;
- `DashboardState`;
- GET `/api/dashboard`;
- normal `/display`.

HUB does not create a monitored NAS `PublicState`, does not run Mac collectors, and does not own Codex/Claude monitored task state.

## 4. Hub `/api/state`

A HUB has no local monitored PublicState. Fabricating one would violate source authority and could make a Hub accidentally acceptable as a peer.

Decision: in HUB role, GET `/api/state` returns **404 Not Found** with a small bounded non-sensitive response.

The peer collector still accepts only HTTP 200 plus `stateKind=public`, so a Hub accidentally configured as a peer is structurally unusable.

## 5. Hub dashboard assembly

HUB dashboard host order is exactly configured peer order. No NAS wrapper is inserted.

```text
0 peers → hosts=[]
1 peer  → hosts=[Mac A]
2 peers → hosts=[Mac A, Mac B]
```

Configured-but-never-observed peers remain visible as wrappers with no fabricated domain facts, as in M5.

## 6. Polling delta

Decision:

- default peer interval: **1 second**;
- request timeout: **1500 ms**, unchanged;
- maximum in-flight: one request per peer, unchanged;
- initial poll: immediate/asynchronous;
- different peers: independent loops;
- shutdown: context cancellation plus completion barrier.

The existing poll-loop shape already prevents same-peer overlap because it waits for each request to finish before starting its cadence timer. M5.1 changes cadence, not architecture.

No event stream, push telemetry, WebSocket or broker is justified.

## 7. Display freshness delta

Current `/display` has no refresh mechanism.

Decision: keep initial SSR and add a simple full-page refresh for the normal dashboard with **2-second default interval**.

Why 2 seconds rather than 1 second: a full document reload every second is unnecessarily disruptive for scrolling and inspection. Combined with the one-second Hub poll, a healthy LAN normally exposes a node change in roughly 1–2 seconds and is expected to remain approximately within three seconds without claiming realtime guarantees.

No frontend framework, client state, SSE or WebSocket is introduced.

Kindle refresh semantics are not reused for the Hub dashboard.

## 8. Kindle delta

NODE: existing `/display/kindle` behavior remains unchanged.

HUB: `/display/kindle` returns **404 Not Found**.

This is less misleading than choosing one arbitrary peer or inventing an incomplete multi-host Kindle experience. M8 remains authority for multi-host Kindle adaptation.

## 9. Configuration delta

Add one scalar section/key in the existing parser style:

```yaml
runtime:
  role: node
```

Allowed values: `node`, `hub`; default `node`.

`multi_host.peers` remains the ordered peer list for HUB.

Historical `multi_host.enabled` is no longer production authority. M5.1 should either remove it or accept only the non-ambiguous disabled/default form; it must not silently reactivate combined node+hub production behavior.

NODE validation requires a valid monitored `host.id` and existing local collector config.

HUB validation must not require a fake monitored host identity. Host fields may remain present in a shared config file but have no Dashboard host authority in HUB role.

Add a small normal-dashboard refresh scalar only if needed by implementation; default is 2 seconds and the safe M5.1 range is 1–2 seconds.

## 10. Docker/NAS delta

The HUB is intentionally stateless for M5.1:

- one DevBoard binary/image;
- role selected by config;
- no database;
- no persistent state volume;
- last-good in memory only;
- restart repopulates by polling configured NODEs;
- config mounted read-only where practical;
- one exposed HTTP port;
- no Docker socket;
- no privileged container;
- no host PID namespace;
- no broad NAS filesystem mount.

Target build architectures are `linux/amd64` and `linux/arm64`.

Static code/dependency inspection does not identify an M5.1-specific reason those targets should be impossible, but actual `CGO_ENABLED=0` cross-builds remain an implementation validation gate and must not be reported PASS until executed.

## 11. Shutdown delta

The old peer runtime already supports cancellation and wait. Container deployment adds a process-level requirement: the executable must handle SIGTERM/SIGINT by calling HTTP shutdown and closing role-specific runtimes so Docker/NAS stop is graceful.

This is a narrow lifecycle correction, not a daemon framework.

## 12. External access boundary

Unchanged security principle, made deployment-explicit:

- DevBoard Hub is a trusted LAN/private-overlay service.
- direct public Internet exposure is unsupported.
- external access terminates at deployment-layer protection such as Tailscale/private overlay or an authenticated reverse proxy/access gateway.
- Hub remains plain HTTP behind that boundary for MVP.

No account DB, password system, session layer, OAuth, certificate management or Cloudflare-specific runtime belongs in M5.1.

## 13. Reused reference evidence

M5.1 does not repeat the broad M5 reference survey. It reuses the exact pinned revisions and only the topology-relevant conclusions from `m5-multi-host-reference-audit-v1.md`.

### Beszel

- revision: `f1e5797c76a234b1c59a31c815f07edfbee0b0e9`
- license: MIT
- reused lesson: explicit Hub/Agent separation, stable system identities, cancellable lifecycle, independent node status.
- still rejected: PocketBase/database authority, dedicated Beszel agent protocol, SSH and WebSocket complexity.

### Netdata

- revision: `be8941f9270bf8917f194ae319d2911d3323bd7b`
- license: GPL-3.0
- reused lesson: central parent visibility does not need to become local collection authority; node identity and local independence remain first-class.
- still rejected: streaming, replication, historical central storage and custom protocol.

### Prometheus

- revision: `d15adb9ad7e5d9fbde3a9a8f30200593a5a14d86`
- license: Apache-2.0
- inherited lesson: pull collection, independent per-target health, explicit interval/timeout.
- no Prometheus dependency, scrape subsystem or TSDB is introduced.

### Glances

- revision: `3bda428beca0f62993f7c1b79f2e886ea8334678`
- license: LGPL-3.0-only / GNU LGPL v3
- inherited lesson: explicit server lists, timeout/failure isolation and distrust of automatic discovery.
- no source copied and no Glances runtime dependency.

No upstream source is copied into DevBoard by M5.1.

## 14. Acceptance delta

M5.1 changes deployment acceptance sequencing, not final multi-host product requirements.

Phase 1:

```text
NAS HUB + Mac A NODE
```

Validate Hub startup, absence of NAS host card, one-second pulls, task/completion propagation, stale/recovery, privacy, Hub restart repopulation and automatic page refresh.

Result may become `M5_HUB_MAC_A_ACCEPTANCE=PASS`, but M5 closure remains pending.

Phase 2 later adds Mac B using the same NODE pattern and no new architecture. Final M5 closure still requires Mac A + Mac B simultaneously visible through the NAS Hub.

## 15. Delta conclusion

The current M5 transport/store/security mechanisms are reusable. The topology defect is localized to runtime authority, local-host inclusion, route behavior, cadence, browser refresh and deployment packaging.

No material unresolved product or architecture decision remains after this delta audit.

```text
UNRESOLVED_MATERIAL_DECISIONS = NONE
M5.1_DELTA_REFERENCE_AUDIT = COMPLETE
```
