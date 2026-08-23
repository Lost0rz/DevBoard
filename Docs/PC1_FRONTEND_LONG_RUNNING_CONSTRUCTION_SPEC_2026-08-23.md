# DevBoard PC1 Frontend Product — Long-Running Construction Specification

Status: **AUDITOR-OWNED / FROZEN FOR PC1-FE**  
Workstream: **Frontend Product**  
Parent: GitHub Issue #8 / child Issue #9  
Construction branch: `codex/pc1-frontend`  
Purpose: finish the browser-facing DevBoard product using the existing backend/state/runtime foundation. This is a long-running construction target, not a micro-patch checklist and not a reliability/hardening milestone.

```text
PC1_FRONTEND_CONSTRUCTION_SPEC = FROZEN_V1
```

## 1. Final product outcome

The Frontend workstream is complete when DevBoard's browser surfaces feel like one coherent read-only product rather than diagnostic/prototype pages.

The user must be able to leave `/display` open as the primary status board and understand, without reading raw IDs or implementation diagnostics:

- which monitored Mac/Node is represented;
- whether the Node connection is ONLINE, STALE, or OFFLINE;
- whether the last accepted snapshot is CURRENT, RETAINED, or NONE;
- which Codex/Claude tasks are active;
- which task needs attention;
- what meaningful checkpoint a task most recently reached;
- what a completed task accomplished when a safe bounded completion summary exists;
- compact System and Network health;
- secondary Source/Project information;
- that Quota is unavailable/not connected when no real quota source exists, without fabricated values.

`/admin` must be a clear Hub control-plane UI for Node registration/credential lifecycle. `/settings` must be a clear local Node configuration UI. All three surfaces must look and behave as one DevBoard product.

## 2. Authority order

When requirements appear to conflict, use this order:

1. This PC1-FE construction specification for Frontend scope and product-completion intent.
2. `Docs/contracts/mvp-monitoring-v1.md` — frozen business definition of the read-only Monitoring MVP and information priority.
3. `Docs/contracts/agent-task-observability-v1.md` — frozen business meaning of task identity, lifecycle, checkpoints, attention, completion, and privacy.
4. `Docs/contracts/m4-task-observability-v1.md` — frozen technical meaning of TaskState/public task information and no-fabrication/privacy rules.
5. `Docs/contracts/display-ux-v1.md` — frozen display semantics, especially Kindle behavior and Agent-first presentation principles.
6. `Docs/contracts/m5-2-node-hub-ingestion-v1.md` — frozen Node/Hub authority, trusted registry identity, connection/liveness semantics, and retained last-good state.
7. `Docs/contracts/m5-5a-dogfood-deployment-v1.md` — frozen Settings/Admin security, restart, credential and deployment behavior.
8. `Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md` — PC1 real acceptance is single-node first while preserving multi-node-capable architecture.
9. `Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md` only for state/runtime/navigation invariants not superseded by later Monitoring MVP scope decisions.

Important scope correction: later Monitoring MVP contracts explicitly defer Process Groups and Safe Navigation from MVP closure. Do not resurrect them merely because older roadmap/state documents mention them.

## 3. Existing foundation to reuse

Do not rebuild the backend. The workstream starts with working foundations already present in the repository:

- Go SSR handlers/templates;
- DashboardState/view-model projection;
- Hub Registry and NodeStateStore data;
- existing ONLINE/STALE/OFFLINE connection logic;
- current CURRENT/RETAINED/NONE snapshot presentation semantics;
- Codex/Claude task lifecycle, checkpoint, attention and completion fields;
- Hub Admin authentication, CSRF and node mutations;
- Node loopback Settings, CSRF and token-redaction behavior;
- `/display/fragment` progressive-refresh foundation;
- separate Kindle route and legacy behavior.

Frontend construction should improve product hierarchy, clarity, responsive behavior and state communication, not invent a second state model.

## 4. Design freedom

The Frontend Agent may freely design/refactor within the owned presentation boundary to achieve the final product target. It may:

- reorganize templates and reusable presentation fragments;
- improve CSS architecture and responsive layouts;
- improve semantic HTML and accessibility;
- refine presentation-only view-model fields;
- improve browser progressive enhancement and refresh-failure UX;
- add focused presentation tests;
- choose a coherent DevBoard visual system using local assets only.

The Agent does not need Core Auditor approval for ordinary design choices inside this boundary.

The Agent MUST STOP and report instead of proceeding if the desired design requires changing a cross-module contract, state schema, authentication contract, machine endpoint, Node/Hub topology, or files owned by another workstream.

## 5. Owned implementation boundary

Primary owned paths:

```text
internal/web/templates/**
internal/web/static/**
internal/web/product_ui_test.go
```

Conditionally owned presentation paths:

```text
internal/web/network.go
internal/web/server.go
existing display-focused tests under internal/web/**
```

`network.go` may change only for view-model/presentation metadata. Do not change connection/snapshot truth semantics.

`server.go` may change only for Web presentation, local asset serving, fragment rendering and closely related GET behavior. Do not redesign machine APIs, Admin auth, Settings security, receiver/uplink behavior or service management.

Do not modify:

```text
internal/product/**
macos/**
deploy/hub/**
internal/state/**
internal/agent/**
internal/hub/**
internal/uplink/**
internal/config/**
Docs/contracts/**
```

## 6. Frozen cross-workstream interfaces

Frontend consumes facts; it does not redefine them.

### Hub/Node topology

```text
Mac Node -> authenticated outbound snapshot -> NAS Hub -> DashboardState -> SSR Web
```

The Hub never polls a Mac LAN address.

### Browser surfaces

```text
/display
/display/fragment
/display/kindle
/admin
/settings
```

### Connection/snapshot semantics

Node connection truth remains distinct from snapshot retention truth.

```text
Connection: ONLINE / STALE / OFFLINE
Snapshot:   CURRENT / RETAINED / NONE
```

Do not collapse these into one ambiguous badge.

A configured Node with no accepted snapshot must remain representable. An offline Node may retain a last-good snapshot and must be visibly different from an online current snapshot.

### Security

Frontend must not weaken or bypass:

- Hub Admin login/session/CSRF;
- one-time Node token display semantics;
- Node Settings loopback restriction;
- Settings Host/DNS-rebinding guard;
- Settings CSRF;
- token redaction/no-store behavior.

## 7. Final Dashboard design requirements

`/display` is the main product screen for modern browsers.

Recommended information hierarchy, which may be visually refined but not semantically inverted:

1. Product/host connectivity summary.
2. Global actionable attention when present.
3. **AI TASKS — primary product area.**
4. Compact System + Network health.
5. Secondary Sources / Projects / Quota status / bounded diagnostics.

### AI tasks

Cards must emphasize human-operational information rather than implementation IDs:

```text
Host
Provider
Project / worktree identity when available
Task title
Lifecycle
Elapsed/age
Latest meaningful checkpoint
Action required (optional)
Completion summary (optional)
```

Do not expose raw session/turn IDs, absolute cwd, transcripts, raw provider payloads, full prompts, full tool input/output or full assistant responses.

ATTENTION/actionable error must visually outrank ordinary WORKING. Recent COMPLETE delivery remains noticeable. Missing information must render honestly as unavailable rather than be guessed.

### Explicit product states

The UI must clearly handle at least:

- zero registered Nodes;
- registered Node awaiting first accepted snapshot;
- online/current;
- stale/current or stale/retained according to backend facts;
- offline with retained last-good snapshot;
- offline with no snapshot;
- no observed AI tasks;
- unavailable/degraded Sources;
- no real Quota source/data.

### Multi-node preservation

PC1 real acceptance uses one Mac, but Frontend must not introduce `mac-a` special cases or a single-node-only rendering/data structure. Existing multiple-host representation must continue to work.

## 8. Progressive refresh

Modern `/display` keeps SSR as initial truth.

`/display/fragment` must render dynamic content from the same server-owned dashboard view model. Browser JavaScript may periodically fetch and replace the dynamic fragment, but it must not deserialize DashboardState and implement its own reducer/presentation truth.

On refresh failure:

- keep the last successful DOM;
- show a bounded visible warning such as `Live refresh paused`;
- clear the warning when refresh recovers;
- do not erase valid retained content merely because one fetch failed.

Do not add React, Vue, npm, Node frontend build tooling, WebSocket/EventSource requirements, CDN assets or remote fonts for PC1.

## 9. Kindle boundary

`/display/kindle` remains governed by `display-ux-v1.md`.

Do not convert Kindle to the modern JS fragment mechanism. Do not remove its SSR/meta-refresh compatibility or old-browser layout semantics. Product styling changes elsewhere must not accidentally break the Kindle path.

## 10. Hub Admin final UX

The Admin UI must make the registry lifecycle understandable:

- login;
- registered Node list;
- Add Node;
- Enable;
- Disable;
- Reset Token;
- one-time credential presentation immediately after add/reset;
- restart/reconstruction transition after committed registry mutation.

The UI must not show raw tokens during ordinary GET. A newly generated token is a one-time mutation result and must be visually treated as such.

After a registry mutation has been committed and Hub restart requested, do not mix saved new registry facts with stale old runtime status in a misleading way.

## 11. Node Settings final UX

The Settings UI must clearly communicate:

- Node ID;
- Display Name;
- Hub Endpoint;
- uplink enabled/disabled;
- token configured yes/no without revealing the token;
- runtime/uplink state;
- Save behavior and supervised restart transition.

Blank token submission preserves the existing credential. Do not prefill the password field with the credential.

Settings must remain usable as the browser configuration surface opened by DevBoard.app; do not assume users will edit YAML manually.

## 12. Responsive/product quality

Target modern browser classes:

- desktop;
- tablet;
- phone.

Requirements:

- attention and active tasks remain visible without requiring horizontal scrolling;
- cards/tables adapt cleanly on narrow screens;
- controls are usable by keyboard and pointer;
- text/state meaning does not depend solely on color;
- loading/empty/error states are explicit;
- no decorative complexity that hides operational status.

## 13. Privacy and honesty

Frontend must present only public/sanitized facts already authorized by frozen contracts.

Never display or log:

- admin secret;
- Node token except the existing one-time Admin mutation result;
- raw prompt/transcript;
- full assistant response;
- raw tool payload;
- absolute private path;
- private correlation IDs.

Do not fabricate Quota, Browser Watch, project metadata, progress percentages, ETAs or provider symmetry.

## 14. Explicit non-goals

PC1-FE does not implement:

- Browser AI Watch collector/domain;
- Quota collector;
- Safe Navigation or remote approval/control;
- Process Groups;
- Mac application/service management;
- provider hook installation;
- NAS packaging/deployment;
- Mac B real-hardware onboarding.

## 15. Validation expectations

At minimum:

```text
gofmt -w <touched Go files>
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
git diff --check
```

Add/retain focused tests proving modern `/display` uses local assets/fragment refresh, connection vs snapshot semantics remain intact, explicit empty/offline states are rendered, Admin/Settings security behavior is not weakened, shared product styling is applied, and Kindle behavior remains unchanged.

## 16. Definition of Frontend-complete handoff

The Agent hands back the workstream only when:

- all three browser surfaces are coherent product UI;
- modern Dashboard has complete state/empty/error presentation;
- task/attention hierarchy is the dominant product story;
- System/Network and secondary diagnostics are compact and understandable;
- Admin and Settings are operationally clear;
- desktop/tablet/phone behavior is deliberate;
- Kindle remains compatible;
- no backend/state/security contract was redefined;
- owned tests and full repository validation pass;
- branch is committed/pushed with a clean working tree.

The Agent must report exact START_HEAD, FINAL_HEAD, REMOTE_HEAD, changed files, validation, screenshots/notes if useful, and remaining concerns. It must not merge, edit frozen contracts, or claim full PC1 product acceptance. Core Auditor performs the later three-workstream integration audit.
