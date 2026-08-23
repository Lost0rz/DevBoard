# DevBoard PC1 macOS Product — Long-Running Construction Specification

Status: **AUDITOR-OWNED / FROZEN FOR PC1-MAC**  
Workstream: **macOS Product**  
Parent: GitHub Issue #8 / child Issue #10  
Construction branch: `codex/pc1-macos`  
Purpose: turn the existing Go Node/runtime foundation into a real user-installable DevBoard Mac product without rewriting the monitoring backend. This is a long-running construction target, not a sequence of micro-remediations and not a reliability/hardening milestone.

```text
PC1_MACOS_CONSTRUCTION_SPEC = FROZEN_V1
```

## 1. Final product outcome

The macOS workstream is complete when a user can take a built `DevBoard.app` artifact to a Mac that has no DevBoard source checkout and no Go toolchain, launch the app, install/repair the background Node, configure it through the supported Settings flow, manage Codex/Claude monitoring integrations, and see real service/Hub/integration status without using Terminal for normal daily operation.

The final product shape is:

```text
DevBoard.app
  ├─ DevBoard Node status/control
  ├─ Hub connection/status shortcuts
  ├─ Codex integration install/status/remove
  ├─ Claude Code integration install/status/remove
  └─ embedded universal Go helper
          ↓
~/Library/Application Support/DevBoard/bin/devboard
          ↓
per-user LaunchAgent com.devboard.node
          ↓
existing Go Node runtime
  ├─ System / Network collectors
  ├─ Codex / Claude local ingestion
  ├─ state.Store / PublicState
  ├─ loopback Settings + node status API
  └─ authenticated outbound Node -> Hub uplink
```

Do not rebuild Node monitoring logic in Swift.

## 2. Authority order

When requirements appear to conflict, use this order:

1. This PC1-MAC construction specification for macOS productization scope and final product intent.
2. `Docs/contracts/m5-5a-dogfood-deployment-v1.md` — frozen persistent service, Settings, restart, credential, Hub Admin and deployment behavior.
3. `Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md` — single real Mac first while preserving multi-node-capable architecture.
4. `Docs/contracts/m5-2-node-hub-ingestion-v1.md` — frozen Node/Hub authority, outbound push, authentication, privacy, liveness and machine transport.
5. `Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md` — state/runtime invariants where not superseded by later Monitoring MVP scope.
6. `Docs/contracts/mvp-monitoring-v1.md` — read-only monitoring product semantics; control/Safe Navigation/Process Groups remain deferred.
7. `Docs/contracts/agent-task-observability-v1.md` — business/privacy meaning of monitored Codex/Claude task information.
8. `Docs/contracts/m4-task-observability-v1.md` — technical task observability, fail-open monitoring and privacy boundaries.
9. `Docs/M2_Agent_Hook_Setup_2026-08-20.md` — historical/provider hook integration setup evidence; useful for compatibility, but PC1 replaces manual-only onboarding with product-managed user-level installation.

Important scope extension: `m5-5a-dogfood-deployment-v1.md` previously listed a native SwiftUI app as out of scope for M5.5A. PC1 explicitly authorizes the SwiftUI **product shell** now. This supersedes only that old scope exclusion. It does not supersede the frozen Node/Hub topology, LaunchAgent supervisor model, Settings security, config/restart semantics, token/privacy rules, or read-only Monitoring MVP boundary.

## 3. Existing foundation to reuse

The repository already contains the expensive backend foundation. Reuse it:

- `cmd/devboard` runtime and role dispatch;
- Node collectors and local agent ingestion;
- `state.Store` / PublicState projection;
- Node uplink scheduler/client;
- loopback `/settings` flow and atomic config authority;
- native `/health` behavior;
- per-user LaunchAgent deployment model and stable paths;
- Codex/Claude `agent-hook` adapters and normalized task lifecycle;
- current PC1 product CLI/service/integration implementation;
- current SwiftUI app/Xcode project/build script.

The Agent may refactor the Mac-owned product layer substantially, but must preserve and reuse the monitoring/runtime authority rather than create parallel state or collectors.

## 4. Design freedom

The Mac Agent may make implementation/design decisions inside the owned product boundary to reach the final user journey. It may:

- refactor `internal/product/**`;
- split product service/integration code into additional files;
- improve the SwiftUI app structure, models, controllers and UX;
- add internal helper abstractions and tests;
- improve app status handling, error presentation and async execution;
- improve build/packaging scripts;
- make provider configuration mutation safer and more maintainable;
- add migration/repair logic for the existing managed paths where it preserves user config and frozen behavior.

It must not invent a new external API or modify another workstream to make local implementation easier.

If the final target requires changing Node->Hub wire format, PublicState, Hub Registry/auth, frontend-owned presentation, NAS packaging, or frozen security semantics, STOP and report the required cross-workstream change.

## 5. Owned implementation boundary

Primary owned paths:

```text
cmd/devboard/product*.go
internal/product/**
macos/DevBoardApp/**
scripts/build-macos-app.sh
```

Conditionally owned backend paths only for the frozen local product interface:

```text
internal/web/settings.go
internal/web/settings_test.go
internal/web/product_ui_test.go   # only Node-status contract tests if needed
```

Do not redesign Settings presentation in this workstream; Frontend owns Web presentation.

Do not modify:

```text
internal/web/templates/**
internal/web/static/**
deploy/hub/**
internal/state/**
internal/agent/**
internal/hub/**
internal/uplink/**
internal/config/**
Docs/contracts/**
```

If an existing interface is insufficient, report it instead of crossing ownership silently.

## 6. Frozen product CLI

The public CLI grammar is exactly:

```text
devboard product service install
devboard product service status
devboard product service restart
devboard product service uninstall

devboard product integrations status
devboard product integrations install codex
devboard product integrations install claude-code
devboard product integrations remove codex
devboard product integrations remove claude-code
```

Every `devboard product ...` invocation:

- writes one bounded JSON object to stdout;
- uses schema version 1;
- exits 0 for successful operation and 1 for failure/attention state according to the existing product contract;
- never prints config contents, tokens, provider files, raw prompts/transcripts or logs.

`devboard agent-hook ...` is separate and must retain zero stdout + fail-open monitoring semantics. Product UX work must not make provider hooks block the coding agent when monitoring fails.

Do not expose provider-specific public `integrations status <provider>` commands. The combined status command is the public interface.

## 7. Stable managed Mac paths

Canonical paths remain:

```text
~/Library/Application Support/DevBoard/bin/devboard
~/Library/Application Support/DevBoard/node.yaml
~/Library/Logs/DevBoard/
~/Library/LaunchAgents/com.devboard.node.plist
```

LaunchAgent label:

```text
com.devboard.node
```

Requirements:

- per-user only;
- no sudo/root;
- `RunAtLoad=true`;
- `KeepAlive=true`;
- existing config preserved on repair/upgrade;
- config remains private (`0600` authority);
- uninstall removes managed service/binary/plist but preserves config/logs unless a future explicitly authorized purge feature is added;
- do not chmod an existing user `~/Library/LaunchAgents` directory merely to impose DevBoard preferences.

PC1 does not migrate supervision to SMAppService. LaunchAgent remains the frozen supervisor for this product slice.

## 8. Service install/repair correctness

`product service install` is the authoritative app-driven installation/repair path.

It must:

1. resolve/create private DevBoard support/log directories;
2. atomically install the currently running embedded helper into the stable binary path;
3. create a default config only when no config exists, using existing config authority/defaults;
4. preserve an existing config rather than regenerate/normalize it;
5. write/validate the LaunchAgent plist using stable paths;
6. bootstrap/kickstart using argument-vector process execution, never interpolated shell command construction;
7. boundedly verify the launched Node before returning success.

Success requires ownership, not merely a responsive port:

```text
launchctl print -> state=running + positive PID
same PID owns TCP/8787 LISTEN
GET /health succeeds with role=node
launchctl still reports the same PID after the health check
```

A foreign/historical process on port 8787 must never produce service success. Do not kill unknown conflicting processes automatically.

`restart` must return success only after the restarted LaunchAgent-owned Node passes the same bounded ownership/health verification.

`status` must distinguish not-running/unhealthy/healthy using the same ownership truth.

## 9. Frozen local Node product-status API

Node role exposes:

```text
GET /api/node/status
```

It is a read-only local product interface for DevBoard.app.

Requirements:

- GET only;
- loopback-only server exposure;
- same Host/DNS-rebinding protection as `/settings`;
- `Cache-Control: no-store`;
- no service-control POST endpoint;
- no CORS broadening;
- no token value.

Exact public keys:

```text
schemaVersion
serviceRunning
nodeId
displayName
hubEndpoint
uplinkEnabled
tokenConfigured
uplinkRunning
connected
lastAttemptAt
lastSuccessAt
lastErrorClass
```

`tokenConfigured` is boolean only. `uplinkRunning` reflects scheduler/health-source presence. Connection/timestamp/error fields come from the existing uplink health authority.

The Mac App consumes this endpoint but must not infer remote truth from button state alone.

## 10. DevBoard.app final UX

The native app is a focused control/status shell, not a second monitoring dashboard and not a replacement for the Web UI.

At minimum it presents three clear sections:

### DevBoard Node

Show real service state and provide:

- Install / Repair Background Node when not verified healthy;
- Restart Node when installed;
- Open Local Settings.

Long-running helper operations must execute off the MainActor. The window must remain responsive during bounded service install/restart checks.

### Hub

Consume `/api/node/status` and show:

- Hub endpoint configured/not configured;
- uplink running/enabled state;
- connected/disconnected status;
- bounded last-error context when useful.

When a Hub endpoint exists, expose:

- Open Hub Dashboard (`/display`);
- Open Hub Admin (`/admin`).

Construct URLs safely from the configured endpoint and never expose the bearer token.

### Integrations

Show Codex and Claude Code independently with real product-command status.

Each provider supports:

- status;
- Install / Repair;
- Remove.

The App must decode the existing Go JSON robustly. Direct operation results may contain primitive `data` metadata and must still decode successfully. The combined `integrations status` envelope maps provider keys `codex` and `claude-code` to provider result objects. The Swift model may use separate leaf/envelope types or an equivalent robust design, but it must not require primitive metadata to decode as nested ProductResult objects.

## 11. Codex integration product contract

Managed target:

```text
~/.codex/hooks.json
```

Do not edit project-local `.codex` files.

Required DevBoard events:

```text
UserPromptSubmit
PreToolUse
PermissionRequest
PostToolUse
Stop
SessionEnd
```

The DevBoard handler invokes the stable binary:

```text
~/Library/Application Support/DevBoard/bin/devboard agent-hook codex
```

Construct valid Codex command syntax including safe quoting for spaces in `Application Support`.

Before install/repair, inspect user-level `~/.codex/config.toml`. If inline `[hooks]` configuration is present, do not create/mutate managed `hooks.json`; report `manual_configuration_required` and preserve user files. This conflict blocks install/repair only. Remove must still be able to remove exact DevBoard-owned handlers from `hooks.json` while preserving all unrelated settings/handlers.

Status semantics:

```text
not_configured
repair_required
configured_requires_trust
manual_configuration_required
invalid_configuration / write failure classes as appropriate
```

A complete managed file is not proof Codex has trusted/activated the hook. Successful configured status must clearly say that the user must review/trust DevBoard via Codex `/hooks`.

## 12. Claude Code integration product contract

Managed target:

```text
~/.claude/settings.json
```

Required events:

```text
UserPromptSubmit
PreToolUse
PermissionRequest
PostToolUse
PostToolUseFailure
PermissionDenied
Notification
Stop
StopFailure
SessionEnd
Elicitation
ElicitationResult
```

Use exec-form command semantics:

```text
command = stable devboard binary
args = ["agent-hook", "claude-code"]
```

Do not place the arguments inside a shell command string.

If existing `disableAllHooks=true`, preserve it and report `configured_but_disabled`; do not silently enable hooks.

Status must distinguish complete vs partial configuration. Remove deletes exact DevBoard-owned handlers only.

## 13. Shared provider mutation safety

Codex and Claude managed JSON updates must:

- parse existing JSON before mutation;
- make no write on malformed/incompatible structure;
- preserve unrelated top-level fields;
- preserve unrelated events/groups/handlers;
- append only missing exact DevBoard handlers;
- be idempotent;
- remove only exact DevBoard handlers;
- use same-directory temp file + fsync + atomic rename;
- preserve existing file permissions on replacement;
- create new private config files with mode `0600`.

Never log/return full provider configs, unrelated user hook commands, raw prompts/transcripts, credentials or Node tokens.

## 14. Build artifact

The workstream must produce a usable dogfood artifact:

```text
dist/DevBoard-macos-universal.zip
```

Containing `DevBoard.app` with embedded resource:

```text
devboard-bootstrap
```

Both app executable and helper must contain:

```text
arm64
x86_64
```

The build flow should:

- cross-build the Go helper for both Darwin architectures;
- combine with `lipo`;
- build SwiftUI target/scheme `DevBoard` for both architectures;
- place the helper in app Resources;
- ad-hoc sign helper/app for dogfood;
- verify `codesign --verify --deep --strict`;
- package with `ditto`.

PC1 does not require Developer ID credentials, notarization, DMG or auto-update. Those are distribution/hardening gates after the basic product vertical is usable.

## 15. Product UX and failure honesty

The App must remain useful when something is missing or broken:

- Node absent -> Install/Repair state;
- service unhealthy/foreign port -> clear bounded error, not fake Running;
- Settings endpoint unavailable -> status unavailable, not guessed;
- Hub not configured -> clear first-run state;
- uplink configured but disconnected -> show configured/disconnected;
- partial provider hooks -> Repair required;
- Codex trust still pending -> explicit `/hooks` instruction;
- Claude hooks disabled -> explicit disabled state;
- malformed provider file -> no destructive rewrite and clear bounded error.

Do not surface secrets, raw config contents or full logs in the App.

## 16. Explicit non-goals

PC1-MAC does not implement:

- Web Dashboard/Admin/Settings visual redesign;
- NAS installer/bundle;
- Browser AI Watch;
- Quota collector;
- Process Groups;
- Safe Navigation;
- remote approve/deny/stop/retry/send-prompt/execute-shell;
- Mac B real-hardware onboarding;
- Hub polling of Mac LAN addresses;
- Developer ID/notarization/update service.

## 17. Validation expectations

At minimum run:

```text
gofmt -w <touched Go files>
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
git diff --check
bash -n scripts/build-macos-app.sh
plutil -lint deploy/macos/com.devboard.node.plist.template
scripts/build-macos-app.sh
codesign --verify --deep --strict <built DevBoard.app>
lipo -archs <app executable>
lipo -archs <app Resources/devboard-bootstrap>
```

Tests must cover service ownership positive/negative cases, config preservation, uninstall preservation, exact CLI grammar, provider structured merge/remove/idempotency/conflicts/malformed no-write behavior, complete vs partial provider status, exact Node status API public fields/redaction/Host/method/health propagation, and relevant Swift/build integration behavior where practical.

## 18. Definition of macOS-complete handoff

The Agent hands back the workstream only when:

- a real `DevBoard.app` builds as universal arm64+x86_64;
- the embedded helper installs/repairs the per-user Node without source/Go/Terminal;
- LaunchAgent ownership/health is verified, not guessed;
- local Settings opens and remains the existing safe config authority;
- App reads real local Node/Hub status;
- Codex/Claude install/status/remove are productized and preserve user configuration;
- Codex trust and Claude disabled states are communicated honestly;
- no cross-module architecture/security contract was redefined;
- full validation passes and branch is committed/pushed cleanly.

The Agent reports exact START_HEAD, FINAL_HEAD, REMOTE_HEAD, changed files, app/service/provider/build validation and remaining concerns. It must not merge, edit frozen contracts, claim real NAS deployment, or claim full PC1 acceptance. Core Auditor performs the later three-workstream integration and real-machine product acceptance.
