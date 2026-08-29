# DevBoard M5.5A Dogfood Deployment Contract v1

Status: **FROZEN**

Owner: Core auditor / repository governance

Purpose: move DevBoard from temporary command-line E2E into an always-on dogfood deployment while preserving the frozen M5.2 Node → Hub ingestion topology.

This contract defines the required behavior for `codex/m5-5a-dogfood-deployment`. Local construction assistants implement against this contract; they do not redefine it.

## 1. Authority and topology

The existing frozen contracts remain authoritative and unchanged:

- `Docs/contracts/m5-2-node-hub-ingestion-v1.md`
- `Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`

M5.5A MUST preserve:

- Node owns Mac collectors, agent ingest, local state, PublicState projection and outbound uplink.
- Hub owns node registry/authentication, receiver/store/liveness and browser display.
- Cross-machine state transport remains outbound **Node → Hub push**.
- Hub MUST NOT require or poll a Mac LAN address.
- No historical `multi_host` polling fallback may become a production authority.
- M5.2 session/sequence/retry/admission/privacy semantics MUST NOT be redesigned by M5.5A.

## 2. Product outcome

M5.5A is complete only when the following always-on dogfood shape is usable:

```text
Mac A / Mac B
  user LaunchAgent
      ↓
  DevBoard Node daemon
      ├─ collectors + Claude/Codex ingest
      ├─ outbound Node → Hub uplink
      └─ http://127.0.0.1:8787/settings

NAS
  Docker Compose (restart: unless-stopped)
      ↓
  DevBoard Hub
      ├─ /api/node/v1/snapshot
      ├─ /api/dashboard
      ├─ /display
      └─ authenticated /admin
```

Normal Mac onboarding MUST NOT require hand-editing Node YAML after the initial installer bootstrap.

## 3. Persistent config contract

A validated atomic config writer MUST exist.

Required semantics:

1. Validate the complete prospective config before replacement.
2. Create a temporary file in the destination directory.
3. Write mode `0600`.
4. Complete the write and fsync the temporary file.
5. Atomic rename is the **installation commit point**.
6. Any failure before rename means the old destination remains authoritative.
7. A parent-directory fsync after a successful rename is durability hardening only; its failure MUST NOT be reported as "config not installed" to callers, because the new file is already committed.
8. No config/token content may be logged.
9. `Load(SaveAtomic(cfg))` must round-trip every active config section.

Managed config mutation MUST request a graceful supervised restart after the committed config has been returned successfully to the user.

## 4. Graceful restart contract

Settings/Admin handlers MUST NOT call `os.Exit` directly.

Successful committed mutation:

```text
validate → atomic save → render success → request restart
```

The serve runtime performs graceful `http.Server.Shutdown`; LaunchAgent or Docker owns process restart.

Validation/auth/CSRF/pre-commit persistence failure MUST NOT request restart.

## 5. Mac Node Settings contract

Node Settings routes:

- `GET /settings`
- `POST /settings`

Security and exposure:

- Settings is Node-role only.
- Settings is available only on a loopback server bind.
- HTTP Host authority must also be loopback/localhost to block DNS-rebinding access.
- Mutation is POST-only and CSRF-protected.
- Managed request body is bounded to **16 KiB maximum**.
- No wildcard CORS.
- Responses use `Cache-Control: no-store`.

Fields:

- Node ID
- Display Name
- Hub Endpoint
- Enable Uplink
- Token replacement

Token rules:

- GET never renders the configured token.
- Password field value is always blank.
- GET may expose only "token configured: yes/no".
- Blank POST token preserves the configured credential.
- Nonblank token replaces it after validation.

Operational status:

- Uplink scheduler absent/disabled: **Not running**.
- Scheduler exists but has no successful connection: **Disconnected**.
- Scheduler health may show Connected, Last Attempt, Last Success and bounded Last Error Class.
- No credential or raw Hub response body is exposed.

## 6. macOS persistent service contract

Canonical user-level paths:

- `~/Library/Application Support/DevBoard/bin/devboard`
- `~/Library/Application Support/DevBoard/node.yaml`
- `~/Library/Logs/DevBoard/node.out.log`
- `~/Library/Logs/DevBoard/node.err.log`
- `~/Library/LaunchAgents/com.devboard.node.plist`

Installer requirements:

- No `sudo`.
- Default Node config starts valid with loopback server and uplink disabled.
- Existing valid config is preserved on upgrade.
- Config mode is `0600`.
- LaunchAgent uses `RunAtLoad=true` and `KeepAlive=true`.
- Binary upgrade is atomic: build/copy into a temporary sibling, then rename into the final path only after success.
- A failed build MUST leave the previously installed binary intact.
- Startup verification is bounded (maximum 10 seconds recommended).
- Success requires BOTH the `com.devboard.node` LaunchAgent itself to be loaded/running with a valid PID AND the Node health endpoint to succeed.
- A foreign/historical process answering port 8787 MUST NOT produce installer success.
- Installer MUST NOT kill an unknown conflicting process automatically.
- Re-running installer upgrades/restarts idempotently.
- Uninstall preserves config unless an explicit purge option is used.
- M5.5A does not silently edit Claude/Codex provider configuration.

## 7. Native healthcheck contract

`devboard healthcheck` MUST:

- use GET only;
- have total timeout `<= 2s`;
- require HTTP 200;
- require valid single JSON response with `status=ok`;
- optionally require role `node|hub`;
- reject malformed/trailing data;
- reject redirects rather than following them;
- use no secret.

## 8. Hub Admin authentication contract

Hub Admin routes:

- `GET /admin`
- `POST /admin/login`
- `POST /admin/logout`
- authenticated node mutations under `/admin/nodes/*`

Machine provisioning credential:

- stored only in the absolute path from `admin.token_file`;
- not stored as a YAML secret value;
- exactly/at least 32 cryptographically random bytes represented by the supported secret format;
- secret file must reject group/world-readable permissions;
- accepted only by the machine Node provisioning API, never by the Web login form.

Web Admin password:

- first visit creates it through `POST /admin/setup`, with no username;
- stored as a salted, iterated hash in the private path from `admin.password_file`;
- missing password file is the supported first-run state;
- never logged, echoed, or returned by normal GET.

Session:

- constant-time credential verification;
- HMAC-signed session cookie;
- bounded lifetime (12h accepted);
- `HttpOnly`;
- `SameSite=Strict`;
- cookie `Path=/admin`;
- `Secure` when the direct request is HTTPS;
- cookie never contains either raw credential.

Mutations and logout:

- POST-only;
- session-bound CSRF required;
- managed body limit **16 KiB**;
- no mutation/restart on auth/CSRF/body-limit failure.

Direct cleartext HTTP Admin is permitted only for explicitly trusted-LAN dogfood. HTTPS or trusted reverse-proxy TLS termination is preferred outside that boundary. The raw Admin port MUST NOT be documented as Internet-safe.

## 9. Hub node-management contract

Admin MUST support:

- Add Node
- Enable Node
- Disable Node
- Reset Token

Delete is out of scope for M5.5A.

Node-token requirements:

- Add/reset generates 32 cryptographically random bytes and encodes them in the existing supported Node-token grammar.
- Newly generated token is shown only in the successful mutation result.
- Normal Admin GET and Dashboard APIs never render raw node tokens.
- Reset invalidates the old credential after the supervised Hub restart/reconstruction.
- Disable persists and reconstructed Hub receiver rejects the disabled node.

Registry mutation model is frozen for M5.5A:

```text
authenticated admin mutation
  → load latest config
  → serialized load/modify/save transaction
  → validate + atomic save
  → success response
  → graceful restart request
  → Docker supervisor restart
  → immutable Hub Registry reconstructed
  → Node heartbeat repopulates latest state
```

M5.5A MUST NOT redesign this into a hot mutable receiver registry unless this contract is explicitly revised by the core auditor.

## 10. NAS Docker contract

Canonical deployment entrypoint:

- `deploy/hub/docker-compose.yml`

There MUST NOT be a second active historical M5.1 Compose entrypoint that users can mistake for the dogfood deployment.

Container requirements:

- build with current Go 1.26 toolchain;
- non-root;
- `restart: unless-stopped`;
- persistent `/var/lib/devboard` config/admin data;
- native absolute-path healthcheck using `/usr/local/bin/devboard healthcheck`;
- read-only root filesystem where practical;
- tmpfs `/tmp`;
- `no-new-privileges`;
- drop all Linux capabilities;
- not privileged.

Private dogfood data, `.env`, tokens and local AI handoff artifacts MUST be excluded from Git and Docker build context.

## 11. NAS bootstrap contract

`deploy/hub/bootstrap.sh` targets Synology-class NAS environments and MUST use portable POSIX shell assumptions:

- `#!/bin/sh`
- `set -eu`
- no `BASH_SOURCE`;
- no Bash arrays/`[[`/`pipefail` dependency.

Bootstrap MUST:

- create private persistent data only when absent;
- preserve existing config and machine provisioning credential;
- generate the machine provisioning credential securely when missing;
- leave the Web Admin password absent for first-run setup unless it already exists;
- keep data directory private and secret/config files `0600`;
- preserve stable UID/GID configuration;
- never print the secret value;
- be idempotent across repeated execution.

## 12. Repository hygiene contract

Local AI coordination artifacts such as `.codex-agent-team/` are not product source and MUST NOT be committed or sent in Docker build context.

Historical executable deployment examples that contradict the canonical M5.5A deployment MUST be removed or converted into unmistakable non-executable deprecation pointers.

## 13. CI contract

Required CI remains:

- Ubuntu latest + Go 1.26: format, test, race, vet, build, diff check.
- macOS 26 + Go 1.26: format, test, race, vet, build, diff check.
- Ubuntu + Go 1.23 compatibility: test, vet, build.

Deployment validation:

- `bash -n deploy/macos/*.sh`
- `sh -n deploy/hub/*.sh`
- LaunchAgent plist lint on macOS;
- Docker build on Linux;
- Compose model validation on Linux.

No existing CI gate may be weakened to make M5.5A pass.

## 14. Real dogfood acceptance gate

Code readiness alone is not M5.5A closure.

After PR code/CI audit passes, the core auditor will separately authorize persistent installation.

Closure acceptance requires real supervised use:

1. Mac A installed under LaunchAgent without terminal-dependent daily runtime.
2. Mac A configured through `/settings` without hand-editing Node YAML.
3. LaunchAgent survives process termination/restart and returns automatically.
4. NAS Hub runs from canonical Docker Compose and returns after container restart.
5. Hub Admin creates Mac A token; Mac A becomes online.
6. Mac B can be added and paired through the same UI flow without hand-editing Node YAML.
7. Mac A and Mac B appear independently on Hub/Display.
8. Add/reset/enable/disable registry mutations survive Hub restart.
9. Old token is rejected and new token succeeds after reset.
10. Real Claude/Codex + System/Network state continues flowing while supervised.
11. iPad/browser can keep `/display` open as the always-on observation surface.
12. No stored admin/node token is leaked by normal GET/logs.

## 15. Explicitly out of scope

M5.5A does NOT include:

- native SwiftUI settings app;
- signing/notarization/final DMG;
- automatic updater;
- final Display visual redesign;
- Hub → Node Safe Navigation command transport;
- approve/deny/stop/retry/send-prompt/execute-shell controls.

Those require later contracts/PRs.

## 16. Frozen marker

```text
M5_5A_DOGFOOD_DEPLOYMENT_CONTRACT = FROZEN_V1
M5_5A_REAL_DOGFOOD_ACCEPTANCE = PENDING
```

Changes to this file are governance changes and require core-auditor review; local construction assistants must not edit it as part of implementation remediation.
