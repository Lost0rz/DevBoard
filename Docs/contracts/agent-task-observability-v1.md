# DevBoard AI Task Observability V1 — Business Contract

> Date: 2026-08-22  
> Parent: `Docs/contracts/mvp-monitoring-v1.md`  
> Status: **BUSINESS CONTRACT FROZEN**  
> Scope: what task information is worth collecting/presenting from Codex and Claude Code. This is not an implementation contract.

## 1. Goal

The Task Board must answer:

> What is each AI coding task doing now, where is it running, does it need me, and what did it finish?

It must not become a full conversation recorder.

## 2. Observable information classes

Task observability is divided into five business classes.

### 2.1 Identity

Useful identity:

- host;
- provider;
- project/worktree public identity;
- top-level turn/work item;
- short human-readable task title when available.

Not useful as primary display:

- opaque session IDs;
- opaque turn IDs;
- transcript paths;
- absolute cwd.

Opaque IDs may remain internal for correlation.

### 2.2 Lifecycle

Existing core lifecycle remains:

- WORKING;
- ATTENTION;
- ERROR;
- COMPLETE;
- STALE/unknown confidence as already modeled.

Lifecycle is authoritative only where provider events establish it.

### 2.3 Significant checkpoint

A checkpoint is the latest meaningful externally observable task node.

Examples:

- task started;
- inspecting/researching;
- editing/implementing;
- running a tool/validation step;
- subagent delegated;
- provider-native subtask created;
- provider-native subtask completed;
- waiting for background work;
- finalizing/completed.

DevBoard does not need every tool event on screen. Frequent low-value events may update internal activity/freshness without replacing the last meaningful checkpoint.

Checkpoint text must remain concise.

### 2.4 Actionable feedback

When the agent needs the user, the board should retain a concise user-facing reason.

Important classes:

- permission/approval request;
- AskUserQuestion / elicitation;
- provider error requiring intervention;
- authentication/billing/rate-limit problem;
- blocked/denied operation where user choice is needed.

The board should show enough context to decide which task needs attention, but MVP remains read-only.

### 2.5 Completion delivery

A completed top-level turn should retain a compact completion summary.

Preferred content:

- result achieved;
- validation/test status if stated;
- important remaining limitation;
- important output identifier (for example a branch/commit) if present in the provider's visible final response.

Completion delivery is not the full final assistant message.

## 3. No continuous reasoning stream

The following are not Task Board business requirements:

- hidden chain of thought;
- continuous assistant prose streaming;
- every internal reasoning update;
- every tool payload;
- complete shell commands;
- complete tool results;
- complete transcript;
- full final response.

A provider may expose message-stream hooks, but MVP does not need to mirror them.

The product values **state transitions and meaningful checkpoints**, not token flow.

## 4. Task title semantics

The task title is a short board label, not a transcript field.

Preferred source order:

1. provider-native named task/session title;
2. provider-native task subject;
3. explicit user label;
4. derived sanitized title from task input;
5. fallback `Project · Provider`.

If title derivation is used:

- do not publish the raw user prompt;
- remove obviously sensitive/private payload material;
- bound length;
- treat the result as a derived display label.

## 5. Progress is checkpoint-based, not percentage-based

MVP progress means:

> latest meaningful observed stage

It does NOT mean estimated percentage complete.

Forbidden unless a provider gives authoritative progress:

- `30% complete`;
- `almost done`;
- fabricated step counts;
- ETA inferred from elapsed time.

Examples of acceptable card progress:

```text
Editing implementation
Running validation
Waiting for approval
Subtask: audit auth flow completed
Final response ready
```

## 6. Provider capability policy

The product uses a common card hierarchy but accepts provider-specific richness.

### 6.1 Codex — useful currently observable classes

Current Codex lifecycle hooks can expose enough signals for:

- turn start through `UserPromptSubmit`;
- cwd/host/session correlation;
- active model/permission mode as capability context if later useful;
- tool category/name through `PreToolUse` / `PostToolUse`;
- permission request and tool class;
- optional human-readable permission description where supplied;
- subagent start/stop and type;
- final `Stop` event;
- final user-visible `last_assistant_message` where available;
- session end.

Business use:

- tool names may become coarse checkpoints;
- permission description may become bounded actionable feedback;
- subagent events may become significant checkpoints;
- Stop final message may feed a bounded completion summary.

Codex prompt text is available at `UserPromptSubmit`, but raw prompt publication is not part of DevBoard's public contract.

### 6.2 Claude Code — useful currently observable classes

Current Claude Code hooks can expose:

- user prompt submission;
- tool start/success/failure;
- permission request / permission denied;
- notifications;
- AskUserQuestion/elicitation-style attention signals;
- subagent start/stop;
- provider-native TaskCreated / TaskCompleted events with task subject/description;
- top-level Stop with final `last_assistant_message`;
- StopFailure/provider error class;
- background task/session-cron state;
- session end.

Business use:

- TaskCreated/TaskCompleted subjects are high-value native task checkpoints;
- permission/notification/question signals drive ATTENTION;
- subagent events provide meaningful delegation progress;
- Stop final message may feed bounded completion summary;
- background-work presence prevents false completion.

Claude Code also exposes assistant message display/stream events in current versions. MVP intentionally does not need continuous message capture.

## 7. Cross-provider normalized card

The visual card should be able to represent this conceptual information:

```text
HOST
PROVIDER
PROJECT / WORKTREE
TASK TITLE

STATUS
ELAPSED

LATEST CHECKPOINT

ACTION REQUIRED (optional)
  short actionable feedback

COMPLETION (optional)
  short completion summary
```

Not every field must be populated for every provider/task.

## 8. Checkpoint replacement policy — business behavior

The visible latest checkpoint should favor meaning over frequency.

Higher-value events replace lower-value generic activity.

Example priority:

1. user attention request;
2. explicit provider-native task created/completed node;
3. explicit validation/build/test-like checkpoint when reliably identified;
4. subagent lifecycle;
5. edit/tool activity;
6. generic working heartbeat.

An ATTENTION event must not immediately disappear because a low-value background heartbeat arrives.

Exact reducer implementation is a technical contract concern, but this presentation intent is frozen.

## 9. Actionable feedback boundary

Actionable feedback is allowed to contain **bounded user-visible context** because its purpose is to tell the user what requires intervention.

Examples:

```text
Approval needed · Bash operation
Question waiting · choose deployment target
Rate limit reached
Authentication required
```

Where a provider exposes a human-readable description/question, DevBoard may retain a sanitized bounded form.

It must not copy arbitrary full tool inputs or command payloads just because they are present in a hook.

## 10. Completion summary boundary

Provider Stop events can expose final user-visible assistant text.

DevBoard may use this source to create a short completion summary, but must not publish/store it as an unrestricted full-response archive.

A good board summary should normally fit within a small card region.

Examples:

```text
Implemented network collector; tests and race suite pass.

Audit complete: no blocking defects; M4.2 not started.

Build failed: dependency unavailable; code unchanged.
```

If a reliable bounded summary cannot be produced, COMPLETE remains valid without summary text.

## 11. Project context relationship

Task identity and project identity belong together in the user experience.

Examples:

```text
Mac mini · CODEX
DevBoard / main
Network Health collector
WORKING · Running validation
```

```text
MacBook · CLAUDE CODE
ProductTool / analytics
M12 audit
ATTENTION · Question waiting
```

The board should not require the user to decode a session ID to know which task is which.

## 12. Multiple simultaneous tasks

The board must assume:

- multiple tasks can run on one host;
- Codex and Claude Code can run simultaneously;
- the same provider can have multiple active sessions;
- multiple hosts can have active tasks simultaneously.

Cards are therefore task/session instances, not one permanent card per provider.

Provider name is a label, not the unique task identity.

## 13. Recent complete behavior

A completed task remains visible as a delivery event for a bounded period.

The existing M2/M2.3 principle remains:

- recent completion deserves promotion;
- older completion yields to active/attention work;
- completion does not vanish immediately merely because the task became idle.

A useful completion summary increases delivery value but does not change lifecycle authority.

## 14. Error semantics

The board distinguishes:

- recoverable tool failure while the agent continues;
- user-action-required failure;
- terminal provider/turn failure.

A single failed tool call does not automatically mean the whole task is ERROR.

Rate limit/authentication/provider fatal stop errors should be visible because they explain why work has stopped.

## 15. Privacy and data minimization

Public Task Board fields may contain:

- safe task title;
- safe project identity;
- coarse checkpoint;
- bounded actionable feedback;
- bounded completion summary.

They must not contain by default:

- raw prompt;
- raw transcript;
- full assistant response;
- raw tool input/output;
- command history;
- absolute filesystem path;
- transcript path;
- secret/token/environment values.

This contract permits new **sanitized derived public fields** in a later state-contract revision where needed. It does not permit simply exposing the raw provider hook fields.

## 16. Source capability and version drift

Provider hook capabilities evolve.

Business semantics are stable even if exact upstream events change.

Implementation must:

- audit the installed/current provider capabilities before each adapter expansion;
- advertise unavailable/degraded capability honestly;
- avoid fabricating data when an older provider version lacks a signal;
- keep the common card usable with partial provider information.

## 17. Read-only boundary

Even when a PermissionRequest or question is visible, Monitoring MVP does not answer it.

The card may say:

```text
ACTION REQUIRED
Approval needed on Mac mini
```

but MVP does not send allow/deny/answer back to Codex or Claude Code.

Remote response/control belongs to the post-monitoring control phase.

## 18. Acceptance examples

### Working with useful checkpoint

```text
Mac mini · CODEX
DevBoard
Network Health
WORKING · 8m
Running validation
```

### Claude native task progress

```text
MacBook · CLAUDE CODE
ProductTool
Analytics audit
WORKING · 21m
Task completed: inspect query race
```

### Attention

```text
Mac mini · CODEX
DevBoard
Network Health
ATTENTION · 9m
Approval needed · Bash operation
```

### Complete

```text
MacBook · CLAUDE CODE
ProductTool
Analytics audit
COMPLETE · 34m
Audit complete; 2 defects fixed; full tests pass.
```

### Partial source capability

```text
Mac mini · CODEX
Project X
WORKING · 4m
```

This remains valid when no safe checkpoint/title detail is available.

## 19. Freeze conclusion

Task observability V1 is frozen as:

```text
NO FULL STREAM
NO CHAIN-OF-THOUGHT MIRROR
NO FABRICATED PERCENT PROGRESS

COLLECT / DISPLAY ONLY HIGH-VALUE SIGNALS:

identity
+ lifecycle
+ significant checkpoint
+ actionable feedback
+ completion summary

provider-native richness when available
+ honest fallback when unavailable
```

**DEVBOARD AI TASK OBSERVABILITY V1 BUSINESS CONTRACT FROZEN.**