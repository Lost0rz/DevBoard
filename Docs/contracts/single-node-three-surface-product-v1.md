# DevBoard SINGLE_NODE_THREE_SURFACE_PRODUCT_V1

> Date: 2026-08-24 (Asia/Shanghai)
> Status: **FROZEN FOR CURRENT SINGLE-MAC PRODUCT MILESTONE**
> Scope: one physical Mac, one NAS Hub, one landscape iPad Display
> Audit branch at freeze: `codex/m6-browser-ai-watch-reference-audit`
> Audit HEAD at freeze: `571269d`

```text
SINGLE_NODE_THREE_SURFACE_PRODUCT_V1 = FROZEN
PRODUCT_TOPOLOGY = MAC_A -> NAS_HUB -> IPAD_DISPLAY
```

This contract freezes the current single-Mac product delivery and acceptance
slice. The user has explicitly accepted `SINGLE_NODE_THREE_SURFACE_PRODUCT_V1`.
It does not delete, edit, or rewrite any historical contract.

## 0. Current milestone authority

For this single-node product milestone, this contract is the controlling
delivery and acceptance authority over older requirements for:

- dual Mac / Mac B;
- Browser Watch implementation;
- remote control; and
- public Internet deployment.

Those four areas are deferred by this contract and are not current release
blockers. Older multi-host or Browser Watch contracts remain in the repository
as historical/future-scope records; they are not silently deleted or rewritten.

The narrower product authority above does **not** weaken the lower-level Node ->
Hub or security semantics inherited from the older contracts. These remain
effective and testable:

- authenticated outbound Node -> Hub push, Node identity binding, admission,
  ordering, retry, and Hub freshness/last-good semantics;
- privacy projection and redaction before transport or display;
- fail-closed behavior for authentication, authorization, malformed input,
  credential, privacy, configuration, and persistence failures; and
- fail-open behavior for monitoring-only Hook/Agent/Quota collection failure:
  the monitored workflow is not blocked, while the affected source is exposed
  as unavailable, degraded, or stale and never reported as healthy.

LaunchAgent/Docker remains the restart authority. The menu-bar App and iPad
Display are consumers of product/runtime status, not alternate state or
supervision authorities.

## 1. Product boundary

The only product path in this contract is:

```text
Mac A
  local collectors + Agent/Hook/Quota integrations
  per-user DevBoard Node supervised by LaunchAgent
       | authenticated outbound Node -> Hub snapshot
       v
NAS Hub
  persistent Hub configuration and Node registry
  health endpoint and Operator Console
  /display read surface
       | trusted-LAN GET /display, 2 second refresh
       v
iPad Display
  landscape, glanceable, read-only monitoring surface
```

The contract is intentionally strict:

- Mac A is the only physical producer in the V1 acceptance path.
- The NAS Hub is the only cross-machine aggregation authority.
- The iPad is the only named display device in this product slice.
- Cross-machine transport is authenticated outbound Mac A Node -> NAS Hub push.
- The iPad reads the NAS Hub; it does not contact Mac A directly.
- The display is read-only. No credential, pairing, or control action is part
  of the iPad surface.

Existing multi-node-capable data structures may remain in the repository for
future work and regression coverage, but they are not V1 delivery, not V1
acceptance, and not a reason to add a second physical machine to this baseline.

## 2. Explicitly deferred

The following are outside this frozen product slice and must not block the
pre-product baseline:

1. Dual Mac / Mac B / any second physical Mac implementation or acceptance.
2. Browser Watch implementation, browser automation, or browser conversation
   ingestion. The display may show an explicit not-connected/deferred state;
   it must not claim Browser Watch coverage.
3. Remote control: approve, deny, answer, stop, retry, continue, prompt,
   shell execution, navigation, or any other remote action.
4. Public Internet deployment, public-user authentication, and direct public
   exposure of the Hub port.

Trusted-LAN dogfood access is the only network assumption for this contract.
The raw HTTP Hub port is not an Internet-safe deployment surface.

## 3. Frozen delivery outputs

### 3.1 Mac product

The Mac delivery must provide:

- `DevBoard.dmg`;
- `DevBoard.app` inside the DMG;
- a menu-bar status surface for local Node/Hub health;
- a per-user `com.devboard.node` LaunchAgent with `RunAtLoad` and `KeepAlive`;
- product-managed Hook configuration for the supported local providers;
- product-managed read-only Quota configuration/identity references;
- normal installation, repair, status, and onboarding without Terminal or
  hand-editing YAML.

The first Mac package is explicitly:

```text
INTERNAL DOGFOOD DMG
```

It is not a public release. The current environment has no Developer ID
credential. Formal distribution is blocked until all of these gates pass:

```text
Developer ID application signing
  + Hardened Runtime
  + notarization
  + staple
```

The existing `scripts/build-macos-app.sh` is evidence of a build foundation,
not proof of this frozen output: it currently performs ad-hoc signing and
writes `dist/DevBoard-macos-universal.zip`, not `DevBoard.dmg`.

#### 3.1.1 Menu-bar status and lifecycle contract

The menu-bar App is a compact status/control surface. Its status menu must
show all five product signals, with text labels and no color-only meaning:

| Signal | Minimum status meaning |
| --- | --- |
| Node | running/healthy, not running, or unhealthy |
| Hub | connected, disconnected, or not configured |
| Codex | configured/healthy, attention, unavailable, or not configured |
| Claude Code | configured/healthy, attention, unavailable, or not configured |
| Quota | available, stale/degraded, unavailable, or not configured |

The menu must provide these actions:

- `Open Display`;
- `Open Local Settings`;
- `Open Hub Admin`;
- `Install/Repair`; and
- `Restart`.

`Install/Repair` and `Restart` act on the LaunchAgent-owned Node through the
existing product service authority. Closing the menu-bar window, hiding the
menu, logging out of the menu-bar App, or quitting the menu-bar App must not
stop or uninstall the LaunchAgent-owned Node. The App must report the Node as
independent background service state rather than infer it from its own process
state.

Startup is split into two independent contracts:

1. **Menu-bar App login startup:** the user-session login item starts the
   status App, if enabled, so the menu and actions are available. Its failure
   must not be treated as Node failure.
2. **Background Node startup:** the per-user LaunchAgent starts the Node with
   `RunAtLoad=true` and `KeepAlive=true`, without requiring the menu-bar App to
   be running. The Node must continue to run when the App is not logged in,
   is quit, or is temporarily unavailable.

#### 3.1.2 Single-Mac Quota product flow

The normal Mac A installation flow is product/UI-led and does not require
Terminal, hand-written HMAC `accountKey` values, YAML edits, or manual alias
file edits:

1. If Mac A has no quota identity key, the product generates a cryptographically
   random key of at least 32 bytes.
2. The product stores that key only inside its private product directory, with
   the key file mode `0600` and private parent-directory permissions. The key
   is never printed, logged, rendered in a result page, uploaded, or placed in
   a diagnostic artifact.
3. The UI/product onboarding invokes bounded, read-only CodexBar account
   detection. It does not mutate CodexBar, provider credentials, or account
   configuration.
4. The user chooses only from the fixed allow-listed public labels `Codex A`,
   `Codex B`, and `GLM`. The UI never asks the user to type an HMAC-derived
   account identifier.
5. The product derives/loads the internal sanitized account identity and
   persists the selected label through product-managed storage. It must not
   require editing YAML or an alias file.
6. Node collection and Hub projection carry only the sanitized allow-listed
   label, irreversible local account key, windows, sampled time, source status,
   and observing Node context required by the existing public-state contract.

The following must never enter the Hub, Display, logs, or result pages:

- email address;
- provider account ID;
- Token;
- Cookie;
- OAuth material;
- API Key; or
- raw CodexBar JSON/command output.

Mac B shared-key export/import is explicitly deferred and is not a single-Mac
V1 acceptance or installation blocker. A single Mac may use its own generated
private identity key and still complete the V1 Quota flow.

### 3.2 NAS Hub product

The NAS delivery must provide one source-free, self-contained `linux/amd64`
bundle. Normal NAS installation must not require a source checkout, Go, a
local image build, or registry access.

The NAS product includes an Operator Console with these named areas:

- Overview;
- Nodes;
- Settings;
- Logs.

These four areas remain frozen as separate operator responsibilities:

- **Overview** is the bounded current product/Hub health summary.
- **Nodes** manages the registered Mac A identity and enable/disable/reset
  workflow without rendering raw credentials.
- **Settings** exposes only application-level settings that are safe,
  schema-validated, and durably persisted before success is shown.
- **Logs** exposes only bounded, redacted application diagnostics.

The Logs area must not read arbitrary filesystem paths, expose raw snapshots,
prompts, tokens, credentials, private paths, or unbounded command output. It
must not mount or access the Docker socket. It may show only an allow-listed,
bounded diagnostic projection whose content has already passed product
redaction and whose failure does not expose the underlying raw source.

Docker host port, UID/GID, TLS/reverse-proxy termination, Docker log driver,
log rotation, volume location, and other host/container wiring remain managed
by the source-free bundle, `.env`, and Compose. They must not be presented as
container-internal Web settings that are safe to change from the Operator
Console. Operator Settings may change only application-level, safe,
verifiable, persistable values and must preserve the existing atomic
configuration/restart semantics.

It also includes:

- persistent Hub configuration, admin credential, Node registry, and operator
  settings;
- a native Hub health check suitable for Compose supervision;
- bounded container log rotation;
- an idempotent install/upgrade path that preserves persistent data;
- `/display` as the read-only display endpoint.

Live Node snapshots may be repopulated from Mac A after a Hub restart; that is
not a claim that volatile live snapshots are durable database state.

#### 3.2.1 Artifact authority

The working tree may contain these pre-existing `dist/` artifacts:

- `dist/DevBoard-macos-universal.zip`;
- `dist/DevBoard-Hub-linux-amd64.tar.gz`.

They predate the current dirty-worktree fixes and are stale/non-authoritative
artifacts. Their presence does not satisfy this contract, does not establish a
release, and must not be used as current acceptance evidence. The new
`DevBoard.dmg` and the latest contract-compliant NAS bundle have not been
generated in this audit.

### 3.3 iPad Display

The display delivery is:

- NAS `/display`;
- landscape Pad/iPad layout;
- 2-second bounded refresh;
- Agent/task status;
- Host health/status;
- Quota status;
- explicit offline and stale semantics.

The display must distinguish these states in text and not rely on color alone:

| Condition | Required meaning |
| --- | --- |
| Hub reachable and Mac A snapshot inside the online window | `HUB ONLINE`, Mac A `ONLINE`, current data |
| Hub reachable and retained Mac A snapshot outside the online window | `STALE` / `DATA STALE`; last-good values are visibly non-current |
| No retained snapshot or retention expired | `OFFLINE`; no fabricated live metrics |
| Display refresh request fails | refresh/degraded state is visible; the last rendered fragment must not be relabeled current |
| Quota source is absent or unavailable | explicit `NOT CONNECTED`/degraded state; no invented reset or percentage |

Agent primary states remain `READY`, `WORKING`, and `COMPLETE`; stale is a
modifier, never a fourth task state. The Pad is not an Operator Console and
must not expose edit, reset, pairing, token, restart, or remote-control actions.

## 4. State and security invariants

The pre-product baseline retains these invariants:

- Node owns local collectors, local Agent/Hook ingestion, local state,
  PublicState projection, and outbound uplink.
- Hub owns registry, authentication, snapshot admission, Hub-clock freshness,
  last-good retention, dashboard projection, and `/display` reads.
- Node identity is configured Node ID plus credential, never a LAN IP.
- Public state excludes raw tokens, account email/identity, cookies, API keys,
  private paths, raw prompts/transcripts, shell commands, and arbitrary provider
  payloads.
- Mac app and iPad Display do not become a second state authority.
- No change in this contract authorizes touching a real Mac, NAS, Hook,
  LaunchAgent, Keychain, CodexBar, or account configuration during this audit.

## 5. Baseline inventory scope

Inventory was captured before this file was added on the audit branch:

- 27 tracked modified files;
- 98 pre-existing untracked files;
- 83 of the untracked files are `.playwright-cli/` visual/console artifacts.

This contract is the only file added by this audit. After adding it, the dirty
tree is expected to contain 27 tracked modified files and 99 untracked files,
including this contract. No dirty file was cleaned, discarded, staged,
committed, pushed, merged, packaged, or deployed by this audit.

The following is the primary baseline ownership map. Test files and visual
artifacts are listed separately so the functional product lanes can be split
without silently dropping evidence.

### 5.1 Shared contracts/state

| Status | Path | Baseline role |
| --- | --- | --- |
| new in this audit | `Docs/contracts/single-node-three-surface-product-v1.md` | this frozen product boundary, inventory, conflict audit |
| untracked | `Docs/contracts/multi-host-pad-closure-v1.md` | multi-host Pad closure evidence; broader than this V1 |
| untracked | `Docs/contracts/pad-display-v1.md` | landscape Pad information architecture and stale semantics |
| modified | `README.md` | repository-level product/runtime status and operator guidance |
| modified | `internal/config/config.go` | shared runtime, Node registry, display, and quota config schema |
| modified | `internal/config/persist.go` | atomic persistent config rendering |
| modified | `internal/dashboard/model.go` | Hub dashboard/read-model host and freshness wrappers |
| modified | `internal/state/model.go` | PublicState and quota model changes |
| modified | `internal/state/projector.go` | internal-to-public privacy projection |
| modified | `internal/state/public.go` | public source/quota/state shapes |
| modified | `internal/state/store.go` | local state store mutation/retention authority |

### 5.2 Mac product

| Status | Path | Baseline role |
| --- | --- | --- |
| modified | `cmd/devboard/main.go` | Node runtime wiring, Hook/Quota startup, product command dispatch |
| modified | `cmd/devboard/product.go` | product service, integration, and onboarding command surface |
| modified | `internal/product/integrations.go` | provider Hook install/status/remove behavior |
| modified | `internal/product/service.go` | per-user service/LaunchAgent install, status, restart, uninstall |
| untracked | `internal/product/onboarding.go` | repeatable Mac-to-Hub onboarding workflow |
| untracked | `internal/product/setup.go` | post-install product setup orchestration |
| modified | `macos/DevBoardApp/DevBoardApp/ContentView.swift` | SwiftUI product shell UI |

### 5.3 NAS product

| Status | Path | Baseline role |
| --- | --- | --- |
| modified | `internal/hub/store.go` | Hub accepted-state store, freshness, retention, quota dedupe |
| modified | `internal/web/admin.go` | authenticated Hub Admin and Node registry mutations |

The repository already has source-free NAS delivery assets under `deploy/hub/`
and a `linux/amd64` bundle builder, but no dirty bundle artifact is present in
the inventory. The Operator Console target is broader than the currently
observed `/display`, `/admin`, and Node-local `/settings` routes; in particular,
an explicit Logs area is not observed.

### 5.4 Display

| Status | Path | Baseline role |
| --- | --- | --- |
| modified | `internal/web/network.go` | Dashboard/Pad view-model projection and freshness rendering |
| modified | `internal/web/static/app.css` | desktop/Pad/operator presentation styles |
| modified | `internal/web/static/dashboard.js` | bounded `/display/fragment` refresh and refresh-state marker |
| modified | `internal/web/templates/dashboard_fragment.html` | Agent, Host, Quota, and Web signal fragment |
| modified | `internal/web/templates/display.html` | NAS `/display` shell and refresh container |
| untracked | `Docs/contracts/pad-display-v1.md` | Display contract evidence |
| untracked | `Docs/contracts/multi-host-pad-closure-v1.md` | broader multi-host display closure evidence |

The two contract paths above are also listed in Shared contracts/state because
they are governance artifacts; they are not duplicate dirty files.

### 5.5 Quota/onboarding

| Status | Path | Baseline role |
| --- | --- | --- |
| modified | `Docs/M5_5A_DOGFOOD_ONBOARDING_2026-08-23.md` | prior onboarding/dogfood evidence and operator flow |
| untracked | `Docs/MACOS_POST_INSTALL_WORKFLOW_2026-08-23.md` | post-install, Hook, LaunchAgent, and Quota workflow evidence |
| untracked | `internal/quota/collector.go` | bounded read-only CodexBar quota adapter and sanitized projection |

The implementation files `internal/product/onboarding.go` and
`internal/product/setup.go` are assigned to Mac product above because they are
the product entrypoints; their Quota phases are part of this cross-cutting
lane.

### 5.6 Test artifacts

#### Modified tracked tests

- `cmd/devboard/product_test.go`
- `internal/product/integrations_test.go`
- `internal/product/service_test.go`
- `internal/uplink/m54_hub_e2e_test.go`
- `internal/web/admin_test.go`
- `internal/web/product_ui_test.go`

#### Untracked test sources

- `internal/hub/multi_host_closure_test.go`
- `internal/product/onboarding_probe_test.go`
- `internal/product/onboarding_test.go`
- `internal/product/setup_test.go`
- `internal/quota/alias_file_test.go`
- `internal/quota/alias_governance_test.go`
- `internal/quota/collector_test.go`
- `internal/state/quota_store_test.go`
- `internal/web/pad_display_test.go`

#### Untracked `.playwright-cli/` local temporary workdir

`.playwright-cli/` is a local temporary work directory, not a product
baseline and not a directory that enters the product or logical commit as a
whole. The local files are intentionally left in place during this audit.

The only future canonical visual evidence may be:

- one final PNG for each accepted target size: `1024x640`, `1280x800`, and
  `2560x1600`;
- one `geometry-report.json`; and
- a necessary bounded, redacted console/acceptance summary.

Raw captures, intermediate crops, repeated `v1`/`v2`/`final` variants,
temporary harnesses, and debugging logs must not enter a logical commit. No
raw image, raw console output, private path, credential, token, prompt, or
unbounded diagnostic text is canonical evidence. The 83 paths below are the
currently observed local files and remain unmodified by this audit:

```text
.playwright-cli/console-2026-08-23T14-24-45-973Z.log
.playwright-cli/harness/main.go
.playwright-cli/pad-audit/1280x800-empty.png
.playwright-cli/pad-audit/1280x800-one.png
.playwright-cli/pad-audit/1280x800-three.png
.playwright-cli/pad-blocker/1024x640-three-audit.raw
.playwright-cli/pad-blocker/1024x640-three-final.raw
.playwright-cli/pad-blocker/1024x640-three-v2.raw
.playwright-cli/pad-blocker/1024x640-three.png
.playwright-cli/pad-blocker/1024x640-three.raw
.playwright-cli/pad-blocker/1280x800-empty-audit.raw
.playwright-cli/pad-blocker/1280x800-empty-final.raw
.playwright-cli/pad-blocker/1280x800-empty-v2.raw
.playwright-cli/pad-blocker/1280x800-empty.png
.playwright-cli/pad-blocker/1280x800-empty.raw
.playwright-cli/pad-blocker/1280x800-offline-audit.raw
.playwright-cli/pad-blocker/1280x800-offline-final.raw
.playwright-cli/pad-blocker/1280x800-offline-v2.raw
.playwright-cli/pad-blocker/1280x800-offline.png
.playwright-cli/pad-blocker/1280x800-offline.raw
.playwright-cli/pad-blocker/1280x800-one-audit.raw
.playwright-cli/pad-blocker/1280x800-one-final.raw
.playwright-cli/pad-blocker/1280x800-one-v2.raw
.playwright-cli/pad-blocker/1280x800-one.png
.playwright-cli/pad-blocker/1280x800-one.raw
.playwright-cli/pad-blocker/1280x800-stale-audit.raw
.playwright-cli/pad-blocker/1280x800-stale-audit2.raw
.playwright-cli/pad-blocker/1280x800-stale-final.raw
.playwright-cli/pad-blocker/1280x800-stale.png
.playwright-cli/pad-blocker/1280x800-three-audit.raw
.playwright-cli/pad-blocker/1280x800-three-final.raw
.playwright-cli/pad-blocker/1280x800-three-v2.raw
.playwright-cli/pad-blocker/1280x800-three.png
.playwright-cli/pad-blocker/1280x800-three.raw
.playwright-cli/pad-blocker/2560-clip1280.png
.playwright-cli/pad-blocker/2560-clip1280.raw
.playwright-cli/pad-blocker/2560x1600-one-audit.raw
.playwright-cli/pad-blocker/2560x1600-one-exact.raw
.playwright-cli/pad-blocker/2560x1600-one-final.raw
.playwright-cli/pad-blocker/2560x1600-one-native-clip.png
.playwright-cli/pad-blocker/2560x1600-one-native-clip.raw
.playwright-cli/pad-blocker/2560x1600-one-v2.raw
.playwright-cli/pad-blocker/2560x1600-one.png
.playwright-cli/pad-blocker/2560x1600-one.raw
.playwright-cli/pad-blocker/2560x1600-three-audit.raw
.playwright-cli/pad-blocker/2560x1600-three-clip2550.png
.playwright-cli/pad-blocker/2560x1600-three-clip2550.raw
.playwright-cli/pad-blocker/2560x1600-three-exact.raw
.playwright-cli/pad-blocker/2560x1600-three-final.raw
.playwright-cli/pad-blocker/2560x1600-three-formatpng.raw
.playwright-cli/pad-blocker/2560x1600-three-native-clip.png
.playwright-cli/pad-blocker/2560x1600-three-native-clip.raw
.playwright-cli/pad-blocker/2560x1600-three-noclip.png
.playwright-cli/pad-blocker/2560x1600-three-noclip.raw
.playwright-cli/pad-blocker/2560x1600-three-v2.raw
.playwright-cli/pad-blocker/2560x1600-three.png
.playwright-cli/pad-blocker/2560x1600-three.raw
.playwright-cli/pad-blocker/crop-tl-ffmpeg.png
.playwright-cli/pad-blocker/crop-tl.png
.playwright-cli/pad-blocker/crop-tr-ffmpeg.png
.playwright-cli/pad-blocker/current-1280.png
.playwright-cli/pad-blocker/geometry-report.json
.playwright-cli/pad-closure/1024x640-empty-final.png
.playwright-cli/pad-closure/1024x640-three-final.png
.playwright-cli/pad-closure/1024x640-three.png
.playwright-cli/pad-closure/1280x800-empty-final.png
.playwright-cli/pad-closure/1280x800-empty.png
.playwright-cli/pad-closure/1280x800-one-final.png
.playwright-cli/pad-closure/1280x800-one.png
.playwright-cli/pad-closure/1280x800-three-final.png
.playwright-cli/pad-closure/1280x800-three.png
.playwright-cli/pad-closure/2560x1600-one-final.png
.playwright-cli/pad-closure/2560x1600-one.png
.playwright-cli/pad-closure/2560x1600-three-final.png
.playwright-cli/pad-closure/2560x1600-three.png
.playwright-cli/pad-v1/1024x640.png
.playwright-cli/pad-v1/1280x800.png
.playwright-cli/pad-v1/2560x1600.png
.playwright-cli/pad-v2/1280-empty.png
.playwright-cli/pad-v2/1280-one.png
.playwright-cli/pad-v2/1280-three.png
.playwright-cli/pad-v2/2560-one.png
.playwright-cli/pad-v2/2560-three.png
```

## 6. Final authority mapping against existing contracts

This table records historical scope relationships and the authority boundary
for the current single-node milestone. No existing contract was edited. The
older multi-host and Browser Watch scopes remain future/deferred records and
are not current release blockers.

| Existing contract | Finding | Treatment |
| --- | --- | --- |
| `Docs/contracts/m5-2-node-hub-ingestion-v1.md` | Core Node -> Hub push, Hub authority, privacy, and no remote control align. Its scope explicitly excludes DMG UI, final Dashboard redesign, Browser Watch, quota-account dedupe, and public-user product, while this contract freezes selected product outputs. | This contract controls the current product packaging/acceptance slice; M5.2 transport, admission, freshness, privacy, and failure semantics remain authoritative. |
| `Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md` | Single-node-first real acceptance aligns, while its multi-node-preservation rule is an architectural/future-capability rule. | This contract controls current Mac A delivery/acceptance. Generic multi-node capability remains permitted and is not removed; Mac B is not a V1 blocker. |
| `Docs/contracts/m5-5a-dogfood-deployment-v1.md` | The historical contract includes Mac B acceptance and lists native app, signing/notarization/final DMG, and final Display redesign as deferred. | For this milestone, this contract covers those four deferred delivery/acceptance families. The historical contract remains unchanged. |
| `Docs/contracts/m5-multi-host-v1.md` | Requires a two-Mac multi-host MVP and real two-Mac acceptance. | Historical/future scope only. Mac B is deferred by this contract and is not a current release blocker. |
| `Docs/contracts/mvp-feature-freeze-v1.md` | Its Monitoring MVP requires at least two hosts and includes Browser AI Watch. | Historical/future scope only. Dual Mac and Browser Watch are deferred by this contract and are not current release blockers. |
| `Docs/contracts/pad-display-v1.md` | `/display`, landscape Pad, single-Mac-first acceptance, Agent/Host/Quota areas, and stale modifier semantics align. Its Web Notifications/Browser Watch signal is broader than this contract. | Use its layout/freshness rules; Browser Watch remains an explicit not-connected/deferred state in V1. |
| `Docs/contracts/multi-host-pad-closure-v1.md` | Defines Mac A + Mac B + Mac N aggregation and global multi-host quota dedupe. | Broader future closure; not V1 acceptance or a current blocker. No rewrite. |
| `Docs/PC1_MACOS_LONG_RUNNING_CONSTRUCTION_SPEC_2026-08-23.md` | Product shell, LaunchAgent, no Terminal/YAML, and Mac B deferral align. It treats DMG/Developer ID/notarization as later gates. | This contract makes the first DMG an internal dogfood output and records the formal release gates. |
| `Docs/PC1_NAS_LONG_RUNNING_CONSTRUCTION_SPEC_2026-08-23.md` | Source-free `linux/amd64` bundle, persistence, healthcheck, and bounded log rotation align. | Operator Console named areas are frozen here as the product acceptance surface; current Logs UI gap is reported. |
| `Docs/contracts/m5-1-always-on-hub-v1.md` | Historical pull-oriented multi-host material conflicts with the current outbound-push topology. | Already superseded by M5.2 for production transport; this contract follows M5.2 and does not edit M5.1. |

## 7. Pre-product baseline steps

The recommended sequence after this audit is:

1. Review and explicitly accept this contract plus the §6 conflict list.
2. Preserve a read-only dirty-tree snapshot: status, full path inventory,
   diff statistics, and hashes for canonical evidence only. Treat the full
   `.playwright-cli/` directory as local temporary material.
3. Create a clean integration baseline branch/worktree from the reviewed
   parent; keep the current dirty audit branch unchanged until its files are
   split into logical commits.
4. Split the existing work into the logical commits in §8, with tests/evidence
   separated from functional product changes.
5. Close Mac blockers: the five menu-bar status signals and five frozen menu
   actions, independent App/LaunchAgent lifecycle, actual internal
   `DevBoard.dmg`, `DevBoard.app` embedding, generated private Quota identity
   key, read-only CodexBar label selection, onboarding without Terminal/YAML,
   and a repeatable internal dogfood install/repair check. Do not pursue
   public signing until Developer ID/Hardened Runtime/notarization/staple are
   available.
6. Close NAS blockers: exact source-free bundle verification and the named
   Overview/Nodes/Settings/Logs Operator Console, with bounded redacted Logs,
   safe persistable application Settings, persistent registry/config,
   healthcheck, and log rotation behavior. Keep host wiring in bundle/.env/
   Compose.
7. Close iPad blockers with a real landscape iPad check at the frozen 2-second
   refresh interval, including Hub failure, stale retention, offline expiry,
   quota unavailable, and no-control/privacy checks.
8. Run repository validation on the clean baseline: `git diff --check`, Go
   tests/race-sensitive tests appropriate to the changed lanes, static/privacy
   checks, macOS package self-tests, and NAS bundle manifest/checksum checks.
9. Only after the single-node path is accepted, open separate deferred work
   for Mac B, Browser Watch, remote control, or public Internet deployment.

No step above authorizes real-device/account mutation during this audit.

## 8. Logical commit and branch plan

No commit, branch mutation, staging, push, merge, package, deployment, or
dirty-file cleanup is performed in this turn. The proposed future split is:

1. `docs: freeze single-node three-surface product v1` — this file only.
2. `state: baseline single-node public state and quota contracts` — shared
   config/state/dashboard changes and their focused tests.
3. `mac: package the internal dogfood product shell` — Mac CLI/service,
   SwiftUI shell, Hook/Quota onboarding, and focused Mac tests.
4. `nas: baseline source-free Hub product and operator console` — Hub Admin,
   persistence, health, logs, bundle assets, and focused NAS tests.
5. `display: freeze iPad landscape surface` — Pad view model/templates/assets,
   2-second refresh, stale/offline behavior, and focused display tests.
6. `test: retain canonical visual and integration evidence` — only final
   target-size PNGs, `geometry-report.json`, and necessary bounded/redacted
   console or acceptance summaries, with provenance. The `.playwright-cli/`
   directory, raw captures, crops, duplicate variants, temporary harness, and
   debug logs remain local and are excluded.
7. `docs: update operator/readme evidence` — README and onboarding runbooks
   after the functional commits have been independently reviewed.

Suggested branch shape, to be created only after a clean baseline is approved:

```text
codex/pre-product-single-node-v1       integration / acceptance branch
├─ codex/pre-product-mac               Mac product lane
├─ codex/pre-product-nas               NAS product lane
├─ codex/pre-product-display            iPad Display lane
└─ codex/pre-product-evidence           tests and visual evidence lane
```

The current `codex/m6-browser-ai-watch-reference-audit` remains the preserved
audit input until the owner decides how to split its dirty changes. Deferred
work must not be merged into the V1 integration branch:

```text
deferred: dual-Mac
deferred: Browser Watch
deferred: remote control
deferred: public Internet deployment
```

## 9. Blocking items

The pre-product baseline is **not yet distribution-ready**. Current blockers
identified without touching real machines are:

- no Developer ID credential; therefore no formal signed/notarized/stapled
  distribution;
- no `DevBoard.dmg` or `DevBoard.app` delivery artifact in the dirty inventory;
- the observed macOS build script emits an ad-hoc-signed ZIP and does not
  produce the frozen DMG;
- no menu-bar status implementation was found in the current SwiftUI app;
- `dist/DevBoard-macos-universal.zip` and
  `dist/DevBoard-Hub-linux-amd64.tar.gz` may exist, but they are stale,
  non-authoritative artifacts from before the current dirty fixes;
- the NAS source-free bundle builder/assets exist, but the latest
  contract-compliant NAS bundle has not been generated or verified in this
  audit;
- current web routes show `/display`, `/admin`, and Node-local `/settings`, but
  no distinct Overview/Nodes/Settings/Logs Operator Console with a Logs area;
- no real iPad acceptance was performed in this audit;
- the historical multi-host and Browser Watch contracts remain broader future
  scope, but their difference is explicitly governed here and is not a current
  release blocker.

These are reports and gates, not implementation work performed here.

## 10. Audit commands and result

Commands executed in this audit included:

```text
git status --short --branch
git diff --name-status
git diff --stat
git diff --numstat
git ls-files --others --exclude-standard
git diff --check
go test ./...
```

`git diff --check` passed before and after adding this contract. No whitespace
errors were reported. The dirty inventory above remains intentionally intact;
the current validation result is reported by the audit response for this
revision.

**SINGLE_NODE_THREE_SURFACE_PRODUCT_V1 FROZEN FOR CURRENT SINGLE-MAC PRODUCT MILESTONE.**
