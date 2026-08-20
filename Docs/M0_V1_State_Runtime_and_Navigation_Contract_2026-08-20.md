# DevBoard M0 — V1 State, Runtime, and Navigation Contract

> Date: 2026-08-20  
> Milestone: M0 — Contract Freeze, M0.1 closure applied  
> Status: **M0 CONTRACT FROZEN**  
> Scope: documentation and synthetic contract examples only. No runtime implementation.

## 1. Product boundary

DevBoard V1 is a **local-first development status aggregation and safe-navigation system**.

V1 has two capabilities:

1. **Status Display** — normalize development signals into one state model and render persistent displays.
2. **Safe Navigation** — accept only allow-listed navigation intents and resolve them to trusted server-owned targets.

First-class display targets include old Kindle browsers, phones, tablets, and desktop browsers.

Allowed V1 navigation actions:

- `focus_app`
- `focus_agent`
- `focus_project`
- `open_project`

Execution-changing actions are out of V1, including `approve`, `deny`, `stop`, `retry`, `send_prompt`, `execute_shell`, arbitrary commands, client-supplied AppleScript, executable paths, and arbitrary client-supplied URLs/applications.

Navigation failure must never change agent activity, outcome, freshness, or alert lifecycle.

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

Kindle touch, phone, keyboard, and future MX Master 4 emit the same `NavigationIntent`. Provider payloads are input facts only, never public State Authority.

## 3. State naming and authority

M0.1 freezes three distinct state concepts.

### 3.1 InternalRootState

`InternalRootState` is the State Core in-process/snapshot authority. It may contain private data required for collection and trusted navigation resolution, including `worktreeRoot`, private app/project references, opaque `focusLocator`, and internal snapshot metadata.

`Docs/contracts/root-state-v1.example.json` is explicitly an **InternalRootState example** despite its historical filename. It contains:

```json
"stateKind": "internal"
```

It is never a valid `/api/state` response.

### 3.2 PublicState

`PublicState` is the explicit allow-listed projection returned by:

```text
GET /api/state
```

Reference example:

```text
Docs/contracts/public-state-v1.example.json
```

It contains:

```json
"stateKind": "public"
```

M1 MUST construct PublicState explicitly. It MUST NOT serialize InternalRootState and then remove a few known fields.

PublicState MUST NOT expose:

- `worktreeRoot`;
- `focusLocator`;
- absolute filesystem paths;
- API keys, OAuth tokens, credentials, or navigation secrets;
- raw prompts, responses, transcripts, hook payloads, or arbitrary environment variables;
- private `NavigationTarget.detail`.

PublicState MAY expose safe navigation summaries containing only:

- `targetId`;
- `kind`;
- `allowedActions`.

### 3.3 DisplayMeta / root meta

`PublicState.meta` is a defined **DisplayMeta** object, not an arbitrary extension bag and not a copy of internal metadata.

V1 DisplayMeta fields:

- `displayContractVersion` — display metadata shape version;
- `kindleRefreshSeconds` — server-selected Kindle GET refresh interval;
- `completeHighVisibilitySeconds` — completion emphasis duration;
- `completeRetentionSeconds` — completion retention duration;
- `safeNavigationEnabled` — whether V1 safe navigation capability is exposed by the runtime;
- `wakeLockMode` — V1 value `best-effort`.

These are safe presentation/capability hints, not Agent lifecycle authority. Internal-only metadata uses a distinct `internalMeta` key and is not projected by name matching.

## 4. Host identity

State supports `host.id` and `host.displayName`. Every internal `NavigationTarget` is host-bound through `hostId`.

V1 runtime may support one local host only, but contracts must not assume a permanent singleton. Future Hub → multiple Mac nodes must remain possible without redesigning target identity.

## 5. Agent state: three dimensions

### Activity

Authoritative values:

- `idle`
- `working`
- `attention`
- `error`

### Outcome

Authoritative values:

- `none`
- `completed`
- `failed`

`COMPLETE` is derived when activity is `idle` and a recent outcome is `completed`; it is not authoritative activity.

### Freshness

Authoritative values:

- `fresh`
- `stale`

`AgentState.freshness` means confidence/age of **one agent's remembered lifecycle state**. It never overwrites activity/outcome.

```text
activity = working
freshness = stale
→ STALE · was WORKING
```

`DisplayStatus` is derived only and is never persisted as business authority.

## 6. Top-level turn semantics

A V1 task means exactly **one top-level agent turn initiated by a user prompt**. DevBoard does not infer project, milestone, PR, or business-task completion.

| Event | Reduction |
|---|---|
| `UserPromptSubmit` | begin new top-level turn → `working + none` |
| `PermissionRequest` | current turn → `attention` |
| `PostToolUse` | current turn → `working` |
| reliable `Stop` | current turn → `idle + completed` |
| fatal `StopFailure` | current turn → `error + failed` |
| `SessionEnd` | session-scoped → `idle`; preserve recent outcome |

Only a begin-turn event may replace current turn identity. Recoverable tool failure is not terminal ERROR. Subagent lifecycle cannot complete the parent top-level turn.

`SessionEnd` does not immediately erase a recent `completed` or `failed` outcome and resolves waiting attention for that session.

## 7. Normalized AgentEvent V1

Envelope fields:

- `schemaVersion`
- `eventId`
- `provider`
- `sessionId`
- `turnId`
- `eventType`
- `occurredAt`
- `cwd`
- sanitized allow-listed `metadata`

Initial providers: `codex`, `claude-code`.

Canonical agent/session identity:

```text
<provider>:<sessionId>
```

### 7.1 Nullability

Frozen types:

```text
turnId: string | null
cwd: string | null
```

A valid lifecycle event MUST NOT be rejected merely because that provider/event does not supply a turn ID or working directory.

`cwd`, when present, is private ingestion data. Public projection exposes sanitized project/worktree identity instead of the path.

### 7.2 Turn-scoped events

Turn-scoped lifecycle events are intended to mutate one top-level turn.

- include non-null `turnId` when the provider reliably supplies it;
- mutate current turn only when identity/order rules establish the event belongs to current turn;
- a null `turnId` is schema-valid, but reducers MUST NOT guess a turn identity;
- if safe attribution is impossible, retain only explicitly safe session-level facts and/or lower affected AgentState freshness/capability; never fabricate completion.

### 7.3 Session-scoped events

Session-scoped events may legitimately have `turnId = null`. `SessionEnd` is the canonical V1 example and may also have `cwd = null`.

They reduce using canonical session identity + session ordering. They do not create or replace current turn identity.

Examples:

- `agent-event-v1.example.json` — turn-scoped event;
- `agent-event-session-v1.example.json` — session-scoped `SessionEnd` with null `turnId/cwd`.

## 8. Event idempotency and ordering

Reducers must satisfy:

1. duplicate `eventId` = no-op;
2. only begin-turn may replace current turn identity;
3. after turn B becomes current, turn A events cannot mutate B;
4. delayed duplicate/older begin cannot replace a newer current turn;
5. older current-turn events cannot roll state backward;
6. a session event may apply only when session ordering permits and never manufactures a turn identity;
7. repeated notifications deduplicate through stable alert identity;
8. subagent lifecycle cannot begin/complete parent turn.

Clock/identity uncertainty changes confidence; it never justifies fabricated completion.

## 9. Provider reduction

Claude Code reductions include:

- `AskUserQuestion` / `Elicitation` / permission or elicitation notification → `attention`;
- `PostToolUseFailure`, `PermissionDenied`, `ElicitationResult` → non-terminal `working` semantics where applicable;
- fatal `StopFailure` → `error + failed`.

Codex adapters consume only lifecycle facts actually supplied by the installed/runtime version. The contract does not assume every version emits every lifecycle field/event.

If reliable completion cannot be observed:

- do not synthesize `completed`;
- lower affected AgentState confidence as appropriate;
- set provider SourceHealth `degraded` only when collector/provider capability or transport itself is degraded.

Normal silence after a completed/idle session does **not** degrade the entire Codex hook source.

## 10. Local hook ingestion boundary

M2 is expected to use a host-local helper/adapter boundary and Unix-domain ingestion where practical.

M0 freezes only that provider hooks are adapters, raw payloads are sanitized before normalized storage, hook ingestion cannot become arbitrary execution transport, and implementation belongs to M2.

No hook is installed during M0/M0.1.

## 11. Generic NavigationTarget V1

M0.1 freezes one generic server-owned target registry for every V1 safe-navigation action.

Internal shape:

- `targetId`
- `kind`
- `hostId`
- `allowedActions`
- `detail` — private kind-specific reference/locator data

Initial `kind` values:

- `agent`
- `project`
- `app`

Action matrix:

| kind | allowed actions |
|---|---|
| `agent` | `focus_agent` |
| `project` | `focus_project`, `open_project` |
| `app` | `focus_app` |

A target may expose a stricter subset, never an action outside the V1 allow-list.

### 11.1 Private detail

`detail` is server-owned and is never constructed by clients.

For `kind=agent`, detail is the **AgentTarget** concept and may contain canonical agent identity, provider/session/optional turn identity, project/worktree references, preferred app, and opaque `focusLocator`.

For `kind=project`, detail may contain `projectId`, `worktreeId`, private `worktreeRoot`, preferred app, and opaque locator.

For `kind=app`, detail may contain a trusted server configuration reference such as `appRef` and opaque locator.

### 11.2 Public target summary

Public projection may expose only:

```text
targetId
kind
allowedActions
```

Clients submit only `action + targetId`. They never construct `hostId`, detail, path, locator, command, AppleScript, executable, or URL.

Reference internal examples:

```text
Docs/contracts/navigation-target-v1.example.json
```

It includes synthetic agent, project, and app targets, so all four V1 actions are expressible without inventing a new target contract in M5.

## 12. AgentTarget relationship

After M0.1, `AgentTarget` means specifically the private agent-specific detail of:

```text
NavigationTarget(kind = agent)
```

It is not the generic navigation envelope. App/project navigation therefore does not depend on agent-only fields.

## 13. NavigationIntent V1

Fields:

- `schemaVersion`
- `requestId`
- `action`
- `targetId`
- `source`
- `requestedAt`

Allowed actions: `focus_app`, `focus_agent`, `focus_project`, `open_project`.

Recognized sources include `kindle`, `web`, `phone`, `keyboard`, and future `mx-master-4` (contract-only in V1).

The client never submits executable path, shell string, arbitrary command, AppleScript, arbitrary application locator, or arbitrary URL.

## 14. NavigationResult V1

All externally exchanged V1 envelopes are versionable.

Fields:

- `schemaVersion`
- `requestId`
- `status`
- `resolvedTarget`
- `message`
- `completedAt`

Statuses:

- `accepted`
- `completed`
- `unavailable`
- `unsupported`
- `failed`

`resolvedTarget` is a sanitized summary only; it never contains private target detail.

## 15. Navigation router and security

```text
NavigationIntent
    ↓ schema/action/source/size validation
Target Resolver
    ↓ trusted targetId lookup
allowedActions check
    ↓
host/capability check
    ↓
Host Adapter
    ↓
sanitized NavigationResult
```

Unknown, expired, wrong-kind, or action-incompatible targets are rejected. The router never interprets `targetId` as path, URL, command, application identifier, or AppleScript.

V1 read surface includes `/health`, `/api/state`, `/display`, `/display/kindle`.

A dedicated allow-listed navigation POST endpoint is implemented later. It validates method, request size, schema/version, action/source, known target, `allowedActions`, host/capability, same-origin/auth policy, and replay identity.

No wildcard CORS. No generic execution endpoint.

Intended V1 LAN mechanism:

**per-install random long-lived navigation secret + same-origin browser flow**.

The long-lived secret is server-owned, not logged, not exposed in PublicState, not placed in URL/query parameters, and never used as client target detail.

## 16. Kindle POST / Redirect / GET invariant

Old Kindle uses meta refresh, so a navigation POST MUST NOT directly render an auto-refreshing dashboard.

Mandatory flow:

```text
POST NavigationIntent form
        ↓
validate + replay/dedup check
        ↓
perform or reuse one navigation result
        ↓
redirect
        ↓
GET /display/kindle
```

Frozen invariant:

> **navigation side effect occurs at most once; browser refresh/reload is always GET.**

Exact `303` versus `302` compatibility may be tested later against old Kindle browsers; the PRG invariant does not change.

### 16.1 Replay protection

POST semantics include:

- stable `requestId` as idempotency key;
- server-issued one-time nonce/replay token carried in POST body;
- nonce binding/validation sufficient to prevent changing intended action/target without server validation.

First valid submission validates request/nonce/target, records or consumes replay identity, performs navigation at most once, stores a bounded sanitized result, then redirects.

Duplicate submission with the same `requestId` MUST NOT execute again; it reuses the prior bounded result semantics and redirects.

Reuse of a consumed one-time nonce with a different request is rejected.

The one-time nonce is not the long-lived navigation secret. The long-lived secret never appears in URL/query params.

## 17. System metrics

Primary local V1 collection uses an embedded mature Go system metrics library, preferably `gopsutil` or equivalent after implementation audit.

Glances is not a mandatory local daemon. A future Glances adapter remains valid for remote machines, NAS, VPS, or external monitored systems.

System state models CPU, memory, swap, disk, and tracked process groups. A process group aggregates all matching PIDs.

Frozen aggregate semantics:

- memory = sum of matched resident memory bytes;
- CPU = sum of matched process CPU values using one documented library/unit convention;
- unavailable metric = `null`, never fabricated as `0`.

## 18. SourceHealth versus Agent freshness

These concepts are independent.

### AgentState.freshness

Scope: **one agent/session lifecycle state**.

It describes confidence/age of the remembered state. Examples: restored active state after daemon restart, lifecycle identity uncertainty, or old active facts that cannot be trusted.

### SourceHealth

Scope: **collector/provider/transport capability**.

Fields:

- `status`: `available | degraded | unavailable`
- `lastAttemptAt`
- `lastSuccessAt`
- sanitized `message`

Examples: hook transport failure, missing provider lifecycle capability, quota adapter failure.

A completed/idle session receiving no newer lifecycle event is normal and does not by itself degrade the provider source.

**SourceHealth is not a second Agent freshness field.**

M0.1 removes per-agent `sourceHealth` from AgentState examples. Provider/collector health lives in root `sources`. Collector failures remain isolated.

## 19. Project/worktree identity

Projects may enter state from pinned configuration or agent `cwd` auto-discovery. When `cwd` is present, resolve the nearest Git worktree root.

Frozen concepts:

- `projectId`
- `displayName`
- `worktreeId`
- `worktreeRoot` — internal/private
- `repositoryIdentity`
- `branch`
- `dirty`
- `modifiedCount`
- `untrackedCount`
- `ahead`
- `behind`

Repository identity is not derived from friendly name alone. PublicState never exposes `worktreeRoot`. GitHub PR/CI is optional; local Git remains functional without `gh`.

## 20. Alert model and identity

Alert types: `attention`, `error`, `complete`, `stale`.

Every agent-related alert contains direct canonical:

```text
agentId = <provider>:<sessionId>
```

`turnId` is `string | null`; session-scoped alerts may use null.

`alertId` remains an opaque stable identifier. Consumers MUST NOT parse `alertId` to discover provider or agent identity.

Stable deduplication key semantics:

```text
type + agentId + turnId (when applicable)
```

Display priority: ATTENTION, ERROR, STALE ACTIVE, COMPLETE, WORKING, INFO.

Attention resolves when the same turn resumes/stops, a newer turn starts in the session, or the session ends. Error persists only for terminal/fatal failure. Completion defaults to 10 minutes high visibility, 30 minutes retention; `SessionEnd` does not immediately remove it.

## 21. Kindle display contract

Endpoint:

```text
GET /display/kindle
```

Requirements: server-rendered HTML, basic CSS, black/white high contrast, large touch targets, meta refresh, explicit status text, no modern-JS dependency.

Must not require Fetch, Promise, EventSource, WebSocket, CSS Grid, Canvas, SVG animation, React, or Vue.

Side-effecting navigation uses conventional server-rendered POST forms and the mandatory PRG/replay rules in §16. Side-effect-free links may use `<a>`.

Support portrait/landscape without hard-coding 600×800, with explicit fallback query values `layout=portrait|landscape`.

Do not assume browser toolbar/menu can be hidden. Kindle jailbreak/fullscreen and device anti-sleep settings are outside DevBoard runtime authority.

## 22. Modern display contract

`GET /display` may use responsive CSS, small vanilla JS, SSE, and Screen Wake Lock API. Wake Lock is best effort only and must not be presented as guaranteed no-sleep behavior.

## 23. Persistence and restart

V1 persistence:

```text
memory
+
atomic state snapshot
```

No database.

Snapshots may restore recent outcomes/alerts, known projects/worktrees, and source timestamps.

After daemon restart, previously active `working/attention` state is restored as `freshness = stale` until a new lifecycle event confirms it. This does not automatically make provider SourceHealth degraded.

TTL/retention is timestamp-based. `elapsedSeconds` is derived, not persisted as authority.

## 24. Public-state sanitization invariant

```text
InternalRootState
    ↓ explicit allow-list projection
PublicState
    ↓
GET /api/state + Display
```

PublicState never exposes secrets/tokens/credentials, prompt/response/transcript content, raw hook payload, arbitrary env, absolute paths, `worktreeRoot`, `focusLocator`, or private NavigationTarget detail.

Navigation public data is summary-only.

## 25. Contract examples

All values are synthetic.

- `Docs/contracts/root-state-v1.example.json` — **InternalRootState only**;
- `Docs/contracts/public-state-v1.example.json` — `/api/state` PublicState;
- `Docs/contracts/agent-event-v1.example.json` — turn-scoped event;
- `Docs/contracts/agent-event-session-v1.example.json` — null-turn session-scoped `SessionEnd`;
- `Docs/contracts/navigation-target-v1.example.json` — private generic agent/project/app target registry;
- `Docs/contracts/navigation-intent-v1.example.json`;
- `Docs/contracts/navigation-result-v1.example.json`.

## 26. Milestone boundary

- **M0 Contract Freeze / M0.1 closure** — documentation, architecture reconciliation, synthetic contracts only.
- **M1 Core + State + Mock Display** — Go skeleton, config, state store, mock states, explicit PublicState projection, `/health`, `/api/state`, `/display`, `/display/kindle`; **no navigation runtime**.
- **M2 Agent Event Ingestion** — CLI helper, Unix socket, reducers, Codex, Claude, alert engine.
- **M3 System Metrics** — embedded local metrics collector and process groups.
- **M4 Project / Worktree** — Git discovery, pinned project, cwd discovery, local status.
- **M5 Safe Navigation** — NavigationIntent/Result, generic NavigationTarget resolver, replay-safe web navigation, macOS `focus_app`, `focus_project`, `open_project`, `focus_agent` adapters.
- **M6 Optional Quota** — CodexBar adapter, independent SourceHealth.
- **M7 Production Runtime** — launchd, atomic snapshot, log retention, startup checks, graceful shutdown.

Future V2: execution-changing Control Layer, MX Master 4, Action Ring, haptics, keyboard backlight, multi-host node transport, approve/stop/retry.

## 27. M0.1 closure

M0/M0.1 contains no production Go server, HTTP runtime, collectors, hook installation, navigation runtime, window switching, MX Master 4 integration, or execution-changing controls.

M0.1 closes:

- InternalRootState versus PublicState;
- DisplayMeta;
- generic NavigationTarget for agent/project/app;
- AgentEvent `turnId/cwd` nullability and event scope;
- Kindle PRG/replay invariant;
- direct alert `agentId`;
- SourceHealth versus Agent freshness;
- NavigationResult `schemaVersion`.

Material decisions required before M1 are closed. Runtime implementation details explicitly assigned to later milestones are not unresolved M0 business decisions.

**M0 CONTRACT FROZEN**
