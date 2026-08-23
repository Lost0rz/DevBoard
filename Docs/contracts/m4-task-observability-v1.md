# DevBoard M4 — AI Task Observability Technical Contract v1

Date: 2026-08-22

Base: `aec946f3f06fff63285bbc26375e193ff9b8e5f8`

Parent contracts:

- `Docs/contracts/mvp-monitoring-v1.md`
- `Docs/contracts/mvp-feature-freeze-v1.md`
- `Docs/contracts/agent-task-observability-v1.md`
- `Docs/contracts/reference-first-integration-v1.md`
- `Docs/contracts/m4-task-observability-reference-audit-v1.md`

Status: **M4 TECHNICAL CONTRACT FROZEN**

This contract is architecture only. It does not authorize runtime implementation on the contract-freeze branch.

---

## 1. Objective and scope boundary

M4 answers:

> What is each AI coding task doing now, where is it running, does it need me, and what did it finish?

M4 is limited to:

- safe per-turn task identity;
- safe project/worktree identity;
- deterministic bounded task title;
- latest meaningful checkpoint;
- bounded actionable attention;
- bounded completion delivery;
- honest capability/freshness degradation.

M4 does not add:

- multi-host transport or aggregation;
- Browser AI Watch;
- quota collection;
- navigation/control/approval response;
- Process Groups;
- transcript storage;
- terminal scraping;
- generic provider polling;
- database/event sourcing;
- LLM summarization service;
- Kindle redesign.

---

## 2. AgentState vs TaskState authority

### Frozen decision

Keep `AgentState` as the provider-session lifecycle authority and add an additive `TaskState` presentation/retention layer.

There is only one lifecycle reducer authority.

```text
provider hook
→ provider allow-list adapter
→ normalized significant event
→ ONE reducer authority
   ├─ authoritative AgentState transition
   └─ TaskState presentation update from the same transition
→ explicit PublicState projection
```

Rules:

1. `TaskState.Lifecycle` mirrors the lifecycle decision produced by the existing reducer authority.
2. TaskState must not independently reinterpret the same event into a conflicting lifecycle.
3. One accepted normalized event produces at most one `Store.Update` transaction containing both AgentState and TaskState changes.
4. No asynchronous second reducer may race AgentState.
5. Existing AgentState behavior remains available for backward compatibility and the frozen Kindle path.

Reason: AgentState is provider-session/current-turn lifecycle state. M4 additionally needs recent per-turn delivery state, title, project context, checkpoint, attention, and completion. Mixing those concerns into AgentState would create competing retention/authority semantics.

---

## 3. Normalized task identity

A top-level task instance is one provider top-level turn/work item.

### Internal source key

Preferred private key:

```text
provider + session_id + authoritative turn identity
```

Provider rules:

- Codex: authoritative turn identity is documented `turn_id`.
- Claude Code: authoritative turn identity is `prompt_id` when available.
- Claude installations without `prompt_id` may use the existing synthetic identity created from the authoritative UserPromptSubmit event, with degraded confidence.
- A later malformed event without reliable turn identity is never guessed onto an unrelated task.

### Public task identity

Public TaskState uses a DevBoard-generated opaque task ID.

Do not expose provider session, turn, prompt, native task, or child-agent IDs as primary labels.

### Multiple turns and sessions

- A new authoritative UserPromptSubmit with a new turn identity creates a new TaskState.
- The previous completed TaskState may remain only for bounded delivery retention.
- `AgentState.CurrentTurn` advances normally.
- Multiple Codex sessions, multiple Claude sessions, Codex + Claude, and multiple worktrees must remain independent.
- Provider name alone is never a task key.

---

## 4. Host boundary

M4 does not implement M5 transport.

The existing root `HostState` already scopes each local snapshot. Therefore M4 does not duplicate host identity into every local TaskState and does not add:

- host registry;
- peer discovery;
- remote aggregation;
- task transport.

M5 may later aggregate complete local snapshots under their root host identity without changing M4 task semantics.

---

## 5. Project / worktree identity

Provider `cwd` is private input only.

```text
private cwd
→ local Git/worktree resolver
→ private canonical ProjectContext
→ bounded sanitized PublicProjectContext
```

### Git resolution

When cwd exists, resolve identity locally with direct argument execution, never shell interpolation. Equivalent operations may include:

```text
git -C <cwd> rev-parse --show-toplevel
git -C <cwd> rev-parse --git-common-dir
git -C <cwd> rev-parse --git-dir
git -C <cwd> branch --show-current
```

M4 must not run full Git status/diff/log analytics merely for identity.

### Canonicalization

Privately:

1. clean/absolute path;
2. resolve symlinks when possible;
3. use Git canonical worktree/root facts when available;
4. preserve a private identity that distinguishes multiple worktrees.

Never project the canonical absolute path.

### Public fields and bounds

A task may expose:

- `projectName`: max 80 UTF-8 bytes;
- `worktreeLabel`: optional, max 80 UTF-8 bytes;
- `branch`: optional, max 120 UTF-8 bytes.

All labels must be valid UTF-8, control-free, bounded, and must not be absolute-path-shaped.

### Edge cases

- Multiple worktrees of one repo remain distinct internally; safe branch/worktree labels may distinguish them publicly.
- Detached HEAD is valid; branch may be absent.
- Non-Git directory fallback is sanitized `basename(cwd)` with no fabricated Git data.
- Deleted/inaccessible/malformed cwd does not invalidate lifecycle; project context becomes unavailable.
- Systematic resolver failure may degrade SourceHealth; one transient lookup failure does not.
- Existing broader `ProjectState`/Git dashboard semantics are not activated or expanded by M4.

---

## 6. Task title policy

Frozen priority:

1. verified provider-native top-level task/session title;
2. verified provider-native top-level task subject;
3. explicit user-defined label, if one exists in a future supported path;
4. deterministic bounded title derived from UserPromptSubmit prompt;
5. `Project · Provider` fallback;
6. `Codex task` / `Claude Code task` fallback.

Current audited hooks do not provide a reliable provider-native top-level title. Claude `TaskCreated.task_subject` is a child/native-task subject and does not replace the root title.

Therefore source #4 is required in M4.

### Raw prompt rule

- Raw prompt is never normalized into AgentEvent/TaskState.
- It is never persisted or logged.
- Title derivation may inspect at most the first 8 KiB of UserPromptSubmit prompt.
- Raw prompt is discarded immediately after derivation.

### Deterministic derivation

No LLM call is allowed.

1. Normalize line breaks and repeated whitespace.
2. Remove control characters and simple Markdown heading/list decoration.
3. Select the first short natural-language-looking line/sentence.
4. Reject unsafe candidates rather than attempting clever redaction.
5. Bound final title to 96 UTF-8 bytes.

Fallback instead of deriving when the candidate is primarily:

- fenced source code;
- shell/command text;
- an absolute path;
- credential-bearing URL or sensitive query string;
- PEM/private-key material;
- bearer/API/token/password/secret-like data;
- long opaque/high-entropy token text;
- large pasted JSON/blob/log;
- non-natural-language content with no safe short label.

---

## 7. Checkpoint taxonomy

Frozen kinds:

```text
started
inspecting
editing
running
validating
delegated
subtask_completed
background_wait
```

Do not create percentage progress, ETA, reasoning-stage labels, or fabricated `finalizing`.

Completion is a lifecycle/delivery state, not a checkpoint.

### Tool mapping

Classification uses provider tool type/name only, never arbitrary command text.

| Tool family | Checkpoint |
|---|---|
| read/search/list/grep/glob/local-browse style | `inspecting` |
| edit/write/apply_patch style | `editing` |
| explicit provider tool whose name itself denotes test/build/lint/vet/validation | `validating` |
| generic shell/Bash/exec/command | `running` |
| unknown tool | `running` |

A Bash command containing `go test`, `npm test`, `git diff`, or similar remains `running`; M4 does not inspect command text merely to infer progress.

### Provider-native mapping

| Signal | Checkpoint |
|---|---|
| UserPromptSubmit | `started` |
| PreToolUse | mapped from tool type/name |
| SubagentStart | `delegated` |
| SubagentStop | `subtask_completed` |
| Claude TaskCreated | `delegated` |
| Claude TaskCompleted | `subtask_completed` |
| Claude Stop with background tasks/session crons remaining | `background_wait` |

Public checkpoint text is template-driven or uses one explicitly allowed bounded child subject. Maximum checkpoint text is 120 UTF-8 bytes.

Forbidden checkpoint content:

- raw command;
- tool input/output;
- transcript content;
- child final assistant response.

---

## 8. Checkpoint priority and replacement

| Priority | Kind |
|---:|---|
| 60 | `background_wait` |
| 50 | `subtask_completed` |
| 45 | `delegated` |
| 40 | `validating` |
| 30 | `editing`, `inspecting` |
| 10 | `running` |
| 0 | `started` |

Rules:

1. same/higher priority may replace immediately;
2. lower priority normally waits until the current higher-value checkpoint is at least 30 seconds old;
3. authoritative resumed activity after `background_wait` may replace it immediately;
4. ATTENTION visually outranks checkpoints;
5. ERROR and COMPLETE visually outrank checkpoints;
6. frequent low-value PostTool/heartbeat-like events may refresh time without erasing a meaningful checkpoint.

All priority behavior is deterministic and fake-clock testable.

---

## 9. ATTENTION semantics

### Public attention kinds

```text
approval_needed
question_waiting
elicitation_waiting
authentication_required
billing_required
rate_limited
provider_action_required
```

Maximum public attention text: 160 UTF-8 bytes.

### Codex sources

- PermissionRequest → `approval_needed`.

Current audited Codex hooks do not provide a separate authoritative question/elicitation event comparable to Claude. M4 does not fabricate one.

### Claude sources

- PermissionRequest → `approval_needed`.
- PreToolUse with exact `tool_name == "AskUserQuestion"` → `question_waiting`.
- Elicitation → `elicitation_waiting`.
- Safe actionable Notification classes may become `provider_action_required` when they represent current user input/approval demand.
- StopFailure `authentication_failed` or `oauth_org_not_allowed` → ERROR plus `authentication_required` feedback.
- StopFailure `billing_error` → ERROR plus `billing_required` feedback.
- StopFailure `rate_limit` → ERROR plus `rate_limited` feedback.

### Not automatically ATTENTION

- one PostToolUseFailure;
- one nonzero shell exit;
- PermissionDenied by itself;
- unknown notification type;
- generic provider warning text;
- SourceHealth degradation by itself.

### Sticky and clearing rules

ATTENTION is sticky against unrelated low-value activity and clears only on deterministic resolution within the same top-level task:

- approval: subsequent authoritative same-turn progress showing the provider moved beyond the request, or terminal Stop/SessionEnd;
- AskUserQuestion: matching/same-turn tool completion or authoritative later same-turn progress, or terminal Stop/SessionEnd;
- Elicitation: ElicitationResult or terminal Stop/SessionEnd;
- generic actionable Notification: subsequent authoritative same-turn progress or terminal Stop/SessionEnd;
- ERROR/COMPLETE replaces ATTENTION lifecycle; a terminal ERROR may retain bounded actionable feedback.

Use provider correlation IDs privately when available. Without one, only a strictly later event for the same task may clear attention.

When existing freshness maintenance marks the authoritative task stale, clear active actionable attention and present stale/unknown confidence instead of claiming the request remains active forever.

---

## 10. ERROR semantics

ERROR is task-terminal, not tool-local.

ERROR may come only from:

- authoritative terminal provider failure such as Claude StopFailure;
- a future explicitly audited provider terminal-failure event;
- existing equivalent authoritative reducer terminal semantics.

ERROR must not come from:

- recoverable PostToolUseFailure;
- nonzero Bash exit alone;
- permission denial alone;
- malformed monitoring event;
- DevBoard hook/socket failure.

Monitoring failures degrade SourceHealth and remain fail-open to the coding agent.

---

## 11. Completion summary derivation

Current audited Codex and Claude Stop events expose final user-visible `last_assistant_message`.

M4 may inspect that field transiently only when the top-level task is eligible for COMPLETE.

Claude Stop with nonzero `background_tasks` or `session_crons` remains WORKING + `background_wait`; its current message is discarded instead of being persisted as premature completion. A later true terminal Stop may produce completion delivery.

Subagent final messages are never used for root completion delivery.

### Input and output bounds

- inspect at most first 16 KiB of final visible message;
- public summary max 320 UTF-8 bytes total;
- max 3 non-empty lines;
- each selected line max 160 UTF-8 bytes before total bound;
- optional result identifier max 96 UTF-8 bytes.

### Deterministic extraction

No LLM summarization.

When safely and explicitly present, retain only:

1. first useful result statement;
2. one validation/test/result statement;
3. one important limitation/blocker statement;
4. one clearly recognizable safe result identifier such as an explicit branch label or 7–40 hex Git SHA.

Never invent results.

Reject lines dominated by:

- code/fenced source;
- shell command text;
- stack trace;
- absolute path;
- environment/config dump;
- secret/token/password/API key;
- credential-bearing URL or sensitive query string;
- large JSON/tool output;
- transcript content.

`COMPLETE` with no safe completion summary is valid.

A retained phrase such as `tests pass` is provider-reported text only. DevBoard must not infer test success from a checkpoint.

Raw final text is discarded immediately after extraction and never logged, normalized, persisted, or projected.

---

## 12. Subagent and provider-native task semantics

Subagents and Claude native Task nodes do not become top-level M4 cards.

The user's top-level coding turn remains the board identity.

### Codex

- SubagentStart → parent `delegated`.
- SubagentStop → parent `subtask_completed`.
- private `agent_id` may correlate/dedupe.
- bounded `agent_type` may influence a template label.
- child transcript path and child final message are discarded.
- inner child tool events do not replace parent checkpoint by default.

### Claude Code

- SubagentStart/Stop follow the same parent-checkpoint rule.
- TaskCreated/TaskCompleted are child/native task-node checkpoints.
- `task_subject` may be sanitized and retained only up to 96 UTF-8 bytes.
- `task_description` is not retained by default.
- task, child-agent, teammate, and team IDs stay private correlation data.

---

## 13. Multiple simultaneous tasks and lifecycle transitions

M4 must support simultaneous independent tasks across providers, sessions, projects, and worktrees.

Conceptual authoritative transitions:

```text
UserPromptSubmit
→ WORKING

PreToolUse / successful or recoverable tool activity
→ WORKING unless sticky ATTENTION remains unresolved

Permission / question / elicitation demand
→ ATTENTION

resolved attention + authoritative progress
→ WORKING

Claude Stop with background work remaining
→ WORKING + background_wait

terminal Stop
→ COMPLETE

terminal provider failure
→ ERROR

freshness timeout
→ stale/degraded according to existing lifecycle semantics

SessionEnd
→ session ended/resting while preserving bounded completed/failed delivery
```

Old/out-of-order events must not roll a newer task backward. Existing dedupe and monotonic-time protections remain mandatory.

---

## 14. Retention semantics

No database or durable task archive is added.

Active tasks remain only while operationally relevant under existing session/freshness behavior.

Completed TaskState reuses existing display configuration:

- `CompleteHighVisibilitySeconds`;
- `CompleteRetentionSeconds`.

Completed tasks are removable after the existing completion-retention window. M4 does not promise full session history.

---

## 15. Privacy and data bounds

### PRIVATE TRANSIENT

May be inspected only during bounded normalization:

- raw user prompt;
- full final assistant message;
- permission description/input;
- question payload;
- elicitation message/schema/URL/result;
- task description;
- tool input/output;
- shell command;
- cwd;
- transcript path;
- child final message;
- raw provider payload.

### PRIVATE NORMALIZED

May exist in bounded in-memory normalized data when required:

- provider/session/turn correlation IDs;
- synthetic-turn marker;
- safe tool name/category;
- safe notification/error enum;
- background/session-cron counts;
- private canonical project/worktree identity;
- private child correlation ID;
- capability/confidence flags.

### PUBLIC

Only:

- DevBoard task ID;
- provider;
- bounded project/worktree labels;
- bounded title;
- lifecycle/freshness/confidence;
- bounded checkpoint kind/text/time;
- bounded attention kind/text/time;
- bounded completion summary/result identifier/time;
- task timestamps.

---

## 16. Raw-data discard rules

Frozen rule:

```text
NO RAW SOURCE CONTENT PERSISTENCE
```

M4 adds no:

- transcript repository;
- prompt archive;
- full completion-response archive;
- command log;
- raw-event log;
- database.

Provider raw JSON exists only long enough for bounded parsing. Diagnostic logging must not include raw provider JSON or rejected sensitive candidate text.

---

## 17. Provider capability/version degradation

Task confidence is intentionally coarse:

```text
high
degraded
```

`high` requires reliable provider-native task correlation for the represented fact and no known material capability gap.

`degraded` applies when basic lifecycle is still useful but, for example:

- Claude uses synthetic identity because `prompt_id` is unavailable;
- installed provider lacks a required M4 enrichment event;
- malformed/missing correlation prevents reliable enrichment;
- project/worktree resolution systematically fails;
- a provider-specific detail is unavailable.

Missing richness never authorizes fabricated checkpoint/title/summary data.

Unknown future events are ignored until audited.

---

## 18. Hook event matrix

Legend:

- **KEEP**: already in M2 and required;
- **ADD**: verified current hook M4 should add;
- **OPTIONAL**: useful but not required for M4 closure;
- **N/A**: not verified for that provider;
- **REJECT**: supported but intentionally not collected.

| Event | Codex | Claude Code | M4 purpose |
|---|---|---|---|
| SessionStart | OPTIONAL | OPTIONAL | freshness/context only |
| UserPromptSubmit | KEEP | KEEP | root task start + transient title derivation |
| PreToolUse | KEEP | KEEP | coarse checkpoint; Claude question detection |
| PermissionRequest | KEEP | KEEP | ATTENTION |
| PostToolUse | KEEP | KEEP | activity/resolution/checkpoint |
| PostToolUseFailure | N/A | KEEP | recoverable tool failure |
| PermissionDenied | N/A | KEEP | denial, not whole-task ERROR |
| Notification | N/A | KEEP | safe actionable type only |
| SubagentStart | ADD | ADD | delegated checkpoint |
| SubagentStop | ADD | ADD | subtask-completed checkpoint |
| TaskCreated | N/A | ADD | Claude child task checkpoint |
| TaskCompleted | N/A | ADD | Claude child task completion |
| Stop | KEEP | KEEP | terminal completion + bounded extraction |
| StopFailure | N/A | KEEP | terminal provider ERROR |
| SessionEnd | KEEP | KEEP | session cleanup/end |
| Elicitation | N/A | KEEP | ATTENTION |
| ElicitationResult | N/A | KEEP | ATTENTION clear |
| PreCompact | not required | not required | no M4 card value |
| PostCompact | not required | not required | no M4 card value |
| MessageDisplay | N/A | REJECT | continuous assistant mirroring is out of scope |
| PostToolBatch | N/A | not required | redundant for checkpoint MVP |
| CwdChanged | N/A | OPTIONAL only if real validation proves needed | common-event cwd preferred |
| WorktreeCreate/Remove | N/A | not required | local Git resolver owns M4 identity observation |

### Minimum Codex set

```text
UserPromptSubmit
PreToolUse
PermissionRequest
PostToolUse
SubagentStart
SubagentStop
Stop
SessionEnd
```

### Minimum Claude Code set

```text
UserPromptSubmit
PreToolUse
PostToolUse
PostToolUseFailure
PermissionRequest
PermissionDenied
Notification
SubagentStart
SubagentStop
TaskCreated
TaskCompleted
Stop
StopFailure
SessionEnd
Elicitation
ElicitationResult
```

Claude AskUserQuestion remains exact tool detection from PreToolUse, not a separate event.

---

## 19. SourceHealth behavior

Existing source identities remain:

- `codex-hooks`;
- `claude-hooks`.

Do not create one SourceHealth entry per task.

Rules:

1. valid baseline lifecycle can stay available when optional richness is absent;
2. unreliable turn attribution → degraded;
3. installed version missing a required M4 enrichment signal → degraded, not unavailable;
4. malformed input is ignored/fail-open and may degrade after repeated/material evidence;
5. unknown new event is ignored, not fatal;
6. provider asymmetry never creates fake data;
7. last-success semantics stay tied to accepted valid monitoring events, not process existence.

Hook failure, daemon absence, socket failure, and monitoring bugs must never stop Codex/Claude execution.

---

## 20. PublicState additions

M4 plans one additive root field:

```text
tasks: []PublicTask
```

`schemaVersion` remains `1` under existing additive PublicState evolution semantics.

Conceptual shape:

```text
PublicTask {
  id
  provider
  project {
    projectName
    worktreeLabel?
    branch?
  }?
  title
  lifecycle
  freshness
  confidence
  startedAt
  updatedAt
  checkpoint? {
    kind
    text
    at
  }
  attention? {
    kind
    text
    at
  }
  completion? {
    summary?
    resultIdentifier?
    at
  }
}
```

PublicTask must not expose:

- provider session/turn/prompt ID;
- child/native task/subagent IDs;
- cwd/worktree root;
- `.git` paths;
- transcript path;
- raw prompt/final response;
- tool input/output;
- shell commands;
- raw provider JSON.

Existing PublicAgent remains for backward compatibility in M4.

---

## 21. Desktop display intent

M4 freezes information hierarchy only.

```text
PROVIDER · PROJECT / BRANCH
TASK TITLE
LIFECYCLE · ELAPSED
LATEST MEANINGFUL CHECKPOINT
ACTION REQUIRED   when present
COMPLETION        when present
```

Opaque IDs and absolute paths are never primary labels.

Final responsive information-density redesign is not part of this contract.

---

## 22. Kindle regression boundary

The frozen M2.3 Kindle presentation contract remains unchanged.

Therefore:

- AgentState lifecycle remains usable by the current Kindle ViewModel;
- additive TaskState does not require Kindle template changes;
- existing selection/capacity/rotation/high-visibility/retention behavior remains intact;
- no new Kindle CSS/JavaScript/markup is required for M4 closure;
- final high-density redesign remains deferred to M8.

Future M4 implementation acceptance must prove equivalent AgentState input produces unchanged frozen Kindle behavior despite TaskState enrichment.

---

## 23. Deterministic test plan

Future M4 implementation must include at least the following deterministic coverage.

### Adapter/privacy

- required Codex events including SubagentStart/Stop;
- required Claude events including Subagent and TaskCreated/Completed;
- raw prompt absent from normalized event/state/public JSON;
- raw final assistant message absent;
- tool input/output/command/transcript absent;
- elicitation schema/URL/result absent;
- background descriptions/commands and cron prompt absent;
- child final message absent;
- unknown events fail open.

### Identity/concurrency

- stable provider/session/turn private identity;
- multiple same-provider sessions independent;
- same session multiple turns produce distinct retained TaskStates;
- Codex + Claude simultaneous isolation;
- Claude synthetic identity sets degraded confidence;
- missing later turn identity is not guessed;
- concurrent event/race coverage.

### Project/worktree

- normal Git repo;
- two worktrees same repo distinct;
- branch and detached HEAD;
- symlink cwd;
- non-Git cwd;
- deleted/unavailable cwd;
- no absolute path leak;
- Git invoked without shell interpolation.

### Title

- safe natural-language derivation;
- multiline normalization;
- 96-byte UTF-8 bound;
- code/shell/path/secret/PEM/token/opaque JSON fallback;
- deterministic/no external call.

### Checkpoints

- tool family mapping;
- generic Bash remains `running` even if fixture command text says test/build;
- unknown tool → `running`;
- fake-clock priority replacement;
- low-value noise cannot immediately erase high-value checkpoint;
- background_wait clears on authoritative resume.

### Attention/error

- permission sticky ATTENTION;
- Claude AskUserQuestion;
- Elicitation/ElicitationResult set/clear;
- clearing restricted to frozen same-task rules;
- stale clears never-ending attention;
- PostToolUseFailure is not task ERROR;
- PermissionDenied is not task ERROR;
- Claude StopFailure safe terminal mapping;
- monitoring failure degrades source only.

### Completion

- safe extraction;
- 16 KiB transient input cap;
- 320-byte/3-line output bounds;
- code/stack/path/secret/command rejection;
- safe explicit SHA extraction;
- no invented validation;
- no safe line → valid nil summary;
- Claude background Stop does not complete;
- later terminal Stop completes.

### Child work

- Codex subagent events affect parent checkpoint only;
- Claude subagent/native Task events affect parent only;
- task subject bounded/sanitized;
- child IDs private;
- no child top-level card.

### State/display regression

- existing AgentState lifecycle tests remain valid;
- TaskState mirrors same reducer transition;
- one accepted event uses one Store update path;
- dedupe/out-of-order protections retained;
- complete retention deterministic;
- PublicState explicit allow-list exact;
- desktop uses bounded public data only;
- Kindle frozen regression unchanged.

---

## 24. Real Mac acceptance plan

After implementation, validate with real installed provider CLIs on macOS.

Record:

- DevBoard implementation SHA;
- Go version, GOOS/GOARCH, macOS version;
- Codex CLI version;
- Claude Code version;
- active provider hook configuration/source;
- whether every required hook is accepted by the installed provider.

Acceptance scenarios:

1. two simultaneous Codex sessions in different projects/worktrees;
2. two simultaneous Claude sessions;
3. Codex + Claude concurrently;
4. each top-level prompt creates an independent bounded title;
5. same-repo worktrees are distinguished without exposing `/Users/...`;
6. read/search/edit/generic tools create only coarse checkpoints;
7. real permission request produces sticky ATTENTION until provider advances;
8. Claude AskUserQuestion shows `Question waiting` without full question text;
9. Claude subagent and native Task node events where supported;
10. Codex subagent events;
11. Claude background Stop remains WORKING/background_wait;
12. real Codex and Claude completion produces bounded delivery;
13. `/api/state` contains no absolute cwd, prompt, raw final response, tool payload, transcript path, or child IDs;
14. stopping DevBoard/making socket unavailable does not stop either coding agent;
15. missing capability/synthetic identity degrades honestly;
16. repository validation: `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./cmd/devboard`, `git diff --check`;
17. frozen Kindle behavior remains unchanged for equivalent lifecycle fixtures.

Unavailable optional capability is not a code failure when baseline lifecycle remains correct and degradation is honest.

---

## 25. Explicit rejected approaches

M4 rejects:

- full transcript monitoring;
- provider JSONL/session-file tailing as primary source;
- terminal scraping;
- generic screen OCR;
- process-based progress inference;
- revived Process Groups;
- continuous assistant-message mirroring / Claude MessageDisplay ingestion;
- arbitrary command capture or command-text semantic parsing;
- complete tool input/result retention;
- raw prompt/title publication;
- raw final assistant response persistence;
- polling provider internals while hooks suffice;
- provider-specific mini-apps;
- generic plugin framework;
- LLM summarization for every title/checkpoint/completion;
- event-sourcing/database/queue architecture;
- multi-host networking inside M4;
- remote approval/question responses;
- M4 Kindle redesign.

---

## 26. Scope closure

Frozen M4 implementation architecture, when later authorized:

```text
CURRENT PROVIDER HOOKS
→ THIN ALLOW-LIST ADAPTERS
→ EXISTING SINGLE REDUCER AUTHORITY
→ AGENT LIFECYCLE + ADDITIVE TASK PRESENTATION STATE
→ BOUNDED PUBLIC TASKS
```

Required user value:

```text
safe identity
+ meaningful checkpoint
+ actionable attention
+ completion delivery
```

No additional subsystem is required.

`UNRESOLVED_MATERIAL_DECISIONS: NONE`

**DEVBOARD M4 AI TASK OBSERVABILITY TECHNICAL CONTRACT V1 FROZEN.**
