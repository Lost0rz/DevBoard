# M4 Task Observability Reference Audit v1

Status: FROZEN REFERENCE AUDIT

Date: 2026-08-22

Authoritative base: `aec946f3f06fff63285bbc26375e193ff9b8e5f8`

Scope: remote architecture/reference audit only. This document does not authorize M4 runtime implementation beyond the separate technical contract.

## Authority order

1. Frozen DevBoard MVP/business contracts.
2. Frozen DevBoard privacy and PublicState semantics.
3. Existing DevBoard implementation at the authoritative base.
4. Current official provider capability documentation.
5. External reference projects.

Reference projects do not redefine DevBoard product scope.

---

# A. CURRENT DEVBOARD M2 AUDIT

## A.1 Existing architecture

M2 already implements the desired basic shape:

```text
provider hook JSON on stdin
        ↓
bounded provider adapter / allow-list normalization
        ↓
AgentEvent
        ↓
local private Unix socket
        ↓
serialized reducer
        ↓
InternalRootState
        ↓
explicit PublicState projection
        ↓
server-rendered displays / API
```

Important existing properties that M4 must preserve:

- hook input is bounded;
- provider adapters are explicit allow-lists, not raw-payload mirrors;
- the monitoring hook is fail-open when DevBoard is unavailable;
- provider input never becomes a transcript archive;
- the local IPC socket is private;
- reducer transitions are authoritative for agent lifecycle;
- PublicState is an explicit allow-list;
- provider asymmetry is already accepted;
- one provider/session does not overwrite another provider/session;
- stale detection and completion retention already exist;
- a failed tool call is not automatically a failed task.

## A.2 Current signal matrix

`stored today?` means preserved semantically in state, not necessarily retaining the original provider field.

| Provider | CURRENT_SIGNAL / upstream event or field | Normalized today? | Stored today? | Public today? | Useful for M4? | Change required? |
|---|---|---:|---:|---:|---:|---|
| Codex | `session_id` | yes | yes | yes | yes | keep as private/source identity; stop using opaque source IDs as primary UI identity |
| Codex | `turn_id` | yes | current turn | yes | yes | use in stable internal top-level task key |
| Codex | `cwd` | yes, private | not publicly projected | no | yes | add private project/worktree resolver; never expose absolute path |
| Codex | `UserPromptSubmit` | yes | lifecycle start only | lifecycle only | yes | transiently inspect `prompt` only for bounded safe title derivation; discard raw prompt |
| Codex | `PreToolUse.tool_name` | yes as `ToolName` | lifecycle only | no | yes | map provider tool type/name to coarse checkpoint; do not retain tool input/command |
| Codex | `PermissionRequest` | yes | ATTENTION | lifecycle/alert | yes | retain only bounded normalized action class; optional provider description stays private/transient and is not required for public text |
| Codex | `PostToolUse` | yes | WORKING | lifecycle | yes | checkpoint update from tool name only |
| Codex | `Stop.stop_hook_active` | yes | completion semantics | lifecycle | yes | keep; additionally derive bounded completion from current `last_assistant_message` |
| Codex | `SessionEnd` | yes | session lifecycle | lifecycle | yes | keep cleanup/end authority |
| Codex | subagent `agent_id` today | deliberately ignored | no | no | yes | accept only `SubagentStart`/`SubagentStop` as child checkpoints; continue ignoring inner subagent tool stream for top-level cards |
| Claude | `session_id` | yes | yes | yes | yes | keep as source identity, not primary label |
| Claude | `prompt_id` | yes when available | turn identity | yes | yes | use as preferred current-turn identity; current official minimum is v2.1.196 |
| Claude | synthetic turn identity | yes fallback | yes | indirectly | yes | keep only as degraded fallback; never claim reliable native identity |
| Claude | `cwd` | yes, private | not publicly projected | no | yes | same private project/worktree resolver as Codex |
| Claude | `UserPromptSubmit` | yes | lifecycle start only | lifecycle only | yes | transient safe task-title derivation from `prompt`; discard raw prompt |
| Claude | `PreToolUse.tool_name` | yes | lifecycle only | no | yes | coarse checkpoint mapping; `AskUserQuestion` remains a derived attention signal from exact tool type |
| Claude | `PermissionRequest` | yes | ATTENTION | lifecycle/alert | yes | normalized approval-needed class; do not retain command/tool input |
| Claude | `PostToolUse` | yes | WORKING | lifecycle | yes | checkpoint mapping from tool name only |
| Claude | `PostToolUseFailure` | yes | WORKING | lifecycle | yes | keep recoverable; failure of one tool is not whole-task ERROR |
| Claude | `PermissionDenied` | yes | WORKING | lifecycle | yes | classify conservatively; denial alone is not whole-task ERROR |
| Claude | `Notification.notification_type` | yes | limited attention/idle behavior | lifecycle/alert | yes | expand only from verified safe notification classes; do not mirror free-text `message` |
| Claude | `Stop.stop_hook_active` | yes | completion semantics | lifecycle | yes | keep |
| Claude | `Stop.background_tasks` | reduced to count | yes | no | yes | keep count-only; use nonzero count as `background_wait`; do not retain descriptions/commands |
| Claude | `Stop.session_crons` | reduced to count | yes | no | yes | keep count-only; same background-wait semantics |
| Claude | `Stop.last_assistant_message` | deliberately discarded | no | no | yes | transient bounded deterministic completion extraction only |
| Claude | `StopFailure.error` | safe type normalized as `ErrorType` | ERROR | lifecycle/alert | yes | keep safe enum/type; never retain raw error details |
| Claude | `SessionEnd` | yes | session lifecycle | lifecycle | yes | keep |
| Claude | `Elicitation` / `ElicitationResult` | yes | attention / clear | lifecycle/alert | yes | keep; public text may be generic `Elicitation waiting`; do not retain schema, URL, credentials, or response content |
| Claude | native task subject/title | not currently accepted | no | no | yes as child checkpoint | add `TaskCreated`/`TaskCompleted`; retain bounded sanitized `task_subject`, never full `task_description` by default |
| Claude | subagent metadata | deliberately ignored | no | no | yes | add `SubagentStart`/`SubagentStop` as child checkpoints; retain bounded agent type, private native child ID |

## A.3 Existing private fields and M4 disposition

| Current private field | Current role | M4 disposition |
|---|---|---|
| `cwd` | provider event context | KEEP PRIVATE; use only for local project/worktree resolution |
| `ToolName` | coarse provider metadata | KEEP PRIVATE NORMALIZED; map to checkpoint category; do not expose arbitrary names blindly |
| `NotificationType` | safe Claude notification class | KEEP PRIVATE NORMALIZED; map only known actionable classes |
| `ErrorType` | safe Claude provider failure class | KEEP PRIVATE NORMALIZED; map to task ERROR/ATTENTION semantics |
| `BackgroundTaskCount` | Claude Stop richness | KEEP; count only |
| `SessionCronCount` | Claude Stop richness | KEEP; count only |
| `StopHookActive` | Stop continuation semantics | KEEP |
| `SyntheticTurnIdentity` | degraded Claude fallback | KEEP PRIVATE; capability/source health must disclose degraded confidence |

## A.4 Data intentionally discarded by M2

M2 intentionally excludes raw or high-risk provider content including:

- raw user prompt;
- final assistant response;
- tool input and tool result;
- shell command text;
- transcript path/content;
- notification free text;
- raw error detail;
- background-task descriptions and commands;
- session-cron prompt content;
- raw provider JSON.

M4 changes this boundary narrowly in exactly two places:

1. the raw `UserPromptSubmit.prompt` may be inspected transiently to derive a bounded safe title;
2. the raw final visible assistant text available on provider Stop may be inspected transiently to derive a bounded completion summary.

Neither raw value becomes normalized state, a log, a database record, or PublicState. Everything else above remains excluded unless the technical contract explicitly names a bounded normalized replacement.

## A.5 Existing authority decision pressure

`AgentState` is currently provider-session lifecycle state with one authoritative `CurrentTurn`. It is deliberately compact and powers existing lifecycle/Kindle behavior. M4 needs retained task presentation data, project/worktree identity, meaningful checkpoints, attention summary, and completion delivery without turning `AgentState` into a transcript-like record. The technical contract therefore introduces a separate additive `TaskState` projection maintained by the same reducer transaction and sourced from the same normalized events. `AgentState` remains provider-session lifecycle authority; `TaskState` must not independently reinterpret lifecycle.

---

# B. CODEX CURRENT CAPABILITY MATRIX

Primary authority: current OpenAI Codex Hooks release-behavior documentation, retrieved 2026-08-22. The official page explicitly warns that `main`-branch schemas can contain fields not present in a released CLI; release documentation wins for capability claims.

Source-level cross-check: `openai/codex` commit `4f39251a010a8bd7d692d25fb33832ff06f1635a`, especially `codex-rs/hooks/src/schema.rs`. License: Apache-2.0.

## B.1 Current documented hook set

Current release documentation lists:

- turn: `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, `SubagentStop`, `Stop`;
- session/subagent start: `SessionStart`, `SubagentStart`;
- main thread end: `SessionEnd`.

The current release documentation does **not** document Codex equivalents of Claude `PostToolUseFailure`, `PermissionDenied`, `Notification`, `StopFailure`, `TaskCreated`, or `TaskCompleted`.

## B.2 Useful Codex signals

| CODEX_SIGNAL | Exact useful fields | Availability/version | Lifecycle meaning | Privacy | Proposed DevBoard use |
|---|---|---|---|---|---|
| `SessionStart` | common `session_id`, `cwd`; start source via matcher | current release; minimum version not stated in current docs | session starts/resumes/clears/compacts | cwd sensitive | OPTIONAL freshness/context; not necessary to create a top-level task |
| `UserPromptSubmit` | `session_id`, `turn_id`, `cwd`, `prompt`, `permission_mode` | current release; minimum not stated | authoritative top-level turn start | prompt highly sensitive | REQUIRED; begin task and transiently derive title; discard prompt |
| `PreToolUse` | `turn_id`, `tool_name`, `tool_use_id`, `tool_input` | current release; local function paths only | tool about to run | input/command sensitive | REQUIRED; use `tool_name` only for coarse checkpoint |
| `PermissionRequest` | `turn_id`, `tool_name`, `tool_input`, optional `tool_input.description` | current release | Codex is about to ask for approval | description/input may reveal command/path | REQUIRED; ATTENTION `Approval needed`; keep tool category only; free-text description not required |
| `PostToolUse` | `turn_id`, `tool_name`, `tool_use_id`, `tool_input`, `tool_response` | current release | supported tool produced output; Bash also fires after nonzero exit | input/output highly sensitive | REQUIRED; mark resumed activity/checkpoint using tool name only; do not infer whole-task failure |
| `SubagentStart` | `turn_id`, `agent_id`, `agent_type` | current release | child agent starts | child ID opaque; type low sensitivity after bounding | REQUIRED richness; parent checkpoint `delegated`; no new top-level card |
| `SubagentStop` | `turn_id`, `agent_id`, `agent_type`, `agent_transcript_path`, `stop_hook_active`, `last_assistant_message` | current release | child agent finished response | transcript/final message sensitive | REQUIRED richness; parent `subtask_completed`; ignore transcript and raw child message |
| `Stop` | `turn_id`, `stop_hook_active`, `last_assistant_message` | current release | main turn has finished responding, subject to Stop continuation | final text sensitive | REQUIRED; authoritative completion candidate and transient bounded completion extraction |
| `SessionEnd` | `session_id`, `cwd`, `reason` (`other` today) | current release | main session ends | cwd sensitive | REQUIRED cleanup/freshness signal |

## B.3 Tool coverage limits

Current Codex documentation says tool hooks cover shell (`Bash`), unified exec (also `Bash`), `apply_patch`, MCP tools, and most local function tools. Hosted tools such as `WebSearch` do not use this hook path, and specialized tools may opt out. Therefore M4 must treat checkpoint richness as best-effort, never as a complete activity trace.

A generic `Bash` tool name is **not** enough to distinguish tests from builds from arbitrary shell work without inspecting command text. M4 will not inspect command text merely to classify progress. Generic shell therefore maps to generic `running`.

## B.4 Signals unavailable or uncertain for Codex

- no documented provider-native top-level task/session title or subject in current hook payloads;
- no documented `TaskCreated`/`TaskCompleted` hook;
- no dedicated documented tool-failure hook; `PostToolUse` may fire after nonzero Bash exit;
- no documented `StopFailure` or terminal provider API failure hook;
- no documented Notification hook;
- no dedicated documented question/elicitation hook equivalent to Claude's richer set;
- no documented field that supports percentage progress or ETA.

M4 must not fabricate symmetry. Missing richness degrades capability, not basic lifecycle.

## B.5 Minimum Codex hook set for M4

Required:

- `UserPromptSubmit`
- `PreToolUse`
- `PermissionRequest`
- `PostToolUse`
- `SubagentStart`
- `SubagentStop`
- `Stop`
- `SessionEnd`

Optional:

- `SessionStart` for session freshness/context.

Not required for M4:

- `PreCompact`
- `PostCompact`

No transcript hook/read is required.

---

# C. CLAUDE CODE CURRENT CAPABILITY MATRIX

Primary authority: current official Claude Code Hooks Reference, retrieved 2026-08-22.

The current documented event set is substantially richer than M2 assumed. Relevant current events include `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PermissionDenied`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `Notification`, `MessageDisplay`, `SubagentStart`, `SubagentStop`, `TaskCreated`, `TaskCompleted`, `Stop`, `StopFailure`, `SessionEnd`, `Elicitation`, and `ElicitationResult` among others.

## C.1 Common identity

Current common hook input includes:

- `session_id`;
- `prompt_id` identifying the active user prompt, available from Claude Code v2.1.196+ and absent before the first user input;
- `cwd`;
- `permission_mode` where applicable;
- `agent_id` and `agent_type` inside subagent calls.

`prompt_id` is the preferred Claude top-level turn identity when present. Older versions remain supported in degraded mode using the existing synthetic-turn fallback.

## C.2 Useful Claude signals

| CLAUDE_SIGNAL | Exact useful fields | Version requirement | Lifecycle meaning | Privacy | Proposed DevBoard use |
|---|---|---|---|---|---|
| `UserPromptSubmit` | common identity + `prompt` | event current; `prompt_id` requires v2.1.196+ | authoritative turn start | prompt highly sensitive | REQUIRED; transient deterministic title derivation only |
| `PreToolUse` | `tool_name`, `tool_use_id`, `tool_input` | current docs; no minimum stated | tool start | input may contain code/command/path | REQUIRED; tool-name checkpoint only; exact `AskUserQuestion` tool identifies question attention |
| `PermissionRequest` | `tool_name`, `tool_input`, optional suggestions; notably no `tool_use_id` | current docs | user permission is being requested now | command/description sensitive | REQUIRED; generic bounded approval attention; do not retain tool input |
| `PostToolUse` | `tool_name`, `tool_use_id`, structured response | current docs | successful tool completion | response sensitive | REQUIRED checkpoint/resume signal; ignore response content |
| `PostToolUseFailure` | tool fields + failure data | current docs | one tool failed | error/input sensitive | REQUIRED; recoverable failure does not make whole task ERROR |
| `PermissionDenied` | `tool_name`, `tool_input`, `tool_use_id`, `reason` | current docs; auto-mode event | auto mode denied one call | reason/input may be sensitive | REQUIRED; generally resume/working or bounded attention only when subsequent lifecycle proves user action is required; never automatic task ERROR |
| `Notification` | `notification_type`, `message`, optional `title` | current; `agent_needs_input` and `agent_completed` require v2.1.198+ | asynchronous user notification | free text may be sensitive | REQUIRED for safe notification classes; use type as authority; do not mirror message/title by default |
| `SubagentStart` | `agent_id`, `agent_type` | current docs | child agent starts | opaque child ID | REQUIRED richness; parent `delegated` checkpoint |
| `SubagentStop` | `agent_id`, `agent_type`, `stop_hook_active`, `last_assistant_message`, parent-scoped background arrays | current docs | child agent finishes response | child final/transcript sensitive | REQUIRED richness; parent `subtask_completed`; do not ingest child transcript/final text |
| `TaskCreated` | `task_id`, `task_subject`, optional `task_description`, teammate/team fields | current docs; no minimum stated | Claude native TaskCreate node created | subject may contain user text; description sensitive | REQUIRED richness; bounded sanitized subject may label child checkpoint, never automatically replace root task title |
| `TaskCompleted` | same task identity/subject fields | current docs; no minimum stated | native task marked complete | same | REQUIRED richness; parent subtask-completed checkpoint |
| `Stop` | `stop_hook_active`, `last_assistant_message`, `background_tasks`, `session_crons` | current docs | main agent finished responding; arrays distinguish done vs paused for background work | final text, task descriptions, commands sensitive | REQUIRED; count-only background semantics; bounded final completion extraction |
| `StopFailure` | `error`, optional `error_details`, optional rendered error text | current docs | turn ended due to API error | detail/error text may contain sensitive data | REQUIRED; safe `error` enum drives terminal ERROR/ATTENTION; discard details/text |
| `SessionEnd` | `reason` + common identity | current docs | session ends | cwd sensitive | REQUIRED cleanup/freshness |
| `Elicitation` | `mcp_server_name`, `message`, optional `mode`, `url`, `elicitation_id`, schema | current docs | MCP server asks user for input/auth | potentially very sensitive | REQUIRED; generic `Elicitation waiting` / auth class only; discard schema/url/content |
| `ElicitationResult` | response action/result fields | current docs | elicitation resolved | response may contain credentials/user data | REQUIRED clear signal; retain only safe action class |

## C.3 AskUserQuestion

Claude Code currently exposes `AskUserQuestion` as a tool. M4 should detect it from `PreToolUse.tool_name == "AskUserQuestion"`; there is no need for a separate transcript parser. The `questions` payload is sensitive and not needed to establish ATTENTION. Public MVP text may simply be `Question waiting`.

## C.4 Background work semantics

Claude Stop currently supplies `background_tasks` and `session_crons` when its task registry is available. They distinguish a truly completed response from a pause that is waiting for background work to wake the session. M2 already reduces both arrays to counts. M4 keeps this rule. It must not store background descriptions, shell commands, MCP details, or cron prompts.

## C.5 MessageDisplay

`MessageDisplay` is current and fires while assistant message text is displayed/streamed. M4 explicitly rejects it for MVP because it would turn DevBoard into continuous assistant-message mirroring, increase event volume, and undermine the completion-only privacy boundary. `Stop.last_assistant_message` is sufficient for bounded completion delivery.

## C.6 Minimum Claude hook set for M4

Required:

- `UserPromptSubmit`
- `PreToolUse`
- `PostToolUse`
- `PostToolUseFailure`
- `PermissionRequest`
- `PermissionDenied`
- `Notification`
- `SubagentStart`
- `SubagentStop`
- `TaskCreated`
- `TaskCompleted`
- `Stop`
- `StopFailure`
- `SessionEnd`
- `Elicitation`
- `ElicitationResult`

Optional/not required:

- `SessionStart` may improve source freshness but is not required for top-level task creation;
- `CwdChanged` is not required because useful events already include current `cwd`; it can be added later only if real validation proves session directory changes are otherwise missed;
- `PostToolBatch` is unnecessary for MVP checkpoint semantics;
- `MessageDisplay` is explicitly rejected;
- WorktreeCreate/WorktreeRemove are not required because M4 resolves identity from `cwd` and Git locally rather than taking over provider worktree management.

---

# D. GITHUB REFERENCE PROJECTS

Only source-level references with a mechanism relevant to M4 were retained.

## D.1 `Ericonaldo/AgentMonitor`

- repository: `Ericonaldo/AgentMonitor`
- pinned revision: `38e2085f112b6725ebfe9f486577c51f9d624f9a`
- revision date: 2026-05-22
- license/provenance: README declares MIT, but no top-level `LICENSE` file exists at the pinned revision; therefore no source code will be copied into DevBoard under this audit
- files inspected:
  - `server/src/services/ExternalAgentScanner.ts`
  - `server/src/services/AgentManager.ts`
  - `server/src/models/Agent.ts`
  - README/package metadata
- observed behavior:
  - monitor-managed agents are launched and streamed by AgentMonitor itself;
  - external agents are discovered through process enumeration;
  - process cwd is resolved from args, `/proc`, or `lsof`;
  - session files are located and JSONL is parsed/tailed;
  - prompts, messages, tool content, PID, worktree path/branch, token usage and other rich state are retained;
  - task text may be taken from process prompt or first user transcript message;
  - external liveness is partly inferred from process state and file activity.
- useful behavior:
  - project/worktree should be first-class context on a task card;
  - simultaneous provider sessions require independent identities;
  - waiting/working/stopped is a useful glanceable normalization;
  - separate managed-vs-external source capability is a useful concept.
- conflict with DevBoard:
  - process polling and transcript tailing are much broader than M4 needs;
  - full message/prompt/tool retention conflicts with DevBoard privacy;
  - orchestration, PTY, resume, remote control and worktree creation are outside M4.
- decision: **BEHAVIORAL REFERENCE ONLY**.

## D.2 `henrikekblad/codelight`

- repository: `henrikekblad/codelight`
- pinned revision: `ddbc122f2392914de6d8dd9b1939e9340de821a4` (release 1.8.1)
- revision date: 2026-08-18
- license: MIT, verified in repository `LICENSE`
- files inspected:
  - `companion/codelight_core/agents/claude.py`
  - `companion/codelight_core/agents/codex.py`
  - `companion/codelight_core/agents/base.py`
  - `companion/test_codelight.py` search results
  - `LICENSE`
- observed behavior:
  - installs small provider hook sets and maps events into simple `working`, `waiting`, and `ended` signals;
  - Claude hook set uses `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `PermissionRequest`, `PermissionDenied`, `Stop`, and `SessionEnd`, with optional `AskUserQuestion` handling;
  - Codex hook set uses `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PermissionRequest`, `Stop`, `SubagentStart`, and `SubagentStop`;
  - project also contains transcript/usage facilities and remote-control features, but those are separable from the small hook-state idea.
- useful behavior:
  - validates the central M4 premise that a very thin provider-native hook layer is sufficient for core working/waiting/ended observability;
  - provider-specific hook sets can normalize to a small common state without pretending payload symmetry;
  - question and permission paths can be treated as explicit attention signals.
- decision: **ADAPT SMALL COMPONENT/PATTERN** — adapt the *pattern* of thin event-to-state normalization, not source code and not its remote-control/transcript subsystems.

## D.3 `jiweiyeah/AgentMonitor`

- repository: `jiweiyeah/AgentMonitor`
- pinned revision: `0d404bdf0968befae73163695342f15e74d1dc04` (release v0.3.0)
- revision date: 2026-05-09
- license: MIT, verified in repository `LICENSE`
- files inspected:
  - `crates/agentmonitor/src/adapter/codex.rs`
  - repository tree including `adapter/claude.rs`, `collector/fs_watch.rs`, `collector/proc_sampler.rs`, `adapter/process_match.rs`
  - `LICENSE`
- observed behavior:
  - discovers session JSONL under provider-owned directories;
  - parses metadata and message previews directly from provider session files;
  - watches filesystem updates with reconciliation;
  - also matches/samples processes to determine active sessions and resource state;
  - derives status partly from session-file freshness/process activity.
- useful behavior:
  - demonstrates robust multi-session discovery and separate provider adapters;
  - shows why transcript/process monitoring can recover information when hooks are unavailable.
- conflict with DevBoard:
  - current official hooks already provide M4's required lifecycle/attention/completion primitives;
  - transcript message parsing and process sampling add privacy, coupling, and maintenance costs that are unnecessary for M4.
- decision: **BEHAVIORAL REFERENCE ONLY** as a fallback design reference; do not adopt its primary collection mechanism.

## D.4 Official Codex source cross-check

- repository: `openai/codex`
- pinned revision inspected: `4f39251a010a8bd7d692d25fb33832ff06f1635a`
- license: Apache-2.0
- file inspected: `codex-rs/hooks/src/schema.rs`
- useful behavior: source confirms schema-level concepts such as `turn_id`, subagent fields, tool name/input identifiers, and current hook event wire types.
- authority caveat: official release documentation remains the capability authority because OpenAI explicitly notes `main` schemas may lead release behavior.
- decision: **BEHAVIORAL/API REFERENCE ONLY**; DevBoard consumes the supported provider hook interface, not Codex source code.

---

# E. LICENSE / PROVENANCE

| Source | Pin | License / status | Reuse decision |
|---|---|---|---|
| OpenAI Codex current hook docs | retrieved 2026-08-22 | official documentation; release behavior authority | interface semantics only |
| `openai/codex` | `4f39251a010a8bd7d692d25fb33832ff06f1635a` | Apache-2.0 | API/source cross-check only |
| Claude Code current hook docs | retrieved 2026-08-22 | official documentation; current behavior authority | interface semantics only |
| `Ericonaldo/AgentMonitor` | `38e2085f112b6725ebfe9f486577c51f9d624f9a` | README says MIT; no top-level license file at pin | behavioral only; no code reuse |
| `henrikekblad/codelight` | `ddbc122f2392914de6d8dd9b1939e9340de821a4` | MIT | adapt pattern only |
| `jiweiyeah/AgentMonitor` | `0d404bdf0968befae73163695342f15e74d1dc04` | MIT | behavioral fallback reference only |

No third-party code is copied by this contract branch.

---

# F. REUSE DECISIONS

1. **Provider-native hooks/events: USE DIRECTLY as interfaces.**
2. **Current DevBoard adapter/socket/reducer/PublicState architecture: EXTEND, do not replace.**
3. **codelight thin hook-state normalization: ADAPT SMALL PATTERN.**
4. **AgentMonitor project/worktree/card concepts: BEHAVIORAL REFERENCE ONLY.**
5. **Transcript/process discovery mechanisms: REJECT for M4 primary architecture.**
6. **Provider final assistant text: use provider hook field transiently, never archive it.**

---

# G. HOOK COMPLEXITY REDUCTION FINDINGS

The audit answer is **YES**: M4 can be built mostly from provider-native supported events with a thin DevBoard normalization layer.

Preferred flow:

```text
provider-native hook events
        ↓
thin provider allow-list adapter
        ↓
normalized significant TaskSignal + existing AgentEvent semantics
        ↓
one reducer authority / one Store update per accepted event
        ↓
AgentState lifecycle + bounded TaskState presentation
        ↓
explicit PublicState projection
        ↓
Dashboard
```

No large plugin framework, polling daemon, transcript parser, terminal scraper, process-progress detector, OCR, or LLM summarization service is required.

## Provider hook delta from M2

### Codex

KEEP:
- UserPromptSubmit
- PreToolUse
- PermissionRequest
- PostToolUse
- Stop
- SessionEnd

ADD:
- SubagentStart
- SubagentStop
- optionally SessionStart for freshness only

REMOVE:
- none

### Claude Code

KEEP:
- UserPromptSubmit
- PreToolUse
- PermissionRequest
- PostToolUse
- PostToolUseFailure
- PermissionDenied
- Notification
- Stop
- StopFailure
- SessionEnd
- Elicitation
- ElicitationResult

ADD:
- SubagentStart
- SubagentStop
- TaskCreated
- TaskCompleted

REMOVE:
- none of the current M2 required set

EXPLICITLY DO NOT ADD:
- MessageDisplay
- PostToolBatch
- WorktreeCreate/WorktreeRemove
- CwdChanged unless real-Mac validation proves common-event cwd is insufficient

---

# H. PROVIDER ASYMMETRY

M4 must preserve real asymmetry:

- Claude has native `TaskCreated`/`TaskCompleted`; Codex current hooks do not.
- Claude has `StopFailure`; Codex current hooks do not.
- Claude has Elicitation and rich Notification classes; Codex current hooks do not.
- both providers currently expose final assistant text on Stop;
- both expose subagent lifecycle, but child metadata differs;
- both expose `cwd`, session identity, tool names, and a top-level turn/prompt identity mechanism;
- Codex tool-hook coverage is explicitly incomplete for hosted/specialized tools;
- Claude `prompt_id` has a documented minimum version; older Claude installations require degraded synthetic identity.

No adapter may manufacture a provider event merely to make matrices symmetrical.

---

# I. PRIVACY RISKS

Highest risks:

1. `UserPromptSubmit.prompt` may contain secrets, commands, absolute paths, customer data, pasted source, or credentials.
2. `last_assistant_message` may contain source code, secrets, full command lines, absolute paths, URLs, stack traces, or large result bodies.
3. permission and elicitation descriptions may include commands, resource names, paths, credentials, or URLs.
4. Task descriptions may reproduce large parts of user instructions.
5. tool input/output and transcript files are effectively conversation archives.
6. cwd and transcript paths expose usernames and local filesystem layout.

M4 therefore permits only bounded deterministic normalization and rejects raw persistence. The exact hard bounds are frozen in the technical contract.

---

# J. REJECTED APPROACHES

## Full transcript monitoring

Rejected. It couples DevBoard to unstable provider transcript formats, creates a transcript archive/privacy surface, and is unnecessary because current hooks expose the required lifecycle and final-message signal.

## Terminal scraping

Rejected. Terminal output is presentation, not stable provider state; it is noisy, provider/version/theme dependent, and leaks commands/results.

## Process-based progress detection

Rejected for M4. Process existence is at best liveness, not task semantics. Current hooks provide authoritative task lifecycle. Process Groups also remain outside MVP scope.

## Continuous assistant-message mirroring

Rejected. Claude `MessageDisplay` and transcript tails can provide it, but M4 only needs bounded completion delivery. Streaming text creates noise and privacy/storage pressure.

## Arbitrary command capture

Rejected. Shell command contents are not needed to know that generic work is running. Bash maps to `running`; M4 will not parse command text just to guess `validating`.

## Generic screen OCR

Rejected. It is less reliable and more invasive than official hooks.

## Polling provider internals when hooks suffice

Rejected as primary architecture. Provider session files/process polling remain a future fallback reference only if an official interface disappears.

## LLM summarization for every checkpoint or completion

Rejected for MVP. It adds latency, cost, failure modes, provider coupling, and additional disclosure of sensitive content. Deterministic bounded extraction is sufficient; an absent summary is valid.

## New generic plugin framework

Rejected. Two explicit provider adapters are simpler and preserve provider asymmetry.

---

# Audit conclusion

The current official provider interfaces are sufficient to freeze M4 without a new polling/transcript architecture. M4 should remain a local, event-driven, fail-open extension of M2 with:

- stable per-turn task identity;
- local private project/worktree resolution from cwd;
- deterministic safe title derivation;
- coarse meaningful checkpoints from provider event/tool type;
- sticky actionable attention with explicit clearing;
- deterministic bounded completion extraction from Stop final text;
- child subagent/native-task events represented as parent checkpoints;
- no raw prompt/final/transcript persistence;
- no Kindle redesign;
- no multi-host transport.

`UNRESOLVED_MATERIAL_DECISIONS: NONE`
