# DevBoard Monitoring MVP V1 — Business Contract

> Date: 2026-08-22  
> Base implementation: `codex/m3-1-system-metrics-foundation` @ `00ebfc7ee72e4b477cbf23eff6f5721457e0147a`  
> Status: **BUSINESS CONTRACT FROZEN**  
> Scope: read-only monitoring MVP product semantics. Implementation details remain milestone-specific.

## 1. Product definition

DevBoard MVP is a **multi-host AI work status board**, not a generic system monitor and not a transcript viewer.

Its primary job is to answer, at a glance:

1. Are the monitored Macs and their network healthy enough to work normally?
2. What are Codex and Claude Code currently doing?
3. Which AI task needs the user's attention now?
4. Which task just finished, and what is the short completion result?
5. Did a selected browser AI conversation produce a new reply or require attention?
6. How much AI usage quota remains and when does it reset?

The MVP is **monitor first, control later**.

No interaction with external agents/apps is required for MVP closure.

## 2. The four MVP information domains

The product is divided into four user-facing domains.

### A. Host Health

Purpose: diagnose whether machine/network conditions may explain slow or unreliable AI development work.

Host Health contains two subdomains:

#### A1. System Health

Per monitored host:

- CPU utilization;
- memory used / total;
- swap used / total;
- disk utilization;
- host connectivity/source health;
- last update freshness.

M3.1 already establishes the first system-health baseline.

Process-level resource accounting is not part of the monitoring MVP.

DevBoard does not need to answer which individual process consumes the RAM/CPU; macOS Activity Monitor already serves that purpose.

#### A2. Network Health

Per monitored host, the board should show enough information to answer whether network quality may be affecting AI work.

Business-level signals are:

- Internet reachability;
- current latency / response time;
- packet-loss indication;
- network quality status (`good`, `degraded`, `unavailable` or equivalent presentation);
- current send/receive load when reliably available;
- freshness / last successful measurement.

Continuous speed tests are not an MVP requirement.

The purpose is operational diagnosis, not full network observability.

### B. AI Task Board

Purpose: show meaningful progress for local AI coding work without mirroring the full conversation.

Initial coding providers:

- Codex;
- Claude Code.

Each visible task should answer, when source data exists:

- which host is running it;
- which provider is running it;
- which project/worktree it belongs to;
- a short task title/identity;
- current lifecycle status;
- latest significant progress/checkpoint;
- elapsed time / age;
- whether user action is required;
- short actionable feedback when user action is required;
- short completion summary after completion.

The task board is the primary content region of DevBoard.

### C. Browser AI Watch

Purpose: surface important state changes from selected AI conversations used in a browser.

The first MVP concerns **selected browser AI conversations**, not arbitrary browser tabs and not general desktop notifications.

A watched browser conversation may expose, when observable:

- host;
- supported AI web service;
- safe conversation label;
- `working/generating` state;
- `new reply / completed reply` state;
- `attention` state when the web experience is waiting for user input;
- event time / age;
- optional bounded reply summary where safely and reliably obtainable.

The MVP must not mirror the full page or conversation transcript into DevBoard.

The browser-source design must remain extensible so future application adapters (for example messaging software) can reuse the same source/event concept, but non-browser apps are not MVP scope.

### D. AI Quota

Purpose: answer whether an AI assistant/account is approaching a usage limit before work is interrupted.

The quota area should show, where the source can provide it:

- provider;
- account identity/label when reliably distinguishable;
- each active quota window;
- remaining percentage;
- reset countdown/time;
- source status/freshness.

The operational meaning is **remaining quota**, not ambiguous raw usage percent.

Multiple windows for one provider/account remain valid.

The MVP is read-only; it does not switch accounts or purchase/modify plans.

## 3. Task Board information hierarchy

The user does not need the agent's complete stream of reasoning.

The task board intentionally retains only information with operational value.

### Tier 1 — always important

- provider;
- host;
- project/task identity;
- status: working / attention / error / complete / stale or equivalent existing semantics;
- elapsed time.

### Tier 2 — significant progress checkpoint

A short statement describing the latest meaningful observable work stage, for example:

- inspecting/researching;
- editing/implementing;
- running validation/tests;
- waiting on a tool/background task;
- delegated/subagent work;
- a provider-native named task started/completed.

A checkpoint is **not** a percentage-complete estimate.

DevBoard MUST NOT invent `40% done`, `almost finished`, or similar progress if the provider does not supply authoritative progress.

### Tier 3 — user intervention

When the task requires the user, ATTENTION should include a bounded actionable summary where the provider exposes enough information, for example:

- permission/approval required;
- question requiring an answer;
- elicitation required;
- rate limit/authentication/billing/provider error;
- a blocked task that needs user choice.

The board should explain **what kind of action is needed and enough context to identify it**, without copying an entire prompt/tool payload.

### Tier 4 — completion delivery

When a top-level task/turn finishes, the board should retain a bounded completion summary containing the most useful final facts, when available, such as:

- what was completed;
- important validation result;
- important failure/limitation;
- final branch/commit/result identifier if part of the provider's user-visible completion response.

The completion summary is a compact derivative, not a full assistant response or transcript.

## 4. Content boundary: no chain-of-thought mirror

The monitoring MVP explicitly does NOT ingest/display a continuous stream of hidden reasoning or all assistant text.

DevBoard does not need:

- hidden chain of thought;
- token-by-token text;
- every tool input/output;
- every shell command;
- full prompts;
- full transcripts;
- full final responses;
- complete browser-page content.

Useful provider-visible metadata and bounded user-facing summaries are allowed when they directly support task identity, progress, attention, or completion.

Derived summaries must be short and sanitized.

## 5. Provider asymmetry is allowed

Codex and Claude Code do not expose identical observability events.

The MVP MUST NOT fabricate symmetry.

If Claude Code exposes a provider-native task subject/completion event and Codex exposes only tool/lifecycle checkpoints, the UI may show richer Claude progress while preserving the same overall card hierarchy.

Unavailable detail is omitted or rendered unavailable; it is not inferred as fact.

Provider capability differences must not change the core meaning of WORKING / ATTENTION / COMPLETE.

## 6. Task identity

A visible AI task is a human-useful projection of one monitored top-level coding turn/work item.

The board should prefer, in order of reliability:

1. provider-native task/session title or task subject;
2. explicit user-defined label if supported later;
3. bounded sanitized title derived from submitted task context;
4. project + provider fallback when no safe title can be established.

Raw submitted prompt text is not a public board field.

A generated/derived task title is not authoritative conversation content.

## 7. Project/worktree context

Project/worktree context is valuable because `CODEX · WORKING` alone is insufficient when multiple coding tasks run concurrently.

The MVP task card should identify project/worktree using safe public identity, for example display name and branch where useful.

Absolute filesystem paths remain private.

Full Git diagnostics are secondary to task identity and do not need to dominate the task card.

## 8. Multi-host business contract

The monitoring MVP must support a **single read-only dashboard covering multiple monitored hosts**, with at least the two-Mac use case in mind.

Host identity is first-class.

Host-scoped information:

- system health;
- network health;
- local Codex/Claude tasks;
- browser AI watch events.

Quota information is provider/account-scoped rather than inherently host-scoped. The UI should avoid presenting the same account quota as independent capacity merely because two hosts observe it.

The user should be able to answer:

> Which machine is this task/problem on?

without opening another page.

## 9. Display-device contract

The canonical product is web-based.

Supported display classes:

- Kindle / old low-capability browser;
- tablet;
- phone;
- desktop browser.

All surfaces consume the same business state. Device adaptation changes density/layout, not state semantics.

### Kindle

Purpose: persistent glanceable appliance.

Priorities:

1. urgent attention;
2. active/recent AI tasks;
3. compact host/network health;
4. compact quota.

Keep SSR/high-contrast/old-browser compatibility from the frozen Kindle contract.

### Tablet

Purpose: primary persistent DevBoard screen.

It may show multiple hosts and more task checkpoint/summary detail simultaneously.

### Phone

Purpose: quick remote status check.

Prioritize attention, active tasks, recent completion, host/network warning, then quota.

### Desktop

Purpose: richest diagnostic read-only surface.

May expose more source health and detail than Kindle/phone while respecting public privacy boundaries.

## 10. Attention priority

Across all monitored source types, information needing user action outranks passive information.

Presentation priority:

1. ATTENTION / actionable error;
2. actively working tasks;
3. recent completion/new browser reply;
4. degraded host/network health;
5. quota warning;
6. normal background status.

A system CPU number must not visually compete with an AI task waiting for user confirmation.

## 11. Recent-event retention

Operational events remain visible long enough to be noticed even if the user was away from the display.

Existing completion retention semantics remain a valid baseline.

The same product principle applies to browser new-reply events and important warnings: a brief event should not disappear before a reasonable dashboard refresh can deliver it.

Exact retention durations are configuration/technical contract details unless already frozen elsewhere.

## 12. Read-only MVP boundary

The monitoring MVP does NOT perform external side effects.

Deferred until monitoring is stable:

- focus app/agent/project actions;
- remote approve/deny;
- answer a question from the dashboard;
- stop/retry/continue;
- browser interaction;
- account switching;
- generic remote control;
- arbitrary command execution.

Normal dashboard-only navigation/filtering is allowed because it does not affect monitored applications.

The existing Safe Navigation design remains reusable later but is not required for Monitoring MVP closure.

## 13. Explicitly deferred resource-manager scope

The prior Process Groups direction is deferred from the monitoring MVP.

Not required:

- Codex CPU/RAM attribution;
- Claude CPU/RAM attribution;
- child-process accounting;
- generic process lists;
- per-process history;
- Activity Monitor replacement.

M3.1 host health remains useful as a compact machine-level diagnostic signal.

## 14. Source truth and honesty

Every displayed fact must distinguish:

- observed fact;
- derived/sanitized summary;
- unavailable/unknown.

DevBoard must not invent missing progress, completion, browser state, network state, or quota data.

Provider/source failures are isolated so one unavailable source does not erase unrelated valid state.

## 15. Monitoring MVP completion definition

Monitoring MVP V1 is complete when one unified web dashboard can reliably present:

1. per-host system health;
2. per-host network health;
3. Codex/Claude task cards with useful task identity and significant checkpoints;
4. clear actionable attention events;
5. bounded completion summaries;
6. selected browser AI new-reply/status events;
7. AI quota remaining/reset state;
8. at least two monitored hosts in one view;
9. responsive/adapted Kindle, tablet, phone, and desktop surfaces;
10. no external control/approval actions required.

## 16. Revised business roadmap

Closed foundation:

- M1 — Core / PublicState / mock display;
- M2 — agent lifecycle ingestion;
- M2.3 — display UX foundation;
- M3.1 — host system health.

Monitoring MVP construction sequence:

### M3.2 — Network Health

Add compact per-host network quality/load signals. No process groups.

### M4 — AI Task Observability

Expand Codex/Claude observable task context:

- project/worktree identity;
- safe task title;
- significant checkpoints;
- actionable feedback;
- bounded completion summary;
- provider capability/freshness handling.

### M5 — Multi-Host Read-Only Dashboard

Unify multiple monitored DevBoard hosts into one read-only state/view with explicit host identity.

### M6 — Browser AI Watch

Add selected browser AI conversation status/new-reply monitoring, starting with narrowly supported browser/service combinations rather than generic screen scraping.

### M7 — Quota

Integrate provider/account quota windows and remaining/reset presentation.

### M8 — Monitoring MVP Display Closure

Complete responsive/density adaptation and end-to-end reliability across Kindle/tablet/phone/desktop, with multi-host/task/browser/quota/system/network state together.

### After Monitoring MVP

Only after the read-only board is reliable:

- Safe Navigation;
- remote confirmation/approval;
- stop/retry/continue;
- app/browser control;
- additional non-browser application notification sources.

## 17. Freeze conclusion

DevBoard Monitoring MVP V1 is frozen as:

```text
MULTI-HOST READ-ONLY AI WORK STATUS BOARD

Host Health
  = System + Network

AI Task Board
  = task identity + status + significant checkpoint
    + actionable feedback + completion summary

Browser AI Watch
  = selected AI conversation status/new reply

Quota
  = remaining + reset + source health

Web surfaces
  = Kindle + Tablet + Phone + Desktop

Interaction/control
  = deferred until monitoring is stable
```

**DEVBOARD MONITORING MVP V1 BUSINESS CONTRACT FROZEN.**