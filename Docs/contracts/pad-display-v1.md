# DevBoard Pad Landscape Display V1 Contract

> Date: 2026-08-23 (Asia/Shanghai)
> Status: **FROZEN PRODUCT / PRESENTATION CONTRACT**
> Surface: `/display`
> Device class: landscape Pad in a fullscreen browser
> Native panel: 1600 x 2560, used as 2560 x 1600 landscape (16:10)
> Authority: `Docs/contracts/mvp-monitoring-v1.md`, existing PublicState and DashboardState semantics

```text
PAD_DISPLAY_CONTRACT = FROZEN_V1
```

## 1. Purpose

The Pad display is a persistent, glanceable AI-work monitor. It is not a
desktop operations console, a project browser, a generic system dashboard, or
an administration surface.

At a glance, the user must be able to answer:

1. Is the Pad still receiving current data from the Hub and monitored Mac?
2. Which Codex or Claude Code task needs user action now?
3. Which tasks are currently working?
4. Which tasks completed recently?
5. Is the monitored Mac under abnormal CPU, memory, swap, or disk pressure?
6. Is AI quota low, or did a watched AI web conversation produce a new reply?

The first Pad closure is single-Mac-first. It must not hard-code `mac-a` or
remove the existing multi-host data model, but multi-Mac Pad composition is not
a V1 acceptance blocker.

## 2. Surface separation

`/display` is the canonical Pad-first monitoring surface.

`/display` MUST NOT become a settings or diagnostics page. Configuration and
operator details remain on:

- `/admin` for Hub registry and credential management;
- `/settings` for the local Mac Node configuration;
- `/display/kindle` for the separately frozen old-Kindle presentation.

The Pad surface MUST NOT expose edit, reset, enable, disable, pairing, token,
restart, approve, deny, stop, retry, prompt, or shell controls.

This contract does not modify `Docs/contracts/display-ux-v1.md` or the behavior
of `/display/kindle`.

## 3. Device and viewport contract

The target panel has a 1600 x 2560 native resolution and is mounted in
landscape, producing a 2560 x 1600 physical canvas with a 16:10 aspect ratio.

The implementation MUST respond to the browser's CSS viewport rather than
assuming that physical pixels equal CSS pixels. Primary acceptance viewports
are:

- 2560 x 1600 physical/full-resolution capture;
- 1280 x 800 CSS pixels, representing a common device-pixel-ratio mapping;
- 1024 x 640 CSS pixels as the minimum supported 16:10 fallback.

At all three acceptance sizes:

- there is no horizontal scroll;
- the normal monitoring state has no page-level vertical scroll;
- all populated operational regions remain visible in one fullscreen viewport;
  the AI Signals region collapses completely when both quota and web sources
  are unavailable;
- task text is bounded rather than allowed to expand the page;
- browser zoom at 100% is sufficient;
- no hover is required to understand current state.

Desktop and phone presentation quality is not part of this Pad V1 closure.

## 4. Frozen information architecture

The Pad contains up to four top-level operational regions:

1. compact Connection Strip;
2. dominant shared Agent Deck;
3. compact Host Health region;
4. compact AI Signals region containing Quota and Web notifications.

No Projects, Source diagnostics, branch/worktree diagnostics, raw lifecycle
logs, generic alerts feed, or navigation menu may appear as an additional
top-level region.

Recommended landscape ownership is:

```text
+------------------------------------------------------------------+
| CONNECTION STRIP                                      4-6%       |
+------------------------------------------------------------------+
|                                                                  |
| SHARED AGENT DECK: READY / WORKING / COMPLETE          58-64%    |
| +------------------+ +------------------+ +------------------+    |
| | Task card 1      | | Task card 2      | | Task card 3      |    |
| +------------------+ +------------------+ +------------------+    |
|                                                                  |
+--------------------------------------+---------------------------+
| HOST HEALTH                          | AI SIGNALS                 |
| CPU | MEMORY | SWAP | DISK           | QUOTA | WEB NOTIFICATIONS |
| 60-65% of lower band                 | 35-40% of lower band       |
+--------------------------------------+---------------------------+
```

The percentages are region-ownership constraints, not exact CSS values. The
implementation may tune gaps and typography while preserving the hierarchy:

```text
Agent Deck >> Host Health >= AI Signals >> Connection Strip
```

## 5. Connection Strip

The Connection Strip is a narrow status rail, not a header or branding band.

It contains only:

- Hub/page refresh health;
- monitored Mac connection/freshness;
- short Mac display name when available;
- last successful board update time or age;
- a small unobtrusive `DevBoard` marker if desired.

The preferred shape is equivalent to:

```text
HUB ● LIVE    MAC A ● ONLINE    UPDATED 8s AGO
```

Connection meaning must not rely on color alone. Every dot has a short text
label such as `LIVE`, `STALE`, or `OFFLINE`.

The strip MUST NOT contain CPU, network throughput, source messages, registry
actions, long error text, navigation tabs, or a large page title.

## 6. Shared Agent Deck

Codex and Claude Code share one provider-agnostic Agent Deck. There are no
fixed provider columns or reserved provider slots.

The Deck displays at most three task cards. Capacity is based on operational
priority, not provider identity.

- one task: one card spans the available deck width and uses a compact
  two-column internal layout where useful;
- two tasks: two equal cards;
- three tasks: three equal cards;
- zero tasks: no artificial empty cards are rendered; the Deck enters compact
  all-clear mode and yields vertical room to the lower regions;
- more than three eligible tasks: show the highest-priority three and a small
  `+N` hidden-task count in the Deck heading; do not add scrolling or an
  automatic carousel.

Provider identity is a small `CODEX` or `CLAUDE CODE` label inside each card.
Provider color may assist recognition but must not define status meaning.

## 7. Frozen Pad task states

The Pad exposes exactly three primary task states:

```text
WORKING
READY
COMPLETE
```

`READY` has a precise Pad meaning: **the task needs user attention or action**.
It does not mean idle, available, initialized, or ready to start.

Existing backend lifecycle semantics map to the Pad without changing
PublicState:

| Existing task fact | Pad primary state | Pad meaning |
| --- | --- | --- |
| `attention` or actionable alert | `READY` | approval, answer, elicitation, or other user action is required |
| `error` requiring intervention | `READY` | an error blocks or interrupts useful progress; error subtype receives strongest READY emphasis |
| `working` | `WORKING` | the assistant is actively processing or performing work |
| `complete` | `COMPLETE` | the top-level task/turn has finished recently |
| idle/no current task | no task card | idle is absence of work, not a fourth Pad task state |

Freshness such as `STALE` is a modifier, not a fourth primary task state. A
stale card must say `DATA STALE` and visually lose live certainty; it must not
silently claim that last-known work is still current. Source/host staleness is
also visible in the Connection Strip.

Amendment (2026-08-25): an error whose session later produced a newer turn
terminated by a valid terminal Stop is a recovered (superseded) error. It
requires no user action, produces no Pad task card, and never occupies a
READY slot; the Pad renders the newer turn's own terminal state instead.
Unrecovered errors keep the strongest READY emphasis and remain on the Pad
until a genuine recovery event or explicit operator action.

## 8. State transitions

The presentation state machine is:

```text
no card  -> WORKING       when a visible top-level task starts
WORKING  -> READY         when approval, a question, elicitation, or an actionable error is observed
READY    -> WORKING       when the blocking attention is resolved and work resumes
WORKING  -> COMPLETE      when the top-level task completes
READY    -> COMPLETE      when the task completes or terminates with a final result
COMPLETE -> WORKING       when the same visible work is authoritatively resumed
COMPLETE -> no card       after retention expiry
any      -> stale modifier when source freshness expires
```

The UI must not fabricate a transition from elapsed time, a missing optional
field, or provider asymmetry. Codex and Claude Code may expose different
details while preserving these three meanings.

## 9. Selection and ordering

The Deck ordering is deterministic and frozen as:

1. `READY` caused by actionable error;
2. other `READY` tasks requiring approval, answer, or elicitation;
3. `WORKING` tasks;
4. high-visibility recent `COMPLETE` tasks;
5. muted retained `COMPLETE` tasks.

Tie-breaking rules:

- READY: longest waiting first;
- WORKING: most recently updated first;
- COMPLETE: most recently completed first;
- final deterministic tie: safe host label, provider, then opaque internal key
  that is not rendered.

READY always displaces WORKING and COMPLETE when Deck capacity is full.
WORKING always displaces muted COMPLETE. Provider identity never changes
priority.

## 10. COMPLETE decay and exit

Pad V1 uses the existing configured completion durations:

```text
complete_high_visibility_seconds = 600   # 10 minutes
complete_retention_seconds       = 1800  # 30 minutes total
```

The frozen behavior is:

- 0-10 minutes: COMPLETE is clearly visible and may occupy a normal Deck slot;
- 10-30 minutes: COMPLETE becomes visually quieter and fills only capacity not
  required by READY or WORKING;
- after 30 minutes: COMPLETE exits the Deck;
- under queue pressure, READY and WORKING may remove COMPLETE from the visible
  three immediately without changing its underlying retention state;
- no manual acknowledgement or dismiss control is introduced in Pad V1.

Completion age is based on authoritative task timestamps, not browser-local
animation time.

## 11. Task card content contract

Every task card may contain only the following presentation hierarchy:

1. primary status (`READY`, `WORKING`, or `COMPLETE`);
2. provider (`CODEX` or `CLAUDE CODE`);
3. bounded safe task title, maximum two visual lines;
4. one state-specific bounded detail:
   - READY: actionable attention/error text;
   - WORKING: latest meaningful checkpoint when available;
   - COMPLETE: bounded completion summary when available;
5. compact footer with safe Mac label and elapsed time or completion age.

When a state-specific detail is unavailable, the card omits it or uses a
short honest fallback. It must not invent progress or repeat diagnostics.

Task cards MUST NOT render:

- branch, worktree, repository path, cwd, or absolute path;
- raw session, turn, event, task, or result identifiers;
- prompt, transcript, chain of thought, tool input, or tool output;
- full completion responses;
- source diagnostic messages;
- percentages of task completion;
- controls or navigation actions.

## 12. Host Health region

The Host Health region sits directly below the Agent Deck and contains exactly
four primary machine facts for the monitored Mac:

```text
CPU | MEMORY | SWAP | DISK
```

Frozen meanings:

- CPU: current utilization percent;
- MEMORY: used/total with percent where available;
- SWAP: used/total with percent where available;
- DISK: utilization percent for the monitored system volume/public disk fact.

Each fact is one compact tile or cell. Charts, history, sparklines, per-process
data, temperature, fan speed, load average, and network metrics are excluded.

Normal values remain visually quiet. Warning/unavailable states receive
emphasis. Missing values render `--` or `UNAVAILABLE`; values are never
fabricated.

The region may include one short host label and freshness label. It must not
repeat the entire Connection Strip or become a diagnostics panel.

## 13. AI Signals region

The AI Signals region combines two compact subdomains:

### 13.1 AI Quota

For every usable real quota window, show only:

- provider/account label when reliably known;
- remaining percentage with explicit `LEFT`/remaining meaning;
- reset countdown when known;
- freshness when not current.

No quota estimate or fabricated placeholder percentage is allowed.

### 13.2 Web notifications

For a selected supported AI web conversation, show only bounded operational
notifications:

- AI service/provider;
- `WORKING`, `NEW REPLY`, or `READY`/attention meaning when authoritative;
- fixed safe conversation label;
- event age/freshness.

The Pad must not render page title, URL, conversation ID, prompt, reply text,
transcript, cookie, account token, or browser diagnostics.

Quota and Browser AI Watch are consumed only when real sanitized sources are
available. This Pad contract does not implement those collectors.

## 14. Empty and unavailable states

Empty-state presentation must not reserve large blank panels.

- No tasks: render one compact all-clear message and allow the lower regions
  to use the released vertical space.
- No quota source: omit the quota subdomain rather than reserving a placeholder
  panel.
- No browser source: omit the web subdomain rather than reserving a placeholder
  panel.
- Neither quota nor browser source: collapse the AI Signals region entirely and
  allow Host Health to use the full lower width.
- Host data unavailable: keep the four metric labels and show unavailable
  values so loss of monitoring is explicit.
- Pad refresh failure: preserve last-good content, mark the Connection Strip
  stale/offline, and never replace the board with a blank screen.

No skeleton loader, oversized illustration, marketing copy, setup tutorial,
or large empty card belongs on the persistent Pad board.

## 15. Refresh and stability

The Pad uses the existing server-rendered fragment as the display authority.
Browser JavaScript may replace the fragment but must not reimplement task
selection or state reduction.

Frozen behavior:

- refresh cadence: existing 2-second dashboard cadence;
- initial state: complete SSR render;
- subsequent state: local same-origin fragment refresh;
- one failed refresh retains last-good DOM;
- refresh health changes the small Connection Strip immediately;
- recovery replaces the fragment with current SSR state;
- no full-page meta refresh for Pad;
- no WebSocket requirement in V1;
- no automatic task carousel or layout animation that harms glance stability.

State reordering may use a short restrained transition, but READY must appear
without waiting for decorative animation.

## 16. Visual and interaction constraints

The Pad is normally viewed from a distance and is not an interactive desktop.

Required:

- large readable task state and title;
- high contrast in fullscreen ambient use;
- status meaning expressed by text plus shape/icon/border, not color alone;
- READY visually outranks WORKING; WORKING outranks retained COMPLETE;
- dense but breathable spacing with no large decorative whitespace;
- consistent card geometry across Codex and Claude Code;
- bounded text with deterministic line clamping;
- touch-safe behavior even though no action is required for monitoring.

Forbidden:

- large product hero/header;
- sidebar or desktop navigation bar;
- hover-only information;
- modal dialogs;
- dense tables;
- ornamental charts;
- animated backgrounds;
- remote fonts, CDN assets, or client framework dependency;
- status meaning conveyed only by red/green color.

## 17. Privacy and security boundary

Pad rendering consumes sanitized public state only.

It MUST NOT expose:

- Node/admin/provider credentials or token-configured secrets;
- private filesystem paths;
- prompts, replies, transcripts, hidden reasoning, or raw browser content;
- raw hook payloads or provider configuration;
- cookies, Authorization values, account/session identifiers;
- internal stack traces or unbounded errors.

The Pad remains read-only. It introduces no control path from Hub to Mac.

## 18. Acceptance scenarios

Pad V1 is accepted only after all scenarios are captured at the primary
1280 x 800 CSS viewport and checked at 2560 x 1600 physical resolution:

1. no task, healthy Mac, no quota/browser sources;
2. one Codex WORKING task;
3. one Claude Code READY task with actionable text;
4. three mixed tasks ordered READY -> WORKING -> COMPLETE regardless of
   provider;
5. more than four eligible tasks with correct top-four selection and `+N`;
6. recent COMPLETE, muted retained COMPLETE, and expiry after retention;
7. stale/offline Mac with last-good task and host data preserved honestly;
8. abnormal CPU/memory/swap/disk values without expanding the page;
9. real quota available and web source unavailable;
10. web `NEW REPLY`/attention notification and quota unavailable;
11. fragment refresh failure followed by recovery;
12. privacy sentinels absent from rendered HTML and browser console.

Every scenario must pass:

- no page-level horizontal or vertical scroll;
- no overlap or clipped primary status;
- no empty placeholder card;
- READY recognizable from across the intended viewing distance;
- Codex and Claude Code using the same card structure;
- browser console free of errors;
- local assets only.

## 19. Implementation boundary for the next construction batch

The Pad implementation should normally be contained within:

```text
internal/web/templates/display.html
internal/web/templates/dashboard_fragment.html
internal/web/static/app.css
internal/web/static/dashboard.js
internal/web/network.go
internal/web/product_ui_test.go
```

Existing display-focused tests may be updated where required by the frozen Pad
behavior.

No implementation batch may silently change PublicState, DashboardState,
Node/Hub transport, registry/auth, hook ingestion, system collectors, quota
collectors, Browser AI Watch, `/admin`, `/settings`, or `/display/kindle` to
make the presentation easier. A missing data field must be reported as a
contract gap before expanding scope.

## 20. Explicit non-goals

Pad Display V1 does not implement or close:

- Kindle redesign;
- phone or desktop dashboard optimization;
- Hub Admin or Node Settings redesign;
- Mac LaunchAgent/hook integration acceptance;
- Browser AI Watch collection;
- AI Quota collection;
- Mac B or final multi-Mac Pad composition;
- task control, Safe Navigation, or remote actions;
- historical metrics, analytics, or task transcript browsing.

## 21. Freeze marker

```text
PAD_DISPLAY_CONTRACT = FROZEN_V1
PAD_PRIMARY_ORIENTATION = LANDSCAPE
PAD_PHYSICAL_CANVAS = 2560x1600
PAD_PRIMARY_CSS_VIEWPORT = 1280x800
PAD_TASK_STATES = READY|WORKING|COMPLETE
PAD_AGENT_DECK_CAPACITY = 4
PAD_COMPLETE_HIGH_VISIBILITY_SECONDS = 600
PAD_COMPLETE_RETENTION_SECONDS = 1800
PAD_SURFACE_IS_READ_ONLY = TRUE
```

Any change to the compact region hierarchy, three-state meaning, four-card
capacity, READY-first ordering, completion decay, single-viewport requirement,
or privacy boundary requires a new reviewed contract revision rather than an
unrecorded frontend change.

## 22. Responsive compact-surface addendum (2026-08-25)

The single-node Pad implementation now carries a reviewed responsive addendum:

- the visible task grid may show up to four cards; READY/WORKING/COMPLETE
  ordering and the completion retention rules are unchanged;
- `Agent Deck` and `Host Health` are semantic regions with visually minimal
  headings, not explanatory title blocks; empty quota/web areas collapse;
- every task card identifies its host and provider in a compact identity line;
  host markers use a bounded registry palette and provider chips use the
  provider colour family (Claude orange, Codex blue);
- quota windows are rendered as remaining-percent rings (green, warning
  yellow, or empty black), while account identity remains the immutable
  HMAC-derived key and the canonical alias remains allow-listed;
- connected quota accounts are rendered as three vertically stacked account
  rows, each retaining four horizontal window rings; the rows use distinct
  bounded accent blocks so Codex A, Codex B, and GLM remain scannable without
  exposing account identity;
- when two hosts are visible, Host Health uses the lower-left region with one
  accent-colored card per host; each host keeps CPU, Memory, Swap, and Disk in
  a two-by-two compact grid before any secondary detail yields;
- card content uses container-relative sizing and bounded internal overflow;
  the three major rows use fractional tracks rather than viewport-sized pixel
  reservations, and metric/ring sizes derive from their own container width;
  the document itself must remain exactly within the viewport at the frozen
  1280x800, 1024x640, and 2560x1600 acceptance sizes. Secondary reset/detail
  copy yields before a primary glyph can be clipped.

This addendum changes presentation capacity and responsive rules only. It does
not change task semantics, transport, privacy, or the canonical quota alias
governance.
