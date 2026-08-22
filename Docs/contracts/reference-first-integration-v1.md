# DevBoard Reference-First Integration V1 — Construction Contract

> Date: 2026-08-22  
> Parent: `Docs/contracts/mvp-monitoring-v1.md`  
> Status: **FROZEN CROSS-CUTTING CONSTRUCTION CONTRACT**  
> Scope: how DevBoard uses proven GitHub/open-source/reference implementations during Monitoring MVP construction.

## 1. Principle

DevBoard follows a **reference-first, contract-first** construction policy.

For every Monitoring MVP module, the implementation sequence is:

```text
business requirement
→ frozen business contract
→ reference/source audit
→ choose reuse/adapt/build decision
→ technical contract
→ implementation
→ independent audit
→ real-device validation
```

The team MUST NOT begin by designing a custom hook, collector, browser watcher, quota adapter, transport, or dashboard mechanism when a proven implementation or upstream-supported integration already exists and can satisfy the frozen business semantics.

The objective is not to maximize code reuse. The objective is to avoid unnecessary complexity and avoid repeating already-solved integration work.

## 2. Reference sources

Reference candidates include, in priority order:

1. provider/vendor official documentation, SDKs, hooks, APIs, and maintained examples;
2. repositories/projects previously supplied or approved by the user as successful references;
3. mature open-source projects with demonstrated use of the required integration;
4. existing DevBoard code and previously frozen adapters/contracts;
5. custom implementation only for the remaining uncovered gap.

Known adjacent references already present in DevBoard planning include:

- Codex / Codex hooks and official lifecycle capabilities;
- Claude Code hooks and official lifecycle capabilities;
- CodexBar for quota-related reference work;
- gopsutil for embedded host metrics;
- Glances as a possible reference/adapter source for remote or broader machine monitoring, not as a mandatory local daemon.

This list is not exhaustive. Additional previously supplied or newly identified repositories should be added to the module's Reference Audit when the relevant milestone begins.

## 3. Mandatory timing

Reference research is not a final cleanup task. It happens **before technical implementation is frozen** for a module.

For each module:

### Gate A — Business freeze

First freeze what the user actually wants to see/do.

Do not search for an implementation and then let that project's feature set redefine DevBoard's business semantics.

### Gate B — Reference audit

After business semantics are frozen and before implementation begins:

- identify relevant official capabilities;
- identify previously supplied reference repositories;
- search GitHub for mature implementations where useful;
- inspect architecture and critical source paths, not only README claims;
- establish version/maintenance/license status;
- determine which parts directly solve the frozen requirement.

### Gate C — Remote source intake

If a candidate is materially useful, the implementation assistant should fetch/clone the exact upstream source at this stage for code-level audit.

Reference source should normally be pulled into an external temporary/sibling audit location, not silently copied into the DevBoard repository.

The audit must record the exact repository and immutable commit/tag used for comparison.

### Gate D — Reuse decision

For each useful reference, choose explicitly:

```text
USE DIRECTLY
ADAPT A SMALL COMPONENT/PATTERN
USE AS BEHAVIORAL REFERENCE ONLY
REJECT
```

Reasons must be recorded.

### Gate E — Technical contract

Only after the reference audit should the technical route be frozen.

The technical contract should prefer the shortest proven path that preserves DevBoard's business, privacy, portability, and state-authority contracts.

### Gate F — Integration

During implementation, bring in only the selected dependency/component/pattern required by the contract.

Do not bulk-copy an upstream project merely because it is useful as a reference.

## 4. Pulling reference code on the remote implementation environment

When a module reaches Gate C, the remote coding assistant is expected to obtain the relevant source directly rather than reconstructing it from memory.

Preferred workflow:

```text
1. fetch DevBoard and verify expected base SHA
2. fetch/clone selected reference repository separately
3. pin exact upstream commit/tag
4. inspect relevant implementation files/tests
5. compare behavior against DevBoard frozen contract
6. write the reuse decision
7. only then modify DevBoard
```

Reference checkout must not alter DevBoard's branch history.

No reset, rebase, force-push, or blind subtree import is justified by reference intake.

## 5. What may be reused

Acceptable reuse includes:

- official supported API/hook/event shapes;
- a maintained library dependency;
- a narrowly scoped parser/adapter with compatible license;
- tested retry/backoff/polling semantics;
- event normalization ideas;
- browser detection/state-machine techniques;
- network health measurement techniques;
- quota source adapters;
- old-browser rendering compatibility patterns;
- test fixtures and edge-case knowledge where license permits;
- architecture patterns proven by an existing implementation.

A successful reference does not automatically become DevBoard architecture authority.

DevBoard's frozen contracts remain authority.

## 6. What must not be copied blindly

Do not blindly import:

- entire applications;
- unrelated frameworks;
- generic plugin systems;
- broad process monitors;
- execution/control capabilities outside the current milestone;
- raw transcript/logging pipelines when DevBoard only needs bounded task signals;
- browser automation stacks when a smaller supported signal source exists;
- dependencies whose maintenance/toolchain cost exceeds the module value;
- code whose license or provenance has not been checked.

Reuse must reduce total complexity, not move complexity into an opaque dependency.

## 7. License and provenance gate

Before source code is copied or materially adapted, record:

- repository URL;
- exact commit/tag;
- license;
- copied/adapted files or concepts;
- attribution/license obligations;
- whether a dependency is linked/imported versus source copied.

If license compatibility is unclear, use the project as behavioral/architectural reference only until resolved.

Official API/event documentation may be used to implement compatible behavior without copying unrelated implementation code.

## 8. Reference Registry

Each implementation milestone that uses external references should maintain a compact Reference Audit in its construction report or contract.

Minimum record:

```text
REFERENCE
- name
- repository / official source
- immutable revision/version
- license (when code reuse is possible)
- relevant module/files
- useful proven behavior
- decision: direct / adapt / behavioral / reject
- DevBoard-specific differences
```

Previously supplied user reference projects are first-class candidates and should be re-opened when their relevant module is reached rather than forgotten after early planning.

## 9. Module-specific application

### M3.2 — Network Health

Before implementation:

- audit proven lightweight local network-health collectors/techniques;
- audit relevant Glances/system-monitor implementations for semantics if useful;
- prefer OS/library-supported counters and bounded reachability/latency checks;
- do not build a custom full network-monitoring stack.

### M4 — AI Task Observability

Before adapter expansion:

- re-audit current Codex official hooks/capabilities;
- re-audit current Claude Code official hooks/capabilities;
- revisit any user-supplied AI status/hook/dashboard reference repositories;
- inspect how successful projects identify task state, attention, completion, subagents, and task nodes;
- use upstream-supported events before inventing polling/scraping;
- reuse/adapt proven normalization logic only where it fits DevBoard's bounded privacy contract.

This is especially important because DevBoard must avoid a large bespoke hook integration when official/provider-native events already provide the needed signal.

### M5 — Multi-Host Read-Only Dashboard

Before transport/state aggregation implementation:

- audit proven lightweight multi-node heartbeat/snapshot patterns;
- prefer simple authenticated state transport over a distributed system;
- reuse mature primitives where they reduce reliability/security risk.

### M6 — Browser AI Watch

This milestone MUST have a reference audit before architecture freeze.

Before inventing DOM scraping or browser automation:

- revisit previously supplied browser/AI-monitor reference projects;
- search GitHub for maintained implementations for the selected browser/service pair;
- audit browser extension APIs, accessibility/notification surfaces, service workers, DOM/state observers, and other supported mechanisms as applicable;
- choose the least invasive proven signal source;
- avoid broad screen scraping when a supported or narrower integration exists.

If a successful reference implementation already solves the selected ChatGPT/browser state detection reliably, DevBoard should adapt/integrate that mechanism rather than independently rebuilding it.

### M7 — Quota

CodexBar and other proven quota readers are explicit reference candidates.

Before implementing quota collection:

- pull and inspect the current relevant CodexBar source/revision;
- identify the exact source of quota truth and refresh/reset semantics;
- reuse/adapt the narrowest reliable mechanism compatible with DevBoard;
- avoid recreating reverse-engineered quota logic if a maintained implementation already exists.

### M8 — Display Closure

Reuse the existing frozen Kindle implementation and proven old-WebKit patterns before introducing new rendering machinery.

Tablet/phone/desktop adaptation should share business state and avoid separate duplicated clients unless a proven constraint requires it.

## 10. Reference code and DevBoard ownership

Even when source is reused, DevBoard owns its normalized domain semantics.

External projects are adapters/implementation evidence, never PublicState authority.

Examples:

```text
Codex/Claude events
→ source facts
→ DevBoard normalized Task State

Browser watcher reference implementation
→ source signal
→ DevBoard Browser AI state

CodexBar
→ quota source technique
→ DevBoard normalized quota state
```

This preserves replaceability when upstream projects change.

## 11. Update policy

Do not continuously chase upstream HEAD.

At construction time:

- pin an audited version/commit;
- implement against that evidence;
- record known version assumptions.

Re-audit upstream only when:

- beginning a new relevant milestone;
- an integration breaks;
- a provider changes its API/hook behavior;
- a security/compatibility issue requires it;
- a materially better maintained solution becomes available.

## 12. Construction report requirement

Every module implementation report should include:

```text
REFERENCE_AUDIT
- sources inspected
- revisions
- reuse decisions
- code actually reused/adapted
- license/provenance result
- custom code remaining and why it was necessary
```

A module should not be considered architecture-ready if a clearly relevant proven reference exists but was never inspected.

## 13. Freeze conclusion

DevBoard Monitoring MVP construction is frozen as:

```text
DO NOT REINVENT FIRST

Business Contract
→ Proven Reference Audit
→ Pull exact upstream source when useful
→ Audit code + tests + license
→ Reuse/adapt the smallest reliable piece
→ Freeze DevBoard technical contract
→ Implement
→ Independent audit
→ Real-device validation
```

This policy applies to Network Health, AI Task Observability, Multi-Host, Browser AI Watch, Quota, and display closure.

**DEVBOARD REFERENCE-FIRST INTEGRATION V1 CONTRACT FROZEN.**
