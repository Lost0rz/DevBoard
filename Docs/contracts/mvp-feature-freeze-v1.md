# DevBoard Monitoring MVP V1 — Feature Freeze

> Date: 2026-08-22  
> Parent contracts:
> - `Docs/contracts/mvp-monitoring-v1.md`
> - `Docs/contracts/agent-task-observability-v1.md`
> - `Docs/contracts/reference-first-integration-v1.md`
> Status: **MVP FEATURE SET FROZEN**
> Scope: defines what must exist before Monitoring MVP V1 is considered complete.

## 1. Product boundary

DevBoard Monitoring MVP V1 is a **multi-host, read-only AI work status board**.

It is not:

- a generic Activity Monitor replacement;
- a full AI conversation/transcript mirror;
- a remote approval/control system;
- a browser automation suite;
- a distributed orchestration platform.

The MVP prioritizes reliable monitoring and presentation first. External control/interaction is deferred until the monitoring product is stable.

## 2. Required MVP domains

### 2.1 Host System Health

Per monitored Mac:

- CPU utilization;
- memory used / total;
- swap used / total;
- disk utilization;
- source/host freshness and health.

M3.1 is the baseline implementation.

Per-process CPU/RAM attribution and Process Groups are explicitly deferred.

### 2.2 Host Network Health

Per monitored Mac:

- Internet reachability;
- latency/response-time signal;
- packet-loss or equivalent bounded reachability-loss indication, using honest source semantics;
- network quality state;
- current send/receive load where reliably available;
- source freshness/last-success state.

This is lightweight operational diagnosis, not continuous bandwidth benchmarking or full network observability.

### 2.3 AI Task Board

Initial providers:

- Codex;
- Claude Code.

Each task card should expose when reliably available:

- host;
- provider;
- safe project/worktree identity;
- safe short task title;
- lifecycle state;
- elapsed/age;
- latest significant checkpoint;
- concise actionable feedback when user intervention is needed;
- bounded completion summary after completion.

The task board is the primary product surface.

DevBoard must not expose hidden chain-of-thought, full prompts, full transcripts, arbitrary tool payloads, full shell commands, or full final responses.

### 2.4 Browser AI Watch

Initial scope: selected browser-based AI conversations.

MVP should expose when reliably observable:

- host;
- supported AI web service;
- safe conversation label;
- generating/working state;
- new reply/completed reply state;
- attention/waiting-for-user state;
- event age/time;
- optional bounded reply summary.

The MVP does not monitor arbitrary browser tabs or mirror full page content.

Additional non-browser apps such as messaging clients are post-MVP extensions.

### 2.5 AI Quota

Per provider/account identity where reliably distinguishable:

- quota window;
- remaining percentage;
- reset countdown/time;
- source health/freshness.

Multiple quota windows are valid.

Quota is provider/account-scoped rather than inherently host-scoped.

## 3. Multi-host requirement

Monitoring MVP V1 must support at least the two-Mac use case in one unified dashboard.

Host-scoped domains:

- system health;
- network health;
- Codex/Claude task state;
- browser AI watch state.

Provider/account-scoped quota must not be duplicated as independent capacity merely because the same account is observed on multiple hosts.

## 4. Web/display requirement

The canonical product is web-based and uses one normalized business state.

Required display classes:

- Kindle / old low-capability browser;
- tablet;
- phone;
- desktop browser.

Device adaptation changes layout/density, not business semantics.

Priority across surfaces:

1. actionable ATTENTION / actionable errors;
2. active AI tasks;
3. recent completion / browser new reply;
4. degraded system/network health;
5. quota warning;
6. normal passive metrics.

## 5. Read-only MVP boundary

Not required for Monitoring MVP closure:

- focus app/agent/project;
- remote approve/deny;
- answer a provider question from the board;
- stop/retry/continue;
- browser/app control;
- generic command execution;
- account switching.

Existing Safe Navigation design remains deferred and reusable after monitoring closure.

## 6. Deferred resource-manager scope

Explicitly out of Monitoring MVP V1:

- Process Groups;
- Codex-specific CPU/RAM attribution;
- Claude-specific CPU/RAM attribution;
- generic process list/history;
- child-process accounting;
- Activity Monitor replacement.

The existing M3.1 host-level health strip is sufficient for system-load diagnosis in MVP.

## 7. Construction policy

Every remaining module follows:

```text
business contract
→ proven reference audit
→ pull exact upstream/reference source where materially useful
→ inspect implementation/tests/license
→ reuse/adapt/build decision
→ technical contract
→ implementation
→ independent audit
→ real-device validation
```

Relevant known references include:

- official Codex hooks;
- official Claude Code hooks;
- `nicolargo/glances`;
- `shirou/gopsutil`;
- `steipete/CodexBar`;
- `Ericonaldo/AgentMonitor` as dashboard/reference material where applicable;
- `conol-ai/openmicrokbd` for later control/notification/integration patterns, especially post-MVP interaction work;
- other user-supplied or newly identified mature GitHub projects relevant to each module.

The frozen DevBoard business/state contracts remain authority; external projects are references/adapters, not product authority.

## 8. Frozen construction sequence

### M3.2 — Network Health

Lightweight per-host network diagnosis. No Process Groups.

### M4 — AI Task Observability

Project/worktree identity, safe task title, significant checkpoints, actionable feedback, completion summary, provider capability handling.

### M5 — Multi-Host Read-Only Dashboard

At least two monitored Macs in one unified read-only web state/view.

### M6 — Browser AI Watch

Selected AI browser-conversation status/new-reply monitoring using the least invasive proven mechanism.

### M7 — Quota

Provider/account quota remaining/reset integration, with CodexBar and other proven sources audited first.

### M8 — Monitoring MVP Display Closure

End-to-end reliability and adapted Kindle/tablet/phone/desktop presentation across all frozen MVP domains.

## 9. MVP completion gate

Monitoring MVP V1 is complete only when the unified dashboard reliably provides:

1. host system health;
2. host network health;
3. useful Codex/Claude task identity and lifecycle;
4. meaningful task checkpoints;
5. actionable feedback;
6. bounded completion summaries;
7. selected browser AI status/new replies;
8. quota remaining/reset;
9. at least two monitored hosts in one view;
10. Kindle/tablet/phone/desktop presentation;
11. source failures isolated and displayed honestly;
12. no external control required.

No additional feature family may be silently promoted into MVP without explicitly reopening this feature freeze.

**DEVBOARD MONITORING MVP V1 FEATURE SET FROZEN.**
