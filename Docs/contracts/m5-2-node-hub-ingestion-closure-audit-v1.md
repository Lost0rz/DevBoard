# DevBoard M5.2 Node Agent → Hub Ingestion — Closure Audit V1

> Date: 2026-08-22
> Freeze branch: `codex/m5-2-node-hub-ingestion-contract-freeze`
> Implementation base: `0955c9a581234f56e0925925485df5f4d33e90aa`
> Contract commit under review: `1fbb7aa0d475b1ef1aafb5397dfdd193e8c4bb00`
> Wire-example commit under review: `4016d21ee6201f5bd7156841a8a50aafcb47a8e3`
> Status: **PASS**

## 1. Review scope

This audit independently re-checks the frozen M5.2 contract against:

- the approved Node Agent + NAS Hub business-flow design;
- current M5.1 NODE/HUB role separation;
- current local Unix-socket Agent ingestion;
- current `state.Store` behavior;
- current `PublicState` model/projector;
- current M5.1 Hub poller and peer store;
- current Hub Web read routes;
- the new `node-snapshot-v1.example.json`.

No runtime implementation is reviewed here because M5.2 is documentation-only by contract.

## 2. Branch purity

Comparison from implementation base to the freeze branch at the wire-example commit shows exactly three added files and no runtime/source modification:

```text
Docs/contracts/m5-2-node-hub-ingestion-reference-audit-v1.md
Docs/contracts/m5-2-node-hub-ingestion-v1.md
Docs/contracts/node-snapshot-v1.example.json
```

Therefore:

```text
M5_2_RUNTIME_CODE_CHANGED = NO
```

## 3. Business-flow alignment

The contract correctly freezes the approved product direction:

```text
Node Agent = state producer
NAS Hub = center service
Node → Hub push
one generic Node product
node_id + per-node token
local Unix socket
in-memory Hub latest state
state-change wake-up + 1s snapshot heartbeat
Mac A reference-node first
```

The old Hub→Node fixed-IP Pull path is explicitly superseded as production authority.

Result: **PASS**.

## 4. Existing-code reuse boundary

The contract does not reopen already-valid domain layers:

- local `state.Store` remains Node-owned;
- PublicState remains the privacy projection;
- local Agent reducers remain local;
- System/Network collectors remain local;
- Hub remains aggregation-only;
- NAS remains absent as a monitored host;
- DashboardState remains the aggregate read model.

Only transport/enrollment/liveness semantics are replaced.

Result: **PASS**.

## 5. Local integration boundary

The contract correctly preserves the existing Unix Domain Socket rather than introducing a LAN-facing local integration API.

Provider hooks do not learn Hub address/token/retry policy.

Result: **PASS**.

## 6. Wire-contract completeness

`NodeSnapshotV1` freezes:

- envelope version/kind;
- node ID grammar;
- random process-session ID;
- monotonic per-session sequence;
- sentAt/generatedAt equality;
- nested PublicState V1;
- fixed receiver route;
- bearer authorization;
- 256 KiB request bound.

The example JSON is structurally consistent with the frozen envelope and current PublicState field model.

Result: **PASS**.

## 7. Identity/auth review

The receiver identity chain is explicit:

```text
Bearer token
  ↔ enabled registry node
  == envelope.nodeId
  == state.host.id
```

The contract prevents Token A from writing Node B and separates machine credentials from later human Web authentication.

Token leakage is explicitly prohibited from logs, Dashboard, APIs and normal settings readback.

Result: **PASS**.

## 8. Retry/replay/order review

The contract addresses the new risks introduced by Push:

- exact retry uses same tuple/body;
- exact duplicate is idempotent;
- same tuple with different body conflicts;
- lower sequence conflicts;
- new session cannot regress accepted generatedAt;
- previous-session delayed packets cannot overwrite newer accepted state;
- Hub restart intentionally resets in-memory ordering authority;
- 409 causes Node session resynchronization rather than rapid retry looping.

This is materially stronger than relying on wall-clock timestamps alone.

Result: **PASS**.

## 9. Liveness/freshness review

The contract separates connection status from retained state freshness:

```text
ONLINE  <=5s
STALE   >5s and <=30s
OFFLINE >30s
```

Last-good retention remains 30 minutes.

A stale/offline Node may retain last-good state but that state is explicitly stale.

This matches the approved business meaning and avoids presenting old data as live.

Result: **PASS**.

## 10. Scheduling/concurrency review

The contract avoids coupling network calls into each collector/reducer.

It freezes:

```text
state.Store mutation
→ non-blocking/coalescing wake-up
→ latest-state snapshot
→ one in-flight HTTP request
```

Burst changes may coalesce; the transport is snapshot-based rather than a lossless event queue.

Heartbeat remains one fresh snapshot per second in healthy steady state.

Result: **PASS**.

## 11. Failure isolation review

Authentication, schema, ordering, payload-size or network failure for one Node cannot mutate another Node.

Rejected requests do not advance accepted state/liveness.

Hub restart recovery remains heartbeat-driven and database-free.

Result: **PASS**.

## 12. Security boundary review

The contract retains the PublicState privacy boundary and explicitly prohibits raw prompt/transcript/tool/shell/InternalState transport.

Production prefers HTTPS and prohibits silent downgrade; HTTP is restricted to deterministic/test or explicit trusted-LAN engineering acceptance.

Direct public user-auth remains out of scope.

Result: **PASS**.

## 13. Phase-gate review

The contract correctly gates implementation:

```text
M5.3 Hub Receiver
→ Fake Node PASS
→ M5.4 Node Uplink
→ Mac A real E2E
```

M5.4 is explicitly blocked until Fake Node closure, preventing simultaneous debugging of two unverified transport ends.

Result: **PASS**.

## 14. Non-goal review

The contract does not prematurely absorb:

- DMG/Settings UI;
- LaunchAgent packaging closure;
- Mac B onboarding;
- final Dashboard redesign;
- Browser AI Watch;
- quota/account dedupe;
- remote approval/control;
- public user auth;
- DB/history/MQ.

Result: **PASS**.

## 15. Findings

Material blocker findings:

```text
NONE
```

Non-blocking implementation notes:

1. M5.3 should keep auth comparison constant-time and never stringify registry secrets.
2. M5.3 tests should use injected clocks to verify 5s/30s/30m boundaries exactly.
3. M5.3 should retain the last accepted payload digest only as private ingestion metadata.
4. M5.4 store-change notification must be non-blocking and coalescing.
5. Historical Pull code may remain for regression evidence during migration but must not be started by the new production runtime path.

## 16. Closure result

```text
M5_2_REFERENCE_AUDIT = PASS
M5_2_TECHNICAL_CONTRACT = FROZEN
M5_2_WIRE_EXAMPLE = FROZEN
M5_2_RUNTIME_CODE_CHANGED = NO
M5_2_UNRESOLVED_MATERIAL_DECISIONS = NONE
M5_2_CLOSURE = PASS
M5_3_IMPLEMENTATION = AUTHORIZED
M5_4_IMPLEMENTATION = BLOCKED_UNTIL_M5_3_FAKE_NODE_PASS
```
