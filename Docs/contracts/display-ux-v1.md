# DevBoard Display UX V1 Contract

> Milestone closure: M2.3.1 — Kindle Presentation Closure
> Status: frozen operational presentation contract
> Scope: presentation only. Lifecycle ingestion, reducer/state contracts, system/quota collection, Safe Navigation, FloatTabs integration, real provider hook installation, and M3 are out of scope.

## 1. Surface roles

### Kindle

`/display/kindle` is an operational status appliance, not a diagnostics page. Its visual hierarchy is:

1. compact System bar with local host/display clock;
2. one shared Agent Deck occupying the dominant visual region;
3. compact Quota rail.

There is no dedicated large DEVBOARD title/header band. A negligible product marker is permitted, but branding must not compete with operational status.

Kindle MUST NOT expose Hook source diagnostics, Projects, cwd or absolute paths, prompt/transcript/tool payloads, raw provider JSON, session/turn IDs as card content, focus locators, or private navigation details.

### Desktop/mobile display

`/display` remains the richer diagnostic surface. Its primary hierarchy is Agent → System → Quota and it may additionally expose public alerts, Hook source health, and public project diagnostics.

## 2. Orientation query contract

Supported forms include:

- `/display/kindle?layout=landscape&rotate=none`
- `/display/kindle?layout=landscape&rotate=left`
- `/display/kindle?layout=landscape&rotate=right`
- `/display/kindle?layout=portrait&rotate=none`

`layout` controls content geometry and Agent Deck capacity:

- landscape: maximum 3 Agent cards;
- portrait: maximum 2 Agent cards.

Invalid or missing `layout` falls back to `landscape`.

`rotate` controls physical display orientation only:

- `none`: normal geometry;
- `left`: counter-clockwise physical rotation;
- `right`: clockwise physical rotation.

Invalid or missing `rotate` falls back to `none`. Arbitrary query values MUST NOT be reflected into rendered classes or CSS.

## 3. Physical rotation canvas

Rotation is implemented by a dedicated viewport shell and rotation canvas, not by rotating a page that was first laid out in the unrotated viewport width.

For `left` and `right`, the rotation canvas swaps effective viewport dimensions before transform (`width` from viewport height and `height` from viewport width), uses top/left translation, `transform-origin: 0 0`, clipping, and old-WebKit-compatible `-webkit-transform` plus standard `transform`.

This ensures a Kindle browser that remains in portrait browser geometry can be physically mounted sideways while landscape content receives the full effective wide canvas.

No JavaScript viewport measurement is required.

## 4. Shared Agent Deck and accepted selection semantics

Codex and Claude Code share one provider-agnostic Agent Deck. There are no provider-specific slots.

Accepted M2.3 selection semantics remain frozen:

1. critical: ATTENTION / ERROR;
2. promoted delivery: recent COMPLETE within configured retention/promotion duration;
3. active: STALE / WORKING;
4. resting delivery: older COMPLETE.

Older COMPLETE does not disappear merely because time elapsed. It yields under active queue pressure and becomes eligible again when capacity is available. Plain idle work need not consume deck capacity.

When active work exists and at least two non-critical slots remain, at most one slot is reserved for promoted COMPLETE delivery and remaining slots go to active work. Under stable critical pressure with one remaining slot, delivery and active queues alternate deterministically so neither starves.

No M2.3.1 change may casually rewrite this selection algorithm.

## 5. Deterministic SSR rotation

There is no JavaScript carousel.

For refresh interval `R > 0`:

`rotationSlot = floor(serverUnixTime / R)`

Stable Agent queues are ordered by canonical public Agent ID and sampled deterministically from the rotation slot. The same PublicState, layout, and slot produce the same deck selection.

Meta refresh requests the next SSR render while retaining the current query URL.

## 6. Agent Deck screen ownership

The Agent Deck is the primary Kindle region and targets roughly 55–65% of the usable visual canvas. The System bar is small/menu-bar-like; the Quota rail occupies the remaining compact bottom region.

Landscape:

- 1 Agent → one large full-deck-width card;
- 2 Agents → two equal large cards;
- 3 Agents → three equal large cards.

Portrait:

- 1 Agent → one large card;
- 2 Agents → two stacked large cards.

No artificial empty card is rendered. Card height is driven by the dedicated Agent Deck region rather than content length, avoiding large unused blank areas merely because status text is short.

Primary card text hierarchy is STATUS → elapsed → provider. Opaque IDs are not rendered.

## 7. Monochrome visual states

Meaning must not depend on color.

- ATTENTION: strongest border/emphasis;
- ERROR: strongest border/emphasis;
- COMPLETE high: inverse black/white treatment;
- COMPLETE promoted/recent: strong border;
- COMPLETE resting: lower emphasis but still readable;
- WORKING: strong normal readable card;
- STALE: visibly distinct border treatment from WORKING.

## 8. Request time and local display clock

Public state remains UTC-authoritative. `/api/state` lifecycle/public projection timestamps are not redefined as local time.

Each HTTP request takes one logical clock snapshot. Kindle uses that same instant in two forms:

- UTC form for PublicState projection;
- original host/local location for human display clock and quota reset countdown calculations.

The Kindle clock MUST NOT be derived by first calling `UTC()` on the presentation time.

## 9. System bar

The System bar is the top operational strip. No separate large SYSTEM section exists on Kindle.

When usable system source data exists, the frozen compact shape is:

`CPU <value> | MEM <used/total> | SWAP <used/total> | DISK <percent> | HH:MM`

Example shape:

`CPU 24% | MEM 14/24G | SWAP 1/4G | DISK 61% | 08:43`

Missing individual metrics render as unavailable values rather than fabricated measurements.

When the system source is unavailable:

`SYSTEM · NOT CONNECTED | HH:MM`

The local clock remains visible in both connected and unavailable states. System collection itself remains out of scope for M2.3.1; M3 may populate this frozen presentation later.

## 10. Quota semantics: remaining, not used

M2.3.1 does not implement quota collection. It consumes existing sanitized `PublicQuota` only.

`PublicQuotaWindow.UsedPercent` means USED. Kindle converts valid input `U` to operational remaining percentage:

`remaining = clamp(100 - U, 0, 100)`

The percentage label is explicit, for example:

`72% LEFT`

Kindle MUST NOT show an ambiguous bare percentage as the primary quota meaning.

A quota window with nil `UsedPercent` is not usable and does not by itself establish quota connectivity.

## 11. Fixed-segment quota bar

Quota uses a deterministic 16-segment text rail generated in the Go ViewModel for predictable old-Kindle rendering.

Example:

`[############----] 72% LEFT`

Filled segments represent remaining quota. Segment count is rounded from remaining percentage and clamped to 0–16. The rail does not require SVG, Canvas, JavaScript, CSS Grid, or modern layout APIs.

## 12. Multi-window quota presentation

One provider/account may expose multiple current public windows. Every usable window is rendered independently, for example:

`CODEX A  5H    [############----] 72% LEFT · reset 2h18m`

`CODEX A  WEEK  [#######---------] 43% LEFT · reset 3d07h`

No assumption is made that a provider has only one window.

Provider strings are rendered from public data and are not remapped. GLM is not modeled as Claude merely because Claude Code may be the runtime.

The current `PublicQuota` contract does not provide a separate account identity beyond its existing provider/domain data. If a future quota source must distinguish multiple Codex accounts that cannot be distinguished by current public identity, that is a future quota collector/domain-contract concern. M2.3.1 does not change PublicState schema or invent account names.

## 13. Quota reset countdown

When `ResetsAt` exists and is in the future, Kindle renders compact relative time using the same request instant, such as:

- `reset 2h18m`
- `reset 3d07h`

When `ResetsAt` is nil, reset text is omitted. When reset time is already due or past, Kindle renders the deterministic safe form:

`reset due`

Raw timestamps are not required on Kindle.

## 14. Partial quota availability

Quota is connected when at least one usable public quota window exists.

If one provider has usable windows while another provider/source is unavailable, Kindle renders the usable rows and does not mark the entire Quota rail disconnected.

If no usable public quota window exists:

`QUOTA · NOT CONNECTED`

No quota values are fabricated.

## 15. Privacy and diagnostics boundary

Kindle MUST NOT render:

- HOOK SOURCES or SourceHealth messages;
- Projects section;
- cwd or absolute paths;
- raw session/turn IDs;
- raw provider JSON;
- prompt/transcript/tool input/tool output;
- focus locator or private navigation detail.

`/display` may continue rendering richer sanitized diagnostics.

`safeNavigationEnabled=false` remains authoritative. M2.3.1 adds no focus links or navigation runtime.

## 16. Kindle compatibility target

Target remains Kindle Paperwhite 1 / 5th generation, firmware 5.6.1.1.

Required:

- SSR HTML;
- meta refresh;
- simple tables/blocks and absolute positioning;
- high-contrast monochrome presentation;
- old-WebKit `-webkit-transform` fallback;
- simple rotation geometry without JavaScript viewport measurement.

Must not require:

- `<script>`;
- Fetch / Promise;
- WebSocket / EventSource;
- React / Vue;
- CSS Grid;
- Canvas / SVG animation;
- ResizeObserver / IntersectionObserver.

## 17. M2.3.1 boundaries

M2.3.1 is presentation closure only. It does not implement or modify:

- `internal/agent/*` lifecycle normalization/reducer/runtime;
- `internal/state/*` or PublicState schema;
- real provider hook installation;
- System collection;
- CodexBar or other quota collection;
- multiple-account domain identity beyond current public data;
- Safe Navigation;
- FloatTabs integration;
- M3 behavior.

## 18. Bounded task-context presentation amendment

The Kindle card may use otherwise-unused card space for one additional,
bounded context line when the existing public projection already contains a
safe checkpoint:

- `READY` may show `LAST PROGRESS` after its actionable attention text;
- `COMPLETE` may show `LAST CHECKPOINT` after its completion summary.

This is a presentation-only derivative of `PublicTask.Checkpoint`. It does
not add raw prompt, transcript, assistant reply, tool input/output, result
identifier, or diagnostic text to PublicState, and it is omitted when no safe
checkpoint exists. `WORKING` continues to use its single latest checkpoint as
the primary state-specific detail.
