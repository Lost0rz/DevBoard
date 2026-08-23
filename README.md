# DevBoard

DevBoard is a local-first development status aggregation and safe-navigation
system. Mac development machines (Nodes) run local collectors and agent
ingestion, project their state through a strict privacy boundary, and push
sanitized snapshots to a central Hub (for example a NAS) that assembles the
multi-node dashboard.

## Current status

- **M5.4 — Node Uplink Runtime: implemented, closure audit in progress.**
  The node-side push runtime (snapshot builder, one-in-flight scheduler,
  session/sequence semantics, retry/backoff, auth/protocol error handling)
  and the hub-side receiver are complete and covered by deterministic
  tests, including the audit remediation batch on this branch.
- **Real Mac A → NAS Hub hardware E2E: PENDING.** The acceptance marker
  `M5_4_MAC_A_NODE_HUB_E2E` is not PASS yet; see the runbook in
  [`Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md`](Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md).
- **M5.5 (Node.app + DMG packaging): NOT STARTED.**

The current authoritative machine contract is
[`Docs/contracts/m5-2-node-hub-ingestion-v1.md`](Docs/contracts/m5-2-node-hub-ingestion-v1.md)
(frozen). The frozen V1 state/runtime/navigation authority remains
[`Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`](Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md).

## Production topology

```text
collectors / hooks (Claude, Codex, System, Network)
        ↓
Node state.Store
        ↓
sanitized PublicState
        ↓
outbound authenticated Node Uplink
        ↓
NAS Hub receiver  (POST /api/node/v1/snapshot)
        ↓
NodeStateStore
        ↓
Dashboard / Web
```

### NODE owns

- local host metrics (System collector);
- network metrics (Network collector);
- local Claude/Codex agent ingestion through the machine-local Unix socket;
- the local `state.Store` and the PublicState projection;
- the Node Uplink runtime (outbound push only).

### HUB owns

- the Node Registry (node ids, display names, per-node tokens);
- the snapshot receiver (`POST /api/node/v1/snapshot`);
- the NodeStateStore (latest accepted state, ordering, liveness, retention);
- the aggregate Dashboard/Web read APIs.

### HUB does NOT

- run Mac collectors;
- fabricate NAS monitored-host state;
- poll Node LAN addresses.

Node identity is a configured `node_id` plus a per-node token — never a LAN
IP. Cross-machine transport is always Node → Hub.

## Build and test

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
```

## Run a Node (Mac)

A Node runs its local collectors and agent ingest independently of the Hub,
then pushes snapshots to the configured Hub address:

```bash
go build -o devboard ./cmd/devboard
./devboard serve --config ./node.yaml
```

Minimal `node.yaml`:

```yaml
runtime:
  role: node
server:
  host: "127.0.0.1"     # local diagnostics surface stays loopback
  port: 8787
host:
  id: "mac-a"           # must equal uplink.node_id
  display_name: "Mac A"
agent:
  stale_after_seconds: 900
network:
  probe_address: "1.1.1.1:443"
  probe_timeout_milliseconds: 1500
uplink:
  enabled: true
  endpoint: "https://hub.example.com"
  node_id: "mac-a"
  token: "<per-node bearer token from the hub registry>"
```

Provider hook helpers (fail-open, zero stdout):

```bash
./devboard agent-hook codex
./devboard agent-hook claude-code
```

Manual provider hook setup is documented in
[`Docs/M2_Agent_Hook_Setup_2026-08-20.md`](Docs/M2_Agent_Hook_Setup_2026-08-20.md).

## Run the Hub (NAS)

The Hub is a stateless latest-state aggregator: registry and accepted
snapshots live in memory, and a Hub restart is repopulated by node
heartbeats:

```bash
./devboard serve --config ./hub.yaml
```

Minimal `hub.yaml`:

```yaml
runtime:
  role: hub
server:
  host: "0.0.0.0"
  port: 8787
display:
  dashboard_refresh_seconds: 2
nodes:
  registered: "mac-a=Mac A=<token>"
```

Generate per-node tokens with `openssl rand -hex 32` (32 random bytes → 64
hex characters). Never commit real tokens and never log them; see
`config.example.yaml` for the full annotated template.

## Transport security

- HTTPS (`https://…`) is the preferred production uplink endpoint — the node
  bearer token must not cross untrusted cleartext transport.
- Explicit `http://…` endpoints are acceptable only for trusted-LAN
  engineering and deterministic testing.
- No real token belongs in the repository, the config example, logs or the
  runbook.

## Real E2E acceptance

The frozen §41 acceptance (16 items, ONLINE/STALE/OFFLINE transitions,
session restart, network interruption, Hub restart repopulation, privacy
grep evidence) is captured step by step in
[`Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md`](Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md).
Its closure marker is **PENDING** until the real run has been executed with
recorded evidence.
