# DevBoard Display UX V1 Contract

> Milestone: M2.3 — Display UX Contract + Kindle Dynamic Agent Deck
> Status: frozen operational presentation contract
> Scope: presentation only; lifecycle ingestion, reducer semantics, collection, quota adapters, navigation runtime, and real provider hook installation are out of scope.

## 1. Surface roles

### Kindle

`/display/kindle` is an operational status display, not a diagnostics screen. It contains only:

1. compact System status bar;
2. one shared Agent Deck for all providers;
3. compact Quota rail;
4. a future browser reply surface only after that capability is explicitly implemented.

Kindle MUST NOT expose Hook source health, hook installation state, raw SourceHealth messages, internal diagnostics, raw provider data, unsupported future features, private paths, or opaque lifecycle/navigation internals.

### Desktop/mobile display

`/display` is the richer diagnostic surface. Its primary hierarchy is Agent → System → Quota. It MAY additionally show active alerts, Hook source health, diagnostics, and public project information.

## 2. Kindle orientation query contract

Supported forms include:

- `/display/kindle?layout=landscape&rotate=none`
- `/display/kindle?layout=landscape&rotate=left`
- `/display/kindle?layout=landscape&rotate=right`
- `/display/kindle?layout=portrait&rotate=none`

`layout` controls information layout and Agent Deck capacity:

- `landscape`: maximum 3 visible Agent cards;
- `portrait`: maximum 2 visible Agent cards.

Invalid or missing `layout` safely falls back to `landscape`.

`rotate` controls only physical page rotation. Allowed values are `none`, `left`, and `right`. Invalid or missing values safely fall back to `none`. Arbitrary query values MUST NOT be reflected into CSS classes or rendered output.

Rotation uses old-WebKit-compatible `-webkit-transform` plus standard `transform`. Modern JavaScript is not required.

## 3. Shared Agent Deck

There are no provider-specific slots. Codex and Claude Code sessions compete in one deterministic queue. Provider is a card label only; it never reserves a column or changes queue membership.

Each card represents one actual public Agent/session/task surface. Kindle does not invent empty provider slots or fabricated tasks.

## 4. Capacity and fill behavior

Landscape:

- 1 candidate → one expanded card;
- 2 candidates → two equal cards;
- 3 candidates → three equal cards;
- more than 3 → choose three according to priority and deterministic rotation.

Portrait:

- 1 candidate → one expanded card;
- 2 candidates → two cards;
- more than 2 → choose two according to priority and deterministic rotation.

Older COMPLETE tasks remain valid candidates. They may fill unused capacity when active pressure does not consume it. Time changes ranking; it does not delete the underlying Agent outcome from presentation eligibility.

## 5. Presentation tiers

Kindle derives presentation only from sanitized PublicState lifecycle facts. It does not mutate lifecycle state.

Tiers are:

1. **critical** — ATTENTION, ERROR;
2. **promoted delivery** — COMPLETE whose `completedAt` is still inside the configured complete-retention duration;
3. **active** — STALE active work and WORKING;
4. **resting delivery** — older COMPLETE outside the promotion window.

The configured complete-retention duration is a foreground-promotion boundary for Kindle, not a hard disappearance boundary. The configured high-visibility duration may still produce a stronger COMPLETE visual style.

A failed outcome remains ERROR presentation even if lifecycle activity has already transitioned to idle after SessionEnd. A completed outcome remains COMPLETE presentation even after the promotion window expires.

Plain idle sessions with no completed/failed delivery state are not required to consume Kindle deck capacity.

## 6. Competition and delivery slot policy

ATTENTION and ERROR always outrank normal work.

When active WORKING/STALE work is queued and at least two non-critical deck slots remain:

- at most one slot is reserved for promoted COMPLETE delivery;
- remaining slots go to active work;
- resting COMPLETE may fill otherwise unused capacity only after active work is exhausted.

When only one non-critical slot remains under simultaneous promoted-delivery and active pressure, that slot alternates deterministically between the two queues across rotation slots. This prevents either delivery or active work from being permanently starved by stable critical pressure.

When active pressure disappears, promoted and resting COMPLETE tasks may fill every remaining deck slot.

Examples:

- 3 old COMPLETE + 0 WORKING on landscape → all three remain displayable/rotatable.
- 1 old COMPLETE + 3 WORKING → the three WORKING tasks occupy foreground capacity.
- 1 recent COMPLETE + 3 WORKING → one COMPLETE delivery slot plus two active slots.

## 7. Deterministic rotation

No JavaScript carousel is used.

For refresh interval `R > 0`:

`rotationSlot = floor(serverUnixTime / R)`

Candidate queues are stably ordered by canonical public Agent ID, then sampled as a circular slice derived from `rotationSlot`. The same PublicState, layout, and rotation slot MUST produce the same card selection. A subsequent slot advances queue selection deterministically.

Selection is fair within each eligible stable queue: active tasks rotate through active capacity; promoted COMPLETE tasks rotate through the delivery slot; critical tasks rotate if they alone exceed capacity.

The existing meta refresh performs the next SSR render. Because the refresh does not replace the URL, current `layout` and `rotate` query parameters are preserved naturally.

## 8. System bar

Kindle uses one compact top System bar. The intended frozen slot order for future M3 data is:

`CPU | MEM | SWAP | DISK | clock`

M2.3 does not fabricate metrics. When the public system source is unavailable:

`SYSTEM · NOT CONNECTED`

M3 may populate these slots later without changing this display contract.

## 9. Quota rail

Quota is a compact bottom rail. Future providers may include multiple Codex accounts and GLM; provider identity comes from actual quota data, not from the runtime used to execute an agent.

M2.3 does not implement CodexBar or fabricate quota values. When no usable public quota window exists:

`QUOTA · NOT CONNECTED`

GLM MUST NOT be modeled as Claude quota merely because Claude Code is the runtime.

## 10. Agent card content and privacy

Kindle card hierarchy is:

1. STATUS;
2. elapsed time;
3. provider label.

A safe project name may appear only if a current public contract provides a trustworthy Agent-to-project identity. M2.3 does not infer one.

Kindle MUST NOT expose:

- cwd or absolute paths;
- prompt, transcript, tool input/output, raw provider JSON, or raw Hook data;
- raw session/turn internals;
- opaque IDs as prominent card content;
- private navigation detail.

ATTENTION and ERROR receive the strongest visual treatment. COMPLETE inside high visibility receives an inverse/high-contrast treatment. WORKING is clear but calmer. Resting COMPLETE remains legible with lower emphasis.

## 11. Future COMPLETE acknowledgement/navigation

Future intended interaction:

`tap COMPLETE → safely focus corresponding Mac task → focus succeeds → acknowledge delivery → delivery loses foreground priority`

M2.3 does not implement this. `safeNavigationEnabled=false` remains authoritative. Kindle MUST NOT create fake focus links or unsafe GET actions.

## 12. Kindle compatibility target

Target: Kindle Paperwhite 1 / 5th generation, firmware 5.6.1.1.

Required characteristics:

- SSR HTML;
- basic high-contrast CSS;
- large readable status text;
- meta refresh;
- simple tables/blocks and old-WebKit-safe transforms;
- no required modern JavaScript;
- no React/Vue runtime;
- no Fetch/Promise/WebSocket requirement;
- no CSS Grid requirement;
- no Canvas or SVG animation requirement.

## 13. M2.3 boundaries

M2.3 changes presentation only. It does not install provider hooks or change Codex/Claude lifecycle normalization, Unix socket security, SourceHealth semantics, PublicState privacy projection, system collection, quota collection, Safe Navigation runtime, FloatTabs integration, or M3 behavior.
