# DevBoard M4 — AI Task Observability Technical Contract v1

> Date: 2026-08-22  
> Base: `aec946f3f06fff63285bbc26375e193ff9b8e5f8`  
> Parent business contracts:
> - `Docs/contracts/mvp-monitoring-v1.md`
> - `Docs/contracts/mvp-feature-freeze-v1.md`
> - `Docs/contracts/agent-task-observability-v1.md`
> - `Docs/contracts/reference-first-integration-v1.md`
> Reference audit: `Docs/contracts/m4-task-observability-reference-audit-v1.md`  
> Status: **M4 TECHNICAL CONTRACT FROZEN**  
> Scope: architecture and deterministic semantics only. No runtime implementation is performed on the contract-freeze branch.

## 1. M4 objective and hard boundary

M4 implements the smallest event-driven architecture that can answer:

> What is each AI coding task doing now, where is it running, does it need me, and what did it finish?

M4 adds only:

- safe per-turn task identity;
- safe project/worktree identity;
- deterministic task title;
- latest meaningful checkpoint;
- bounded actionable attention;
- bounded completion delivery;
- provider capability/freshness degradation.

M4 does **not** add:

- multi-host transport or aggregation;
- Browser AI Watch;
- quota collection;
- navigation/control/approval responses;
- Process Groups or process resource attribution;
- transcript storage;
- terminal scraping;
- generic provider polling;
- database/event sourcing;
- LLM summarization service;
- Kindle redesign.

---

# 2. Authority decision — AgentState vs TaskState

## 2.1 Frozen decision

**Keep `AgentState` as provider-session lifecycle authority and add an additive `TaskState` presentation/retention layer.**

Do not turn `AgentState` into a second transcript-like domain object and do not create an independent lifecycle reducer.

Existing authority remains:

```text
provider event
→ provider adapter
→ normalized event
→ one reducer authority
→ authoritative AgentState lifecycle transition
```

M4 extends the same reducer transaction:

```text
provider event
        ↓
thin allow-list adapter
        ↓
normalized significant event
        ↓
ONE reducer authority
        ├─ updates AgentState lifecycle
        └─ updates TaskState presentation/retention from that same transition
                 ↓
           explicit PublicState projection
```

`TaskState.Lifecycle` is a presentation mirror of the authoritative reducer result. It MUST NOT independently infer a conflicting WORKING / ATTENTION / ERROR / COMPLETE state.

## 2.2 Why not extend AgentState only

`AgentState` currently represents one provider session and its current turn. M4 needs multiple recent top-level turns to remain separately identifiable while completion delivery is retained. Adding project identity, title, checkpoint, attention text, completion delivery, child task metadata, and retention history directly to `AgentState` would mix provider-session authority with card-delivery state and would make later multi-host aggregation harder to reason about.

## 2.3 One mutation boundary

One accepted normalized provider event MUST produce at most one `Store.Update` transaction containing both lifecycle and TaskState changes. M4 must not introduce an asynchronous second reducer that can race AgentState.

---

# 3. Normalized task identity

## 3.1 Internal source key

A top-level task instance is one provider top-level turn/work item.

Preferred source key:

```text
provider + session_id + authoritative turn identity
```

Where:

- Codex authoritative turn identity = current documented `turn_id`;
- Claude authoritative turn identity = `prompt_id` when available;
- Claude installations lacking `prompt_id` may use the existing synthetic turn identity for the initial `UserPromptSubmit`, but confidence is degraded;
- a malformed later event without reliable turn identity is never guessed onto an unrelated active task.

## 3.2 Public task ID

Public TaskState uses a DevBoard-generated opaque task ID. It MUST NOT use the raw provider session/turn ID as the primary card identity.

The private correlation tuple remains in process memory only.

## 3.3 Multiple turns in one session

A new authoritative `UserPromptSubmit` with a new turn identity creates a new TaskState. The previous completed TaskState may remain visible only under bounded completion retention. `AgentState.CurrentTurn` advances normally.

## 3.4 Multiple simultaneous tasks

The model MUST independently support:

- multiple Codex sessions on one host;
- multiple Claude sessions on one host;
- Codex and Claude simultaneously;
- multiple worktrees of the same repository;
- multiple recently completed task instances while active tasks continue.

Provider name alone is never a unique task key.

---

# 4. Host boundary

M4 does not add a host registry or transport.

The existing root `HostState` already scopes every local task snapshot. M5 can aggregate whole local PublicState snapshots under their host identity without M4 adding a duplicate `hostId` to every local `TaskState`.

Therefore:

- no new network transport;
- no remote registry;
- no peer discovery;
- no task-level host duplication required in M4 internal state.

A future M5 aggregator may attach/root-scope host identity during aggregation without changing M4 task semantics.

---

# 5. Project / worktree identity

## 5.1 Source

Provider `cwd` remains **PRIVATE TRANSIENT/PRIVATE NORMALIZED INPUT**. It is never projected directly.

Conceptual flow:

```text
private cwd
→ local resolver
→ private canonical ProjectContext
→ sanitized PublicProjectContext
```

## 5.2 Git resolver

When cwd exists, M4 resolves project/worktree context locally using direct argument execution, never shell interpolation. The resolver may use equivalent Git commands such as:

```text
git -C <cwd> rev-parse --show-toplevel
git -C <cwd> rev-parse --git-common-dir
git -C <cwd> rev-parse --git-dir
git -C <cwd> branch --show-current
```

The resolver MUST NOT run full Git status/diff/log analytics merely for M4 identity.

## 5.3 Canonicalization

Private path handling:

1. clean/absolute path privately;
2. resolve symlinks when possible;
3. use Git's canonical worktree/root facts when available;
4. never return the canonical absolute path to PublicState.

If symlink resolution or cwd lookup fails, lifecycle ingestion still proceeds.

## 5.4 Public identity fields

A task may expose only bounded sanitized project context:

- `projectName` — repository/root basename or non-Git cwd basename;
- `worktreeLabel` — optional bounded distinguishing label;
- `branch` — optional bounded current branch when Git reports one.

Public project strings are labels, not authoritative filesystem addresses.

Hard limits:

- `projectName`: 80 UTF-8 bytes;
- `worktreeLabel`: 80 UTF-8 bytes;
- `branch`: 120 UTF-8 bytes.

Control characters are removed; invalid UTF-8 is rejected; absolute-path-shaped output is not permitted.

## 5.5 Multiple worktrees

Two worktrees of one repository remain distinct internally by their private canonical worktree roots / derived private identity. They may share `projectName` while differing by safe branch/worktree label.

## 5.6 Detached head

Detached HEAD is valid. `branch` may be absent. A safe worktree basename may be used as `worktreeLabel`; no commit SHA needs to be shown merely to establish project identity.

## 5.7 Non-Git directories

Fallback:

```text
sanitized basename(cwd)
```

with no fabricated branch or repository identity.

## 5.8 Missing/deleted cwd

If cwd is unavailable, deleted, inaccessible, or malformed:

- the task still exists;
- project context becomes unavailable;
- SourceHealth may degrade only if this is systematic rather than one transient lookup failure;
- title falls back to provider-based identity when no safe project label exists.

## 5.9 Existing ProjectState

M4 does not activate or expand the broader existing `ProjectState`/Git dashboard concept. The task card receives only the minimum sanitized ProjectContext required to identify where work belongs.

---

# 6. Task title policy

## 6.1 Frozen priority

Use the first safe available source in this order:

1. verified provider-native top-level task/session title;
2. verified provider-native top-level task subject;
3. explicit user-defined label, if DevBoard later receives one;
4. deterministic bounded sanitized title derived from `UserPromptSubmit.prompt`;
5. `Project · Provider` fallback;
6. `Codex task` / `Claude Code task` fallback when project is unavailable.

Current audited Codex/Claude hooks do not provide a reliable provider-native **top-level** title. Claude `TaskCreated.task_subject` is a child/native-task checkpoint, not the root card title.

Therefore **source #4 IS implemented in M4**.

## 6.2 Raw prompt handling

Raw prompt is never normalized into AgentEvent/TaskState and never persisted.

Adapter-side title derivation may inspect at most the first **8 KiB** of `UserPromptSubmit.prompt`. After title derivation the raw prompt buffer is discarded under the existing hook process lifetime.

## 6.3 Deterministic derivation

No LLM call is permitted for task naming.

Derivation:

1. normalize line breaks and repeated whitespace;
2. remove control characters and simple Markdown heading/list decoration;
3. select the first short natural-language-looking line/sentence;
4. reject rather than reinterpret unsafe candidates;
5. truncate safely to **96 UTF-8 bytes**.

## 6.4 Conservative rejection

Prompt-derived title MUST fall back instead of publishing a candidate when the candidate is primarily:

- fenced source code;
- shell/command text;
- an absolute filesystem path;
- URL with credentials or query secrets;
- PEM/private-key material;
- bearer/API/token/password/secret-like credential material;
- long opaque/high-entropy token text;
- a large pasted JSON/blob/log;
- a line with no reasonable natural-language label.

M4 does not attempt clever redaction of arbitrary secrets. If uncertain, use the fallback title.

---

# 7. Checkpoint taxonomy

M4 freezes the following normalized checkpoint kinds:

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

No percentage, ETA, reasoning-stage estimate, or generic `finalizing` state is invented.

Completion is lifecycle/completion delivery, not a checkpoint kind.

## 7.1 Tool → checkpoint mapping

Classification uses only provider tool type/name, never arbitrary command text.

Conservative mapping:

| Provider tool/type family | Checkpoint |
|---|---|
| read/search/list/grep/glob/browse-local style tools | `inspecting` |
| edit/write/apply_patch style tools | `editing` |
| explicit provider tool whose **tool name itself** denotes test/build/lint/vet/validation | `validating` |
| generic shell/Bash/exec/command | `running` |
| unknown tool | `running` |

A shell command containing `go test`, `npm test`, `git diff`, etc. is **not parsed** merely to upgrade the checkpoint. Generic Bash stays `running` unless the provider exposes a higher-level tool type.

## 7.2 Provider-native mapping

| Signal | Checkpoint |
|---|---|
| `UserPromptSubmit` | `started` |
| `PreToolUse` | tool mapping above |
| `SubagentStart` | `delegated` |
| `SubagentStop` | `subtask_completed` |
| Claude `TaskCreated` | `delegated` |
| Claude `TaskCompleted` | `subtask_completed` |
| Claude `Stop` with nonzero background task/session-cron count | `background_wait` |

## 7.3 Checkpoint text

Checkpoint text is template-driven or uses one explicitly allowed bounded child subject.

Maximum public checkpoint text: **120 UTF-8 bytes**.

Examples:

```text
Inspecting files
Editing implementation
Running tool
Running validation
Subagent started
Subtask completed · audit auth flow
Waiting for background work
```

Raw command, tool input, tool response, transcript text, and child final response are forbidden.

---

# 8. Checkpoint priority and replacement

Visible meaning outranks event frequency.

Frozen priority:

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

1. a same/higher-priority new checkpoint may replace the visible checkpoint immediately;
2. a lower-priority checkpoint normally does not replace a higher-priority checkpoint until the higher checkpoint is at least **30 seconds** old;
3. an authoritative resumption event after `background_wait` may replace it immediately;
4. ATTENTION is lifecycle/actionable state and visually outranks checkpoints regardless of checkpoint priority;
5. ERROR and COMPLETE visually outrank checkpoints;
6. frequent PostTool/heartbeat-like events may update freshness without replacing a meaningful checkpoint.

This policy is deterministic and must be unit-tested with a fake clock.

---

# 9. ATTENTION semantics

## 9.1 Public attention kinds

M4 permits only bounded normalized classes:

```text
approval_needed
question_waiting
elicitation_waiting
authentication_required
billing_required
rate_limited
provider_action_required
```

Maximum public attention text: **160 UTF-8 bytes**.

## 9.2 Events that cause ATTENTION

### Codex

- `PermissionRequest` → `approval_needed`.

Current audited Codex hooks do not expose a separate authoritative question/elicitation event comparable to Claude. M4 does not invent one.

### Claude Code

- `PermissionRequest` → `approval_needed`;
- `PreToolUse` with exact `tool_name == "AskUserQuestion"` → `question_waiting`;
- `Elicitation` → `elicitation_waiting`;
- safe actionable `Notification` classes may produce `provider_action_required` when they represent current user input/approval demand;
- `StopFailure` with safe terminal type `authentication_failed` / `oauth_org_not_allowed` → ERROR plus `authentication_required` feedback;
- `StopFailure` `billing_error` → ERROR plus `billing_required` feedback;
- `StopFailure` `rate_limit` → ERROR plus `rate_limited` feedback.

A terminal ERROR remains ERROR even when its bounded action summary describes what the user can do.

## 9.3 Events that do NOT automatically cause ATTENTION

- one `PostToolUseFailure`;
- one nonzero shell exit reported through Codex PostToolUse;
- Claude `PermissionDenied` by itself;
- unknown notification type;
- generic provider warning text;
- SourceHealth degradation by itself.

## 9.4 Sticky behavior

Once ATTENTION is established, low-value tool/background activity does not immediately clear it.

## 9.5 Clearing ATTENTION

ATTENTION clears only on a deterministic authoritative resolution:

- approval: subsequent same-turn authoritative tool progress showing the provider moved past the request, or terminal Stop/SessionEnd;
- `AskUserQuestion`: matching/same-turn tool completion or subsequent authoritative model/tool progress after the question, or terminal Stop/SessionEnd;
- Elicitation: `ElicitationResult`, or terminal Stop/SessionEnd;
- generic actionable Notification: subsequent same-turn authoritative progress, or terminal Stop/SessionEnd;
- task ERROR/COMPLETE transition replaces ATTENTION lifecycle; terminal actionable summary may remain attached to ERROR as bounded feedback.

Where a provider gives a correlation ID, use it privately. Where it does not, clearing is restricted to the same top-level task and strictly later event time.

## 9.6 Stale attention

ATTENTION must not stick forever after the provider disappears. When existing task freshness maintenance marks the authoritative current turn stale after the configured stale threshold, M4 clears active actionable attention and presents stale/unknown confidence instead. It does not claim the user is still being asked indefinitely.

---

# 10. ERROR semantics

ERROR is task-terminal, not tool-local.

Task ERROR may be established only by:

- an authoritative provider terminal failure such as Claude `StopFailure`;
- a future provider event explicitly audited and mapped as terminal failure;
- existing authoritative reducer semantics equivalent to a terminal failed turn.

Task ERROR MUST NOT be established by:

- recoverable `PostToolUseFailure`;
- nonzero Bash exit alone;
- permission denial alone;
- one malformed monitoring event;
- DevBoard hook/socket failure.

Monitoring failures degrade SourceHealth and remain fail-open to the coding agent.

---

# 11. Completion summary derivation

## 11.1 Source

Current audited Codex and Claude Stop events expose final user-visible `last_assistant_message`.

M4 may use that field **transiently only** when the top-level turn is actually eligible for COMPLETE.

Claude Stop with nonzero `background_tasks` or `session_crons` remains WORKING / `background_wait`; the current Stop message is discarded rather than stored as a premature completion summary. A later true terminal Stop may produce the summary.

Subagent final messages are not used for root completion delivery.

## 11.2 Input bound

Adapter-side completion extraction inspects at most **16 KiB** of the final visible message.

The raw message is never placed in normalized events, TaskState, logs, snapshots, or PublicState.

## 11.3 Deterministic extraction

No LLM summarization is allowed for MVP.

The extractor may retain, when safe and explicitly present:

1. first useful result statement;
2. one safe validation/test/result statement;
3. one safe important limitation/blocker statement;
4. one clearly recognizable safe result identifier such as an explicit branch name or 7–40 hex Git commit SHA.

It must not invent results or reinterpret hidden reasoning.

## 11.4 Public bounds

Completion data hard limits:

- total `summary`: **320 UTF-8 bytes**;
- maximum **3 non-empty lines**;
- each retained line: **160 UTF-8 bytes** before total bound;
- optional `resultIdentifier`: **96 UTF-8 bytes**.

## 11.5 Sanitization/rejection

Do not retain lines dominated by:

- fenced code/source blocks;
- shell command text;
- stack traces;
- absolute filesystem paths;
- raw environment/config dumps;
- secrets/tokens/password/API keys;
- credential-bearing URLs or sensitive query strings;
- large JSON/tool output;
- transcript content.

Markdown decoration may be stripped; whitespace is normalized.

When safety is uncertain, omit the line.

`COMPLETE` with no completion summary is always valid.

## 11.6 Validation wording

A statement such as `tests pass` is provider-reported completion text, not independently verified by DevBoard. The extractor may retain it only if the final user-visible response explicitly states it. M4 must not infer test success from the mere existence of a validation checkpoint.

---

# 12. Subagent and provider-native task semantics

## 12.1 Top-level card rule

**Subagents and Claude provider-native Task nodes do not become separate top-level M4 cards.**

The user's top-level coding turn remains the board identity.

## 12.2 Codex

- `SubagentStart` → parent `delegated` checkpoint;
- `SubagentStop` → parent `subtask_completed` checkpoint;
- private `agent_id` may correlate/dedupe the child;
- bounded `agent_type` may influence a template label;
- child transcript path and child final assistant text are discarded;
- inner subagent tool stream does not replace the parent checkpoint by default.

## 12.3 Claude Code

- `SubagentStart`/`SubagentStop` behave as above;
- `TaskCreated`/`TaskCompleted` are child/native task-node checkpoints;
- `task_subject` may be retained only after the same conservative sanitizer and with a **96 UTF-8 byte** bound;
- `task_description` is not retained by default;
- `task_id`, `agent_id`, teammate/team IDs remain private correlation only.

---

# 13. Lifecycle transition rules

M4 preserves existing reducer lifecycle meaning.

Conceptual transitions:

```text
UserPromptSubmit
  → WORKING

PreToolUse / successful or recoverable tool activity
  → WORKING unless a sticky unresolved ATTENTION remains

Permission / question / elicitation demand
  → ATTENTION

resolved attention + subsequent authoritative progress
  → WORKING

Claude Stop with background work remaining
  → WORKING + background_wait

terminal Stop
  → COMPLETE

terminal provider failure
  → ERROR

freshness timeout without authoritative new event
  → STALE / degraded confidence according to existing lifecycle model

SessionEnd
  → session resting/ended while preserving already completed/failed delivery semantics
```

Out-of-order/old turn events MUST NOT roll back a newer task. Existing event dedupe and monotonic-time protections remain mandatory.

---

# 14. Retention semantics

No database or durable task archive is added.

## Active tasks

WORKING/ATTENTION/ERROR-current tasks remain while they are operationally relevant under existing in-memory session/freshness bounds.

## Completed tasks

Completed TaskState uses the existing display configuration:

- `CompleteHighVisibilitySeconds`;
- `CompleteRetentionSeconds`.

A COMPLETE task is eligible for removal after its configured retention window. This preserves the existing delivery principle without introducing another independent retention configuration.

## Old turns

Only the bounded in-memory set required to satisfy recent completion delivery is kept. No full per-session history is promised.

---

# 15. Privacy/data classification

## 15.1 PRIVATE TRANSIENT

May be read only during adapter/resolver normalization:

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

## 15.2 PRIVATE NORMALIZED

May exist in bounded in-memory normalized events/state when needed:

- provider/session/turn correlation IDs;
- synthetic-turn marker;
- safe tool name/category;
- safe notification/error enum;
- background/session-cron counts;
- private canonical project/worktree identity;
- private child task/subagent correlation ID;
- capability flags/confidence.

## 15.3 PUBLIC

Only:

- DevBoard-generated task ID;
- provider;
- bounded project/worktree labels;
- bounded task title;
- lifecycle/freshness/confidence;
- bounded checkpoint kind/text/time;
- bounded actionable attention kind/text/time;
- bounded completion summary/result identifier/time;
- normal task timestamps.

---

# 16. Raw-data discard rules

Frozen rule:

```text
NO RAW SOURCE CONTENT PERSISTENCE
```

M4 does not add a database, transcript file, prompt archive, completion-message archive, command log, or raw-event log.

Provider raw JSON exists only long enough for bounded parsing in the hook process. Sensitive fields permitted for title/completion extraction are discarded immediately after deterministic normalization.

Diagnostic logging MUST NOT log raw provider JSON or rejected sensitive candidate text.

---

# 17. PublicState additions

M4 plans an additive root field:

```text
tasks: []PublicTask
```

`schemaVersion` remains `1` under the existing additive PublicState evolution policy.

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

## Public exclusions

`PublicTask` MUST NOT expose:

- provider session ID;
- provider turn/prompt ID;
- child task/subagent IDs;
- cwd/worktree root;
- repository `.git` paths;
- transcript path;
- prompt/final raw text;
- tool input/output;
- shell commands;
- provider raw JSON.

Existing `PublicAgent` is not removed in M4. Backward compatibility is preserved while desktop/task-board presentation can begin consuming additive `tasks`.

---

# 18. Confidence and capability degradation

Public/task confidence values are intentionally coarse:

```text
high
degraded
```

`high` means the current top-level task is correlated by a reliable provider-native identity and the adapter has not detected a material capability gap for the represented fact.

`degraded` means basic lifecycle remains useful but one of the following applies:

- Claude uses synthetic turn identity because `prompt_id` is unavailable;
- required rich event capability is known unavailable on the installed provider version;
- malformed/missing correlation prevents reliable enrichment;
- project/worktree resolution systematically fails for a source;
- a provider capability needed for a particular detail is unavailable.

A degraded task does not fabricate the missing title/checkpoint/summary.

---

# 19. SourceHealth and capability drift

Existing sources remain:

- `codex-hooks`;
- `claude-hooks`.

M4 does not add one SourceHealth record per task.

Rules:

1. valid basic lifecycle can remain available even when optional richness is absent;
2. reliable turn attribution unavailable → source degraded;
3. provider version too old for a **required M4 enrichment event** → source degraded, not unavailable;
4. malformed provider input is ignored/fail-open and may degrade source after repeated/material capability evidence; it never blocks the coding agent;
5. an unknown future provider event is ignored, not treated as fatal;
6. absence of a provider-specific optional signal never causes fabricated symmetry;
7. last-success semantics remain based on accepted valid monitoring events, not the existence of a provider process.

Hook execution failure, DevBoard daemon absence, socket timeout, invalid event, and monitoring bugs MUST NOT be capable of stopping Codex/Claude execution.

---

# 20. Hook event matrix

Legend:

- **KEEP** — already in M2 and required for M4;
- **ADD** — M4 should add this verified provider-native hook;
- **OPTIONAL** — useful but not required for M4 closure;
- **N/A** — not currently verified as a supported provider hook;
- **REJECT** — supported but intentionally not collected for M4.

| Event | Codex | Claude Code | M4 purpose |
|---|---|---|---|
| SessionStart | OPTIONAL | OPTIONAL | source/session freshness only |
| UserPromptSubmit | KEEP | KEEP | top-level task start + transient title derivation |
| PreToolUse | KEEP | KEEP | coarse checkpoint; Claude question detection |
| PermissionRequest | KEEP | KEEP | ATTENTION |
| PostToolUse | KEEP | KEEP | activity/resolution/checkpoint |
| PostToolUseFailure | N/A | KEEP | recoverable tool failure |
| PermissionDenied | N/A | KEEP | denial signal; not whole-task ERROR |
| Notification | N/A | KEEP | safe actionable type only |
| SubagentStart | ADD | ADD | delegated checkpoint |
| SubagentStop | ADD | ADD | subtask-completed checkpoint |
| TaskCreated | N/A | ADD | Claude native child-task checkpoint |
| TaskCompleted | N/A | ADD | Claude native child-task completion |
| Stop | KEEP | KEEP | terminal completion / final bounded extraction |
| StopFailure | N/A | KEEP | terminal provider ERROR |
| SessionEnd | KEEP | KEEP | session end/cleanup |
| Elicitation | N/A | KEEP | ATTENTION |
| ElicitationResult | N/A | KEEP | ATTENTION clear |
| PreCompact | not required | not required | no M4 card value |
| PostCompact | not required | not required | no M4 card value |
| MessageDisplay | N/A | REJECT | continuous assistant-message mirroring is out of scope |
| PostToolBatch | N/A | not required | redundant for checkpoint semantics |
| CwdChanged | N/A | OPTIONAL only if real validation proves needed | common-event cwd is preferred |
| WorktreeCreate/Remove | N/A | not required | M4 observes local Git identity; does not own provider worktrees |

## Minimum installation set

### Codex

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

### Claude Code

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

`AskUserQuestion` remains tool detection from Claude `PreToolUse`, not a separate hook event.

---

# 21. Desktop display intent

M4 freezes information hierarchy, not final responsive design.

Desktop task card conceptual order:

```text
PROVIDER · PROJECT / BRANCH
TASK TITLE

LIFECYCLE · ELAPSED
LATEST MEANINGFUL CHECKPOINT

ACTION REQUIRED   (only when present)
COMPLETION        (only after completion)
```

Examples:

```text
CODEX · DevBoard / codex/m4
M4 task observability
WORKING · 12m
Editing implementation
```

```text
CLAUDE CODE · ProductTool / analytics
Analytics audit
ATTENTION · 21m
Question waiting
```

```text
CODEX · DevBoard
M4 contract audit
COMPLETE · 34m
Audit complete; validation reported passing.
```

Raw IDs/paths are never required to identify a card.

---

# 22. Kindle regression boundary

The frozen M2.3 Kindle presentation contract remains unchanged in M4.

M4 technical design therefore requires:

- existing `AgentState` lifecycle remains available to the Kindle ViewModel;
- additive `tasks` do not require `/display/kindle` template changes;
- existing card selection/capacity/rotation/high-visibility/retention semantics remain intact;
- no new CSS/JavaScript/markup is required on Kindle for M4 closure;
- any final high-density task presentation redesign is deferred to M8.

M4 implementation acceptance MUST include a Kindle regression proving that adding TaskState does not change frozen M2.3 output/selection behavior for equivalent AgentState inputs.

---

# 23. Deterministic M4 test plan

Implementation is not part of this contract branch. Future M4 implementation must include at least:

## Adapter / privacy

- Codex current required event coverage including SubagentStart/Stop;
- Claude current required event coverage including Subagent, TaskCreated/Completed;
- raw prompt never appears in normalized event/state/public JSON;
- raw final assistant message never appears in normalized event/state/public JSON;
- tool input/output/command/transcript path never appears;
- Elicitation schema/URL/result never appears;
- background task description/command and cron prompt never appears;
- child final assistant message never appears;
- unsupported/unknown events fail open.

## Identity

- stable provider/session/turn internal identity;
- same provider multiple sessions remain independent;
- same session multiple turns become distinct retained TaskStates;
- Codex + Claude simultaneous isolation;
- Claude synthetic prompt identity sets degraded confidence;
- missing later turn identity is not guessed.

## Project/worktree

- normal Git repo;
- two worktrees of same repo remain distinct;
- branch available;
- detached HEAD;
- symlink cwd;
- non-Git cwd;
- deleted/unavailable cwd;
- no absolute path leaks publicly;
- resolver invokes Git without shell interpolation.

## Task title

- safe natural-language prompt derives bounded title;
- multiline whitespace normalization;
- UTF-8 byte bound;
- code fence fallback;
- shell/command fallback;
- absolute path fallback;
- secret/token/PEM/credential fallback;
- opaque blob/JSON fallback;
- deterministic output, no external call.

## Checkpoints

- each exact tool-name family mapping;
- generic Bash remains `running` even when fixture command text says test/build;
- unknown tool → `running`;
- priority replacement with fake clock;
- low-value event cannot immediately erase delegated/validation checkpoint;
- background_wait can be cleared by authoritative resume.

## Attention / error

- PermissionRequest sticky ATTENTION;
- Claude AskUserQuestion detection;
- Elicitation/ElicitationResult set/clear;
- same-turn progress clears only under frozen rules;
- stale maintenance clears never-ending attention;
- PostToolUseFailure does not mark task ERROR;
- PermissionDenied does not mark task ERROR;
- Claude StopFailure maps safe terminal error and safe actionable class;
- hook/source failure degrades monitoring only.

## Completion

- normal safe final response extraction;
- 16 KiB transient input cap;
- 320-byte / 3-line public bounds;
- code/stack/path/secret/command rejection;
- explicit safe SHA extraction;
- no invented validation statement;
- no safe text → valid nil summary;
- Claude Stop with background work does not complete or retain premature summary;
- later terminal Stop completes.

## Subagents/native tasks

- Codex child events update parent checkpoint only;
- Claude child events update parent checkpoint only;
- TaskCreated/Completed subject sanitized/bounded;
- child IDs are private;
- child tasks never become root cards.

## State / concurrency

- AgentState existing lifecycle tests unchanged;
- TaskState mirrors same reducer lifecycle transition;
- accepted event uses one Store update path;
- duplicate/out-of-order event handling retained;
- race suite with simultaneous provider events;
- complete retention expires TaskState deterministically;
- PublicState explicit allow-list contains only allowed task fields.

## Display regression

- desktop task hierarchy renders bounded public task data only;
- Kindle equivalent AgentState renders identically before/after M4 TaskState enrichment;
- Kindle never renders project path/raw IDs/checkpoint payloads added solely for M4.

---

# 24. Real Mac acceptance plan

After implementation, run on real macOS with real installed provider CLIs.

Record exact:

- DevBoard commit;
- `go version`, GOOS/GOARCH, macOS version;
- Codex CLI version;
- Claude Code version;
- active hook sources/config files as reported by provider hook inspection UI;
- whether each required event is accepted by the installed provider.

Acceptance scenarios:

1. start two simultaneous Codex sessions in different projects/worktrees;
2. start two simultaneous Claude sessions;
3. run Codex + Claude concurrently;
4. confirm each top-level prompt creates an independent safe task title;
5. confirm project/branch/worktree label distinguishes same-repo worktrees without exposing `/Users/...`;
6. exercise read/search/edit/generic tool work and verify coarse checkpoints only;
7. exercise a real permission request and verify ATTENTION is sticky until provider advances;
8. exercise Claude AskUserQuestion and verify `Question waiting` without exposing the full question;
9. exercise Claude subagent and native Task node events when the installed version supports them;
10. exercise Codex subagent events;
11. exercise Claude background task/session-cron Stop behavior and verify it remains WORKING/background_wait;
12. complete real Codex and Claude turns and verify bounded completion delivery;
13. inspect `/api/state` for no absolute cwd, prompt, raw final message, tool payload, transcript path, or child IDs;
14. stop DevBoard or make its socket unavailable and verify both coding agents continue normally;
15. verify source/task capability degrades honestly when a required rich signal is unavailable or identity is synthetic;
16. run normal repository validation (`go test ./...`, race suite, vet, build, diff check) on the real Mac;
17. render Kindle before/after equivalent lifecycle fixtures and confirm the frozen M2.3 presentation boundary is preserved.

An unavailable optional provider capability is not a code failure if the adapter reports it honestly and baseline lifecycle remains correct.

---

# 25. Rejected approaches

M4 explicitly rejects:

- full transcript monitoring;
- provider JSONL/session-file tailing as the primary source;
- terminal scraping;
- screen OCR;
- process-based progress inference;
- revived Process Groups;
- continuous assistant-message mirroring / Claude MessageDisplay ingestion;
- arbitrary shell command capture or command-text semantic parsing;
- complete tool input/result retention;
- raw prompt/title publication;
- raw final assistant response persistence;
- polling provider internals while native hooks suffice;
- one provider-specific mini-dashboard per provider;
- new generic plugin framework;
- LLM summarization for every title/checkpoint/completion;
- event-sourcing/database/queue architecture;
- multi-host networking inside M4;
- remote approval/question responses;
- M4 Kindle redesign.

---

# 26. Scope closure

M4 implementation, when later authorized, is frozen to:

```text
CURRENT PROVIDER HOOKS
        ↓
THIN ALLOW-LIST ADAPTERS
        ↓
EXISTING SINGLE REDUCER AUTHORITY
        ↓
AGENT LIFECYCLE + ADDITIVE TASK PRESENTATION STATE
        ↓
BOUNDED PUBLIC TASKS
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
