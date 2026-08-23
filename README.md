# DevBoard

DevBoard is a local-first development status aggregation and safe-navigation
system. Mac development machines (Nodes) run local collectors and agent
ingestion, project their state through a strict privacy boundary, and push
sanitized snapshots to a central Hub (for example a NAS) that assembles the
multi-node dashboard.

## Current status

- **M5.4 — Node Uplink Runtime: CLOSED / PASS.** The node-side push runtime
  (snapshot builder, one-in-flight scheduler, session/sequence semantics,
  retry/backoff, auth/protocol error handling) and the hub-side receiver are
  complete, covered by deterministic tests, and validated on real hardware:
  the full frozen §41 acceptance passed 16/16 on real Mac A → NAS Hub.
  ```text
  M5_4_MAC_A_NODE_HUB_E2E = PASS
  ```
  Evidence: [`Docs/M5_4_REAL_E2E_EVIDENCE_2026-08-23.md`](Docs/M5_4_REAL_E2E_EVIDENCE_2026-08-23.md);
  procedure: [`Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md`](Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md).
- **M5.5A — Dogfood Deployment: CODE READINESS / PASS; real dogfood acceptance pending.**
  The frozen implementation contract and remote CI have passed independent
  core audit. Persistent Mac/NAS dogfood installation is now authorized for
  the real acceptance run; M5.5A does not close until that supervised run is
  independently accepted. Construction remains governed by
  [`Docs/contracts/m5-5a-dogfood-deployment-v1.md`](Docs/contracts/m5-5a-dogfood-deployment-v1.md)
  (`M5_5A_DOGFOOD_DEPLOYMENT_CONTRACT = FROZEN_V1`). See
  [`Docs/M5_5A_DOGFOOD_ONBOARDING_2026-08-23.md`](Docs/M5_5A_DOGFOOD_ONBOARDING_2026-08-23.md).
  ```text
  M5_5A_CODE_READINESS = PASS
  M5_5A_REAL_DOGFOOD_ACCEPTANCE = PENDING
  ```

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

Toolchain prerequisites:

- CI and recommended development toolchain: **Go 1.26.x**.
- `go.mod` retains the Go 1.23 language/module compatibility floor — the
  language version and the compiler/linker used to build are separate
  concerns here.
- Modern macOS builds (macOS 26 closure validation included) must use a
  linker that emits Mach-O `LC_UUID` (Go ≥ 1.24 does this by default); old
  Go 1.23-era binaries fail on current macOS with
  `dyld: missing LC_UUID load command`.

## Run a Node (Mac)

A Node runs its local collectors and agent ingest independently of the Hub,
then pushes snapshots to the configured Hub address:

```bash
go build -o devboard ./cmd/devboard
./devboard serve --config ./node.yaml
```

Minimal `node.yaml` (the config loader does not strip inline comments — keep
`key: value` lines clean and put guidance on separate `#` lines):

```yaml
runtime:
  role: node
server:
  host: "127.0.0.1"
  port: 8787
host:
  id: "mac-a"
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

`host.id` must equal `uplink.node_id`; the server block is the loopback-only
local diagnostics surface.

Provider hook helpers (fail-open, zero stdout):

```bash
./devboard agent-hook codex
./devboard agent-hook claude-code
```

Manual provider hook setup is documented in
[`Docs/M2_Agent_Hook_Setup_2026-08-20.md`](Docs/M2_Agent_Hook_Setup_2026-08-20.md).

For the authorized M5.5A real dogfood acceptance run, use the no-`sudo`
per-user installer:

```bash
deploy/macos/install-node.sh
```

It starts unpaired, preserves an existing private config on upgrade, and
opens `http://127.0.0.1:8787/settings`. Node identity, Hub endpoint, uplink,
and token replacement are managed there; the stored token is never rendered.

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

For the authorized M5.5A real dogfood acceptance run, the canonical NAS
Compose entrypoint is:

```bash
cd deploy/hub
./bootstrap.sh
docker compose up -d --build
```

Hub Admin is exposed as `http://<NAS>:<PORT>/admin` only for an explicitly
trusted-LAN dogfood environment. The admin credential travels over that
transport. Prefer HTTPS or a trusted reverse-proxy TLS termination outside
that controlled LAN, and never expose the raw cleartext Hub Admin port to the
public Internet. The iPad display remains `http://<NAS>:<PORT>/display` for
trusted-LAN dogfood.

## Transport security

- HTTPS (`https://…`) is the preferred production uplink endpoint — the node
  bearer token must not cross untrusted cleartext transport.
- Explicit `http://…` endpoints are acceptable only for trusted-LAN
  engineering and deterministic testing.
- Cleartext Hub Admin is likewise trusted-LAN dogfood only; use HTTPS or a
  trusted TLS-terminating reverse proxy beyond that boundary.
- No real token belongs in the repository, the config example, logs or the
  runbook.

## Real E2E acceptance

The frozen §41 acceptance (16 items, ONLINE/STALE/OFFLINE transitions,
session restart, network interruption, Hub restart repopulation, privacy
grep evidence) is captured step by step in
[`Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md`](Docs/M5_4_REAL_E2E_RUNBOOK_2026-08-23.md).
Its closure marker is **PASS**; the independently accepted real run and its
sanitized evidence are recorded in
[`Docs/M5_4_REAL_E2E_EVIDENCE_2026-08-23.md`](Docs/M5_4_REAL_E2E_EVIDENCE_2026-08-23.md).
