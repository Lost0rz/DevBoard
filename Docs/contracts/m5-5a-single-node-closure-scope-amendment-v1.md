# DevBoard M5.5A Single-Node Closure Scope Amendment v1

Status: **FROZEN**

Owner: Core auditor / repository governance

Purpose: revise only the real-hardware closure scope of M5.5A so the project first closes a stable, continuously usable **Mac A + NAS + browser UI** dogfood loop before introducing a second physical Mac.

This amendment does **not** replace or weaken the implementation architecture in `m5-5a-dogfood-deployment-v1.md`. It changes only which real-machine acceptance items block M5.5A closure.

## 1. Authorities preserved

The following remain frozen and authoritative:

- `Docs/contracts/m5-5a-dogfood-deployment-v1.md`
- `Docs/contracts/m5-2-node-hub-ingestion-v1.md`
- `Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`

All Node → Hub push, privacy, authentication, restart, Registry reconstruction, Settings/Admin, Docker hardening, and Safe Navigation exclusions remain unchanged.

## 2. Revised M5.5A product outcome

M5.5A closes when one real Mac and the NAS form a reliable daily dogfood system:

```text
Mac A
  per-user LaunchAgent (RunAtLoad + KeepAlive)
      ↓
  DevBoard Node
      ├─ collectors + Claude/Codex ingest
      ├─ loopback /settings
      └─ outbound authenticated Node → Hub uplink

NAS
  canonical Docker Compose (restart: unless-stopped)
      ↓
  DevBoard Hub
      ├─ authenticated /admin
      ├─ /api/dashboard
      └─ /display
```

The required user journey is:

```text
Admin creates/manages Mac A credential
→ Mac A is configured through loopback /settings
→ LaunchAgent supervises the Node continuously
→ Node pushes sanitized state outbound to NAS
→ NAS Hub survives supervised/container restart
→ browser /display shows current Mac A state
```

M5.5A is a usable single-node dogfood closure, not the final visual redesign and not a single-node-only architecture.

## 3. Multi-node capability MUST remain preserved

Deferring the second physical Mac MUST NOT remove or narrow existing multi-node capability.

M5.5A implementation MUST continue to retain:

- Hub Registry support for multiple Node IDs and independent per-node credentials;
- Add/Enable/Disable/Reset Token node-management interfaces;
- NodeStateStore/dashboard structures capable of holding multiple Nodes;
- `/display` and dashboard APIs capable of rendering/representing multiple Nodes;
- Node identity based on configured `node_id` + token, never LAN IP;
- independent outbound Node → Hub ingestion semantics.

No code, schema, API, or UI may be simplified into a `mac-a`-special-case or single-node-only design merely because real Mac B validation is deferred.

## 4. M5.5A real acceptance gate

The original M5.5A Contract §14 items remain authoritative except for items 6 and 7.

For M5.5A closure, the required items are:

1. Mac A installed under LaunchAgent without terminal-dependent daily runtime.
2. Mac A configured through `/settings` without hand-editing Node YAML.
3. LaunchAgent survives process termination/restart and returns automatically.
4. NAS Hub runs from canonical Docker Compose and returns after container restart.
5. Hub Admin creates Mac A token; Mac A becomes online.
6. Add/reset/enable/disable registry mutations survive Hub restart. *(original §14 item 8)*
7. Old node token is rejected and new token succeeds after reset. *(original §14 item 9)*
8. Real Claude/Codex + System/Network state continues flowing while supervised. *(original §14 item 10)*
9. Browser `/display` can remain open as the always-on observation surface and shows current Mac A state. *(original §14 item 11; browser path is sufficient for M5.5A)*
10. No stored admin/node token is leaked by normal GET/logs. *(original §14 item 12)*

Original §14 items 6 and 7 are explicitly **DEFERRED**, not waived as passing:

- real Mac B pairing through the same UI flow;
- independent real Mac A + Mac B presence on Hub/Display.

Those are tracked by Issue #5 as M5.5B real multi-node expansion acceptance.

## 5. Deployment path clarification

The Contract requires the Hub image to be built with the current Go 1.26 toolchain; it does not require the physical NAS to perform the build.

For the accepted Synology-class dogfood environment, the canonical operational path is:

```text
audited source on Mac
→ build linux/amd64 image
→ verify/save image archive
→ transfer archive to NAS
→ docker load
→ tag as devboard/hub:dogfood
→ docker compose up -d --no-build
```

This preserves canonical `deploy/hub/docker-compose.yml` while avoiding an unnecessary NAS-side registry/build dependency.

A developer environment may still build locally when appropriate. The production dogfood acceptance evidence is tied to the verified prebuilt image provenance chain.

## 6. Deferred M5.5B scope

M5.5B will validate, on real second hardware:

- Mac B installation and Settings pairing;
- independent Mac A and Mac B registration/authentication;
- independent state/liveness on Hub and `/display`;
- multi-node credential and enable/disable behavior.

M5.5B must preserve the same outbound Node → Hub architecture and must not introduce Hub polling of Mac LAN addresses.

## 7. Frozen marker

```text
M5_5A_SINGLE_NODE_CLOSURE_SCOPE = FROZEN_V1
M5_5B_MULTI_NODE_REAL_ACCEPTANCE = DEFERRED_FROM_M5_5A
```

This file is governance authority owned by the core auditor. Local construction assistants must not edit it as implementation remediation.
