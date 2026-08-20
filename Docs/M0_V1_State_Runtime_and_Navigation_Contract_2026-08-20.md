# DevBoard M0 — V1 State, Runtime, and Navigation Contract

> Date: 2026-08-20  
> Milestone: M0 — Contract Freeze  
> Status: **M0 CONTRACT FROZEN**  
> Scope: documentation and synthetic contract examples only. No runtime implementation.

## 1. Product boundary

DevBoard V1 is a **local-first development status aggregation and safe-navigation system**.

V1 has two product capabilities:

1. **Status Display** — normalize development signals into one state model and render them on persistent displays.
2. **Safe Navigation** — accept only allow-listed navigation intents and resolve them to trusted server-owned targets.

First-class display targets include old Kindle browsers, phones, tablets, and desktop browsers.

DevBoard V1 is not a quota-only dashboard, Kindle-only application, AI orchestration platform, remote shell, Electron application, replacement for Codex/Claude, or full observability platform.

### 1.1 V1 allowed navigation

Allowed actions:

- `focus_app`
- `focus_agent`
- `focus_project`
- `open_project`

Execution-changing actions are out of V1, including `approve`, `deny`, `stop`, `retry`, `send_prompt`, `execute_shell`, arbitrary commands, client-supplied AppleScript, and client-supplied application/URL execution.

A navigation failure is operational only. It must never change agent activity, outcome, freshness, or alert lifecycle.

## 2. Architecture authority

```text
Data Sources
    ↓
Collectors / Hook Adapters
    ↓
Normalized Events
    ↓
Unified State Core
    ↓
┌─────────────────────────┐
│ Display Projection      │
│ Navigation Projection   │
└─────────────────────────┘
    ↓                 ↓
Display            Navigation Router
    ↓                 ↓
Kindle/Phone       Target Resolver
Browser                ↓
                   Host Adapter
                       ↓
                   macOS App
```

Input devices do not own business semantics. Kindle touch, phone, keyboard, and future MX Master 4 must emit the same `NavigationIntent`.

Provider payloads are input facts only. They are never public State Authority.

## 3. State authority and projections

The State Core owns `InternalState`.

`InternalState` may contain private fields needed for collectors and trusted navigation resolution, including worktree roots and opaque focus locators.

Display/API consumers receive only `PublicState`, a sanitized projection:

```text
InternalState
    ↓ sanitize / derive
PublicState
    ↓
Display / read API
```

`DisplayStatus`, elapsed time, alert visibility phase, and other presentation labels are derived. They are not persisted as business authority.

The synthetic `root-state-v1.example.json` illustrates internal contract structure so `AgentTarget` and worktree identity can be reviewed. It is documentation only and intentionally uses synthetic paths. A production public projection must remove internal paths and `focusLocator`.

## 4. Host identity

Root state supports host identity from V1:

- `host.id`
- `host.displayName`

`AgentTarget.hostId` is mandatory whenever a target is host-bound.

V1 runtime may operate one local host only. Contracts must not assume a singleton host forever. Future Hub → multiple Mac nodes must remain possible without redesigning `AgentTarget`.

## 5. Agent state: three independent dimensions

### 5.1 Activity

Authoritative values:

- `idle`
- `working`
- `attention`
- `error`

Activity describes what the current top-level turn is doing now.

### 5.2 Outcome

Authoritative values:

- `none`
- `completed`
- `failed`

Outcome describes the latest top-level turn result relevant to the session state.

`COMPLETE` is not activity. It is derived when activity is `idle` and a recent outcome is `completed`.

### 5.3 Freshness

Authoritative values:

- `fresh`
- `stale`

Freshness is source confidence. It never overwrites activity or outcome.

Example:

```text
activity = working
freshness = stale
→ display: STALE · was WORKING
```

### 5.4 DisplayStatus

Derived display labels may include:

- `WORKING`
- `ATTENTION`
- `COMPLETE`
- `ERROR`
- `STALE`
- `IDLE`

Never persist `DisplayStatus` as authority.

## 6. Top-level turn semantics

A V1 task means exactly **one top-level agent turn initiated by a user prompt**.

DevBoard does not infer project, milestone, PR, or business-task completion unless a future source explicitly supplies such semantics.

### 6.1 Begin turn

`UserPromptSubmit`:

- begins a new top-level turn;
- establishes the new current `turnId`;
- sets `activity = working`;
- sets `outcome = none`;
- sets `startedAt`;
- supersedes older-turn activity for current display.

Only a begin-turn event may replace current turn identity.

### 6.2 Successful stop

`Stop` for the current turn:

- `activity = idle`
- `outcome = completed`
- `completedAt = occurredAt`

### 6.3 Fatal stop failure

`StopFailure` for the current turn:

- `activity = error`
- `outcome = failed`
- failure time derives from event time.

### 6.4 Session end

`SessionEnd`:

- sets current activity to `idle`;
- does not immediately erase a recent `completed` or `failed` outcome;
- resolves waiting attention for that session.

### 6.5 New turn after completion

A newer `UserPromptSubmit` starts a fresh current turn and resets the new turn outcome to `none`. Previous outcomes may remain in bounded history/alerts but cannot describe the new active turn.

## 7. Normalized AgentEvent V1

Required fields:

- `schemaVersion`
- `eventId`
- `provider`
- `sessionId`
- `turnId`
- `eventType`
- `occurredAt`
- `cwd`
- `metadata`

Initial providers:

- `codex`
- `claude-code`

Canonical session identity:

```text
<provider>:<sessionId>
```

Example: `codex:session-synth-001`.

`metadata` is strictly allow-listed and sanitized. It must not carry raw prompt text, assistant response text, transcript content, arbitrary environment variables, tokens, or arbitrary command payloads.

`cwd` is private ingestion data. Public projection exposes sanitized project/worktree identity instead of the absolute path.

## 8. Event idempotency and ordering

Reducers must satisfy all rules below.

1. **Event idempotency** — an already-seen `eventId` is a no-op.
2. **Turn authority** — only a begin-turn event (`UserPromptSubmit`) may replace the session's current `turnId`.
3. **Historical-turn protection** — once turn B is current, a non-begin event from turn A cannot mutate turn B activity/outcome.
4. **Old begin protection** — reducers retain bounded seen-turn identity/history. A delayed duplicate/previous begin event cannot replace a newer current turn.
5. **Begin ordering** — for unseen begin-turn events in one session, `occurredAt` must not be earlier than the current turn `startedAt`; equal timestamps use internal ingestion sequence as deterministic tie-breaker.
6. **Current-turn event ordering** — a current-turn event older than the latest reduced event time cannot roll state backward. It may be retained for diagnostics/history but is not state authority.
7. **Repeated notifications** — alert identity is stable and deduplicated; repeated hooks refresh the same alert rather than creating unbounded duplicates.
8. **Subagents** — subagent lifecycle cannot begin/complete the parent top-level turn in V1.

Provider clock/path anomalies lower source confidence rather than fabricating lifecycle completion.

## 9. Provider event reduction

### 9.1 Common

| Event | Reduction |
|---|---|
| `UserPromptSubmit` | begin new turn → `working` + `none` |
| `PermissionRequest` | current turn → `attention` |
| `PostToolUse` | current turn → `working` |
| `Stop` | current turn → `idle` + `completed` |
| `SessionEnd` | session → `idle`; preserve recent outcome |

### 9.2 Claude Code

| Event | Reduction |
|---|---|
| `AskUserQuestion` | `attention` |
| `Elicitation` | `attention` |
| `Notification.permission_prompt` | `attention` |
| `Notification.elicitation_dialog` | `attention` |
| `Notification.idle_prompt` | bounded idle fallback only |
| `PostToolUseFailure` | `working` |
| `PermissionDenied` | `working` |
| `ElicitationResult` | `working` |
| `StopFailure` | `error` + `failed` |

A recoverable tool failure is not terminal agent failure.

### 9.3 Codex

Codex adapters may consume currently available lifecycle concepts such as `session_id`, `turn_id`, `cwd`, `hook_event_name`, `model`, and `permission_mode`, but the contract does not assume every Codex version emits every lifecycle event.

Provider capability health:

- `available`
- `degraded`
- `unavailable`

If reliable completion cannot be observed, mark the source degraded/stale. Never synthesize a completed outcome.

## 10. Local hook ingestion boundary

M2 is expected to use a local helper/adapter boundary and local Unix-domain ingestion where practical.

M0 freezes only these properties:

- provider hooks are adapters, not public API authority;
- local hook ingestion is host-local by default;
- raw provider payloads are sanitized before normalized event storage;
- no client can submit arbitrary execution instructions through hook ingestion;
- transport/runtime implementation belongs to M2.

No hook is installed during M0.

## 11. AgentTarget

`AgentTarget` is trusted navigation metadata, not execution authority.

Conceptual fields:

- `targetId`
- `hostId`
- `provider`
- `sessionId`
- `turnId` (optional)
- `projectId` (optional)
- `worktreeId` (optional)
- `preferredApp` (optional)
- `focusLocator` (optional, opaque and server-owned)

Rules:

- clients navigate by `targetId`, not by locator;
- `focusLocator` is never interpreted from client input;
- target data must come from trusted current/pinned state or trusted configuration;
- public display projection may expose `targetId` and allowed actions but must not expose private `focusLocator`.

## 12. NavigationIntent V1

Required fields:

- `schemaVersion`
- `requestId`
- `action`
- `targetId`
- `source`
- `requestedAt`

Allowed actions:

- `focus_app`
- `focus_agent`
- `focus_project`
- `open_project`

Recognized sources include:

- `kindle`
- `web`
- `phone`
- `keyboard`
- `mx-master-4`

`mx-master-4` is contract-only in V1.

The client must never send an executable path, shell string, arbitrary command, AppleScript, arbitrary application, or arbitrary URL.

## 13. NavigationResult V1

Fields:

- `requestId`
- `status`
- `resolvedTarget`
- `message`
- `completedAt`

Allowed status values:

- `accepted`
- `completed`
- `unavailable`
- `unsupported`
- `failed`

`resolvedTarget` is a sanitized resolution summary, not an executable command or private locator.

## 14. Navigation router semantics

Navigation processing is:

```text
NavigationIntent
    ↓ validate schema/action/source/size
Target Resolver
    ↓ trusted targetId lookup
Host check / capability check
    ↓
Host Adapter
    ↓
sanitized NavigationResult
```

The router rejects unknown or expired targets. It never falls back to interpreting `targetId` as a path, URL, command, application identifier, or AppleScript.

Host adapters may only implement the V1 allow-list.

## 15. Navigation web security

V1 has separate web surfaces.

### Read surface

Read-only endpoints such as:

- `/health`
- `/api/state`
- `/display`
- `/display/kindle`

### Navigation surface

A dedicated allow-listed navigation endpoint is implemented later (M5). M0 freezes these controls:

- method validation;
- request-size limit;
- schema and enum validation;
- known `targetId` validation;
- target must belong to trusted current/pinned state;
- host/capability validation;
- explicit same-origin/auth policy;
- no wildcard CORS;
- no generic execution endpoint.

Intended V1 LAN mechanism:

**per-install random navigation token + same-origin browser navigation flow**.

The long-lived navigation token is server-owned secret material:

- it must not appear in public state JSON;
- it must not be logged;
- it must not be accepted through arbitrary cross-origin requests;
- exact cookie/form/nonce transport is finalized in M1 before any navigation implementation, with old Kindle compatibility tested.

## 16. System metrics

Primary local V1 collection uses an embedded mature Go system metrics library, preferably `gopsutil` or an equivalent selected after implementation audit.

Glances is not a mandatory local daemon. A future Glances adapter remains valid for remote machines, NAS, VPS, or externally monitored systems.

System state models:

- CPU
- memory
- swap
- disk
- tracked process groups

A process group aggregates all matching PIDs.

Frozen aggregate semantics:

- memory = sum of matched resident memory bytes;
- CPU = sum of matched process CPU values using one documented library/unit convention;
- unavailable metric = `null`, never fabricated as `0`.

## 17. SourceHealth and collector isolation

Every optional/external collector has:

- `status`: `available | degraded | unavailable`
- `lastAttemptAt`
- `lastSuccessAt`
- `message`

Messages are sanitized.

Collector failures are isolated. Quota failure cannot degrade Agent state. One project failure cannot invalidate unrelated projects. Missing `gh` cannot stop local Git status.

## 18. Project/worktree identity

Projects may enter state through:

1. pinned configuration;
2. agent `cwd` auto-discovery.

When `cwd` is supplied, resolve the nearest Git worktree root. Identity must be worktree-aware.

Frozen concepts:

- `projectId`
- `displayName`
- `worktreeId`
- `worktreeRoot` (internal/private)
- `repositoryIdentity`
- `branch`
- `dirty`
- `modifiedCount`
- `untrackedCount`
- `ahead`
- `behind`
- `sourceHealth`

Repository identity must not be derived from friendly display name alone.

Absolute filesystem paths are internal by default. Public state exposes sanitized project/worktree identity.

GitHub PR/CI is optional; local Git remains functional without `gh`.

## 19. Alert model

Alert types:

- `attention`
- `error`
- `complete`
- `stale`

Recommended display priority:

1. `ATTENTION`
2. `ERROR`
3. `STALE ACTIVE`
4. `COMPLETE`
5. `WORKING`
6. `INFO`

### Attention

Active while the current turn waits. Resolve when:

- same turn resumes working;
- same turn stops;
- same session starts a newer turn;
- session ends.

### Error

Persistent only for terminal/fatal agent failure.

### Complete

Default visibility:

- high visibility: first 10 minutes;
- recent: until 30 minutes;
- hidden after retention.

`SessionEnd` does not immediately remove a complete alert.

Alert identity must be stable and deduplicated, for example from alert type + canonical session + turn identity.

## 20. Kindle display contract

Endpoint:

```text
GET /display/kindle
```

Requirements:

- server-rendered HTML;
- basic CSS;
- black/white high contrast;
- large touch targets;
- meta refresh;
- no modern-JS dependency;
- text status always present.

Must not require Fetch, Promise, EventSource, WebSocket, CSS Grid, Canvas, SVG animation, React, or Vue.

### 20.1 Touch

Agent/project cards may initiate allowed navigation. Old-browser-compatible HTML must be used.

Preferred implementation choices:

- large `<a href="...">` for side-effect-free navigation pages; or
- conventional server-rendered `<form method="POST">` for a navigation action.

No JavaScript click handler is required. The whole visual card should behave as one large target.

### 20.2 E-Ink semantics

- `WORKING`: white background, standard border.
- `COMPLETE`: black background/white text where supported.
- `ATTENTION`: very thick border plus `ACTION REQUIRED`.
- `ERROR`: heavy distinct border plus explicit `ERROR`.
- `STALE`: explicit stale text; never color-only.

### 20.3 Orientation and browser chrome

Support portrait and landscape without hard-coding 600×800.

Allow explicit fallback:

- `/display/kindle?layout=portrait`
- `/display/kindle?layout=landscape`

Conservative orientation media queries may be used if supported.

Do not assume Kindle browser toolbar/menu can be hidden. Layout must remain usable with browser chrome visible. Kindle jailbreak/fullscreen and device-specific anti-sleep configuration are outside DevBoard runtime authority.

## 21. Modern display contract

Endpoint:

```text
GET /display
```

May use responsive CSS, small vanilla JS, SSE, and Screen Wake Lock API.

Wake Lock is best effort only. UI should be able to report acquired vs unavailable; DevBoard must not claim guaranteed no-sleep behavior.

## 22. Persistence and restart

V1 persistence:

```text
memory
+
atomic state snapshot
```

No database.

Snapshot may restore:

- recent outcomes;
- recent alerts;
- known projects/worktrees;
- source timestamps.

After daemon restart, previous `working` or `attention` activity is not immediately trusted. Restore its freshness as `stale` until a new lifecycle event confirms current state.

TTL/retention is timestamp-based. Continuously changing `elapsedSeconds` is never persisted as authority; it is derived from timestamps.

## 23. Public-state sanitization

Never expose through Display/public API:

- API keys;
- OAuth tokens;
- Claude/Codex credentials;
- raw prompts;
- assistant responses;
- transcript content;
- raw hook payloads;
- arbitrary environment variables;
- absolute filesystem paths by default;
- private focus locators;
- navigation token.

Public projection must be explicit and allow-listed. Raw `InternalState` must never be serialized as the public API by default.

## 24. M0 JSON example invariants

Required examples:

- `Docs/contracts/root-state-v1.example.json`
- `Docs/contracts/agent-event-v1.example.json`
- `Docs/contracts/navigation-intent-v1.example.json`
- `Docs/contracts/navigation-result-v1.example.json`

All example values are synthetic.

The root example includes:

- one local host;
- working Codex agent;
- attention Claude agent;
- recent completed agent;
- multiple SourceHealth states;
- one worktree;
- nullable unavailable metric;
- optional quota unavailable;
- AgentTarget instances.

## 25. Milestone boundary

- **M0 Contract Freeze** — this document + architecture reconciliation + synthetic JSON examples.
- **M1 Core + State + Mock Display** — Go skeleton, config, state store, mock states, public projection, `/health`, `/api/state`, `/display`, `/display/kindle`.
- **M2 Agent Event Ingestion** — CLI helper, Unix socket, reducers, Codex, Claude, alert engine.
- **M3 System Metrics** — embedded local metrics collector and process groups.
- **M4 Project / Worktree** — Git discovery, pinned project, cwd discovery, local status.
- **M5 Safe Navigation** — NavigationIntent, resolver, macOS `focus_app`, `focus_project`, `focus_agent` capability adapters.
- **M6 Optional Quota** — CodexBar adapter, independent source health.
- **M7 Production Runtime** — launchd, atomic snapshot, log retention, startup checks, graceful shutdown.

Future V2: execution-changing Control Layer, MX Master 4, Action Ring, haptics, keyboard backlight, multi-host node transport, approve/stop/retry.

## 26. M0 closure

M0 contains no production Go server, collectors, hook installation, window switching, MX Master 4 integration, or execution-changing controls.

Material product/state/runtime/navigation decisions required for M0 are frozen by this contract. Runtime transport details explicitly assigned to later milestones are not unresolved M0 business decisions.
