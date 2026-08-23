# M5.5A Single-Node Dogfood Acceptance Evidence — 2026-08-23

Status: **CORE AUDIT ACCEPTED**

This record closes the real M5.5A dogfood acceptance under the frozen single-node closure amendment:

- implementation contract: `Docs/contracts/m5-5a-dogfood-deployment-v1.md`;
- scope amendment: `Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md`.

```text
M5_5A_DOGFOOD_DEPLOYMENT_CONTRACT = FROZEN_V1
M5_5A_SINGLE_NODE_CLOSURE_SCOPE = FROZEN_V1
M5_5A_CODE_READINESS = PASS
M5_5A_REAL_DOGFOOD_ACCEPTANCE = PASS
M5_5B_MULTI_NODE_REAL_ACCEPTANCE = DEFERRED_FROM_M5_5A
```

Real Mac B pairing and independent dual-node display are not claimed here; they are tracked by Issue #5.

## 1. Audited source and CI

Audited implementation commit:

`8deffc92b54c3c5446287582fedb2117f5c15152`

Original readiness/onboarding head:

`07db5b0289b9e3a9a130b59fb58b1e864ba24dd3`

Original final code-readiness CI:

`32619307705` — PASS

The later auditor-owned scope/onboarding/README amendment head also passed the full CI matrix before this closure record was written.

No implementation code change was required to defer real Mac B acceptance; existing multi-node Registry/API/state/UI capability remains intact.

## 2. Mac A persistent runtime

Accepted real Mac A facts:

- macOS 26.6.2 arm64;
- per-user `com.devboard.node` LaunchAgent installed without daily terminal dependency;
- LaunchAgent loaded/running with positive PID;
- the LaunchAgent PID itself owned TCP 8787;
- Node `/health` returned `status=ok`, `role=node`;
- private config mode `0600`;
- terminating the known Node process caused LaunchAgent KeepAlive to start a different PID that again owned 8787 and passed health;
- Settings save later caused the expected graceful supervised restart rather than a manual daemon start.

Canonical installed paths remain those frozen by the M5.5A Contract:

```text
~/Library/Application Support/DevBoard/bin/devboard
~/Library/Application Support/DevBoard/node.yaml
~/Library/Logs/DevBoard/node.out.log
~/Library/Logs/DevBoard/node.err.log
~/Library/LaunchAgents/com.devboard.node.plist
```

## 3. Mac A Settings onboarding

Mac A was paired through the loopback Settings surface only:

`http://127.0.0.1:8787/settings`

Accepted facts:

- Settings GET succeeded;
- configured token was never rendered by normal GET;
- Node ID `mac-a`, display name `Mac A`, Hub endpoint and uplink were committed through Settings;
- no hand-edit of Node YAML was used;
- successful Settings mutation caused graceful Node exit and LaunchAgent restart;
- the new LaunchAgent PID owned 8787 and Node health recovered;
- later Settings GET still did not expose the stored token.

## 4. NAS clean baseline and verified image provenance

Before final deployment, historical DevBoard containers/images were removed without a global Docker prune and without touching unrelated Docker projects. Persistent `deploy/hub/data` was preserved.

The accepted Hub image was built once from the audited source on Mac for `linux/amd64` and transferred as a verified archive.

Accepted provenance facts:

- Mac Docker 29 containerd-store inspect ID: `sha256:55a0bccad54bcf341aa69d3d27033f079d80b62496a162a08792241e8832c0df`;
- OCI config blob: `sha256:2a5da0c971713eee119b27cfb41f944f51e1f5534cf052c6a24ba6b1a38d239e`;
- OCI config self-hash: PASS;
- OCI config platform: `linux/amd64`;
- archive SHA-256 on Mac and NAS: `430a135033878378c07bd257eeb86f3e685d99b1b87855dcc678bfd26c28f3d5` on both sides;
- NAS Docker 24 classic-store loaded image ID: `sha256:2a5da0c971713eee119b27cfb41f944f51e1f5534cf052c6a24ba6b1a38d239e`;
- loaded platform: `linux/amd64`.

The differing Mac Docker 29 descriptor ID and OCI config digest were accepted as storage-backend semantics; cross-machine provenance was tied to the verified OCI config and identical archive bytes.

## 5. NAS canonical persistent Hub

Canonical deployment remained `deploy/hub/docker-compose.yml`.

Accepted real NAS facts:

- verified image tagged `devboard/hub:dogfood`;
- canonical Compose started with `--no-build`;
- host port `8788` mapped to container port `8787`;
- running container image matched the verified loaded image;
- process user was non-root (`1027:100` in the accepted environment);
- restart policy was `unless-stopped`;
- native Docker health reached `healthy`;
- `/health` returned `{"role":"hub","schemaVersion":1,"status":"ok"}`;
- explicit `docker compose restart devboard-hub` returned the same service to healthy state.

The accepted Synology operational path is:

```text
audited source on Mac
→ build linux/amd64 image
→ verify/save archive
→ transfer to NAS
→ docker load
→ tag devboard/hub:dogfood
→ docker compose up -d --no-build
```

This avoids making daily NAS operation depend on registry access or NAS-side compilation while preserving canonical Compose.

## 6. Hub Admin and Registry reconstruction

Authenticated Hub Admin was exercised on the trusted LAN.

Accepted facts:

- Admin login succeeded without exposing the admin credential;
- `mac-a` / `Mac A` was created through Admin;
- generated Node token was shown only at the successful mutation response and absent from later normal Admin GET;
- Add/Reset/Disable/Enable mutations followed the frozen model: atomic config save → graceful Hub exit → Docker supervisor restart → immutable Registry reconstruction;
- Hub health recovered after each mutation;
- registration and final enabled state persisted.

No hot-mutable Hub Registry redesign was introduced.

## 7. Token rotation acceptance

Accepted real credential-rotation sequence:

1. Reset Token was performed through Admin.
2. Mac A continued temporarily with the old configured token.
3. The old credential produced a sanitized uplink authentication rejection: HTTP 401.
4. The replacement token was installed through loopback `/settings`, with no YAML hand-edit.
5. LaunchAgent restarted the Node normally.
6. The replacement credential authenticated successfully and Mac A returned online.
7. Disable Node caused rejection while preserving registration.
8. Enable Node restored automatic online state using the same replacement token.

No token value is recorded in this evidence file.

## 8. Explicit Hub restart persistence and outbound repopulation

Before the final explicit Hub container restart, Mac A PID was `56136`.

After `docker compose restart devboard-hub`:

- Hub health recovered with role `hub`;
- `mac-a` remained registered and enabled;
- replacement-token authority persisted;
- Add/Reset/Disable/Enable state survived reconstruction;
- Mac A PID remained `56136`;
- the same PID still owned local 8787;
- Node health remained `ok`;
- Mac A became online again automatically;
- `/api/dashboard` repopulated Mac A current state;
- the same replacement token was accepted;
- repopulation came from normal outbound Node → Hub traffic;
- Hub did not poll a Mac LAN address.

This closes the persistence/reconstruction gate while preserving the frozen outbound topology.

## 9. Real state and browser surface

Accepted real supervised state:

- System state available at the Hub;
- Network state available at the Hub;
- a real Codex event reached Hub-visible public state;
- `/display` returned HTTP 200;
- `/api/dashboard` contained current Mac A state.

For M5.5A, `/display` is the always-on browser observation surface. Final visual redesign remains explicitly out of scope.

## 10. Privacy

Normal verification found no stored credential or raw provider-content leak in the inspected surfaces/logs:

- Hub Admin normal GET;
- dashboard API;
- `/display`;
- Mac Settings GET;
- normal Mac Node logs;
- accessible normal Hub logs.

Accepted result:

```text
stored_admin_secret_leak = NO
stored_node_token_leak = NO
raw_provider_prompt_leak = NO
raw_provider_transcript_leak = NO
```

## 11. Revised single-node closure gate

Under the scope amendment, all required M5.5A real gates passed:

1. Mac A persistent LaunchAgent runtime — PASS.
2. Mac A Settings onboarding without YAML hand-edit — PASS.
3. LaunchAgent process restart recovery — PASS.
4. NAS canonical Compose restart recovery — PASS.
5. Admin creates Mac A credential; Mac A online — PASS.
6. Add/Reset/Enable/Disable Registry persistence — PASS.
7. Old token rejected; replacement token succeeds — PASS.
8. Real Codex/System/Network flow — PASS.
9. Browser `/display` current Mac A observation — PASS.
10. Normal GET/log privacy — PASS.

## 12. Multi-node deferral

The implementation remains multi-node-capable. M5.5A does not remove:

- multiple Hub Registry entries;
- per-node independent credentials;
- multi-node NodeStateStore/dashboard data structures;
- multi-node-capable Admin and Display interfaces.

The following real-hardware validation is deferred to Issue #5 / M5.5B:

- real Mac B pairing;
- independent simultaneous Mac A + Mac B Hub/Display presence.

They are **DEFERRED**, not represented as M5.5A passing evidence.
