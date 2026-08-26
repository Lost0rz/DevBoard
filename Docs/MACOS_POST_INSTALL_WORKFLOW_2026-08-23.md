# macOS Post-Install Workflow

> Status: product workflow baseline for repeatable Mac onboarding, repair, and upgrade.

This workflow replaces per-machine diagnostic setup with one idempotent product
operation. The packaged `DevBoard.app` embeds the same helper that becomes the
long-running Node binary.

## Authoritative command

From the embedded helper or an already installed product helper:

```bash
devboard product setup
```

The command performs these phases in order:

1. install or repair the stable helper at
   `~/Library/Application Support/DevBoard/bin/devboard`;
2. preserve an existing private `node.yaml`, or create the default once;
3. install, bootstrap, and verify the per-user `com.devboard.node`
   LaunchAgent;
4. remove exact known legacy DevBoard provider handlers that point to
   `~/.local/bin/devboard`;
5. install or repair the stable Codex and Claude Code handler definitions;
6. return structured per-phase results and the remaining user action.

The operation is safe to repeat for first install, repair, and upgrade. It
does not delete unrelated provider configuration and it does not replace an
existing Node configuration.

## Hook-source preflight

First-run and repair must audit the complete Codex hook set before asking the
user to run a test task. Codex merges matching definitions from user, project,
and enabled-plugin sources, so repeated failures for one lifecycle event can
come from different commands and must not be attributed to DevBoard from the
event name alone.

The deterministic preflight is:

1. enumerate every active hook source and handler for the events used by the
   installed providers;
2. resolve the handler executable in the same non-interactive environment used
   by Codex;
3. classify exit code `127`, or an absent absolute executable path, as a stale
   command definition;
4. verify the stable DevBoard command independently with representative JSON
   input and require exit code `0`;
5. back up provider configuration before any cleanup;
6. remove DevBoard-owned legacy handlers automatically, but preserve unrelated
   handlers unless the user explicitly approves orphan cleanup;
7. after the one-time trust review, run a real task and verify source telemetry.

The packaged first-run UI should present orphan cleanup as one bounded action,
including the source file and missing executable. This turns the failure into a
repeatable setup state instead of requiring an open-ended diagnostic session.

## Codex surface detection

Codex CLI and Codex Desktop are different monitoring surfaces and must not be
reported as one integration:

| Detected surface | Primary adapter | User action |
| --- | --- | --- |
| Codex CLI on `PATH` | lifecycle hooks | review the exact command hook with CLI `/hooks` |
| Desktop bundle with an executable Codex runtime | Desktop session observer; optional CLI hooks | no Desktop `/hooks` step |
| Desktop session store only | Desktop session observer | none |
| Neither surface | integration unavailable | install or start a supported Codex surface |

The installer detects surfaces; it does not assume that a `codex` shell
command exists. Hook-file presence is proof only of CLI configuration, never
proof that Desktop tasks are observable.

## CLI trust boundary

Codex requires non-managed command hooks to be reviewed against their exact
definition hash. DevBoard must not bypass or fabricate that trust decision.
For machines that actually run Codex CLI, after first setup or after a future
release intentionally changes the hook command definition:

1. open Codex CLI;
2. run `/hooks`;
3. review and trust only the handler whose command uses the stable DevBoard
   application-support path;
4. start a new Codex task.

Some Desktop releases bundle a CLI runtime at a path such as:

```text
/Applications/ChatGPT.app/Contents/Resources/codex
```

Packaging may discover that path at runtime and present it as an optional CLI
route, but must not treat it as a universal installation guarantee or as the
Desktop monitoring transport. The Desktop UI does not expose the CLI `/hooks`
trust browser.

Normal DevBoard upgrades keep the stable hook command unchanged. That avoids
forcing a new trust decision merely because the DevBoard binary contents were
updated.

## Result states

| Status | Meaning | Next action |
| --- | --- | --- |
| `setup_complete_requires_trust` | Node and provider files were configured | Select the detected Codex surface; CLI needs trust, Desktop needs its observer |
| `service_setup_failed` | LaunchAgent installation or ownership/health verification failed | Repair the reported service condition; provider files were not changed |
| `manual_configuration_required` | Codex user config contains active inline Hook definitions | Preserve the file and let the user reconcile it manually; generated `[hooks.state]` tables alone do not block setup |
| `repair_required` | Stable provider handlers are incomplete or only legacy handlers exist | Run setup again |
| `cleanup_required` | Stable and known legacy DevBoard handlers coexist | Run setup to migrate/remove legacy handlers |
| `configured_but_disabled` | Claude Code has `disableAllHooks=true` | User decides whether to enable Claude hooks |
| `invalid_configuration` | A provider JSON file is malformed or incompatible | Make no write; ask the user to repair or restore the file |

## Verification workflow

Configuration success and runtime success are separate checks:

```text
product setup + surface detection
→ LaunchAgent owns a healthy Node
→ provider status has no repair/cleanup state
→ CLI: user completes Codex trust review
→ Desktop: resident observer tails thread/session state
→ new Codex and Claude Code tasks emit or expose real lifecycle events
→ /api/state records lastAttemptAt and lastSuccessAt for both sources
→ Hub receives the updated Node snapshot
```

Do not claim provider activation from the presence of JSON alone. Acceptance
requires a real event from each installed provider. Diagnostics are used only
when one of these bounded checks fails.

## Upgrade and uninstall

- Upgrade runs the same `product setup` command. It preserves `node.yaml`,
  rewrites the stable helper atomically, restarts/verifies the LaunchAgent,
  migrates known legacy handlers, and repairs missing current handlers.
- Provider removal deletes exact current and known legacy DevBoard handlers
  while preserving unrelated settings.
- Service uninstall removes the managed binary and LaunchAgent plist while
  preserving Node configuration and logs.
- A future packaged uninstaller should remove provider handlers before the
  stable binary so no dead command remains in Codex or Claude Code.

## Packaging acceptance

The macOS package is not complete until its first-run/repair action invokes
the embedded helper's `product setup` operation, renders the structured phase
results, detects the available Codex surface, and starts the Desktop session
observer when Desktop is present. It must clearly expose CLI trust only when
CLI is the selected surface, and must never use
`--dangerously-bypass-hook-trust`.

## Multi-Mac Node onboarding closure

The Hub administrator can provision a new Node through the machine-readable
registry route. The admin secret is copied to the new Mac through the team's
approved secure channel; it is never committed or pasted into a process-list
argument. On the new Mac, after the DevBoard binary/package is installed, use
the following repeatable workflow:

```sh
# The file must be mode 0600 and contain only the Hub admin secret.
chmod 600 /secure/devboard-admin.token

# These two files are provisioned through the team's secure channel. The
# identity key is the same shared HMAC key on every Node; neither command
# below creates or copies either file. The alias file contains only safe
# accountKey=display-name mappings. Codex A, Codex B, and GLM are initial UI
# defaults and may be renamed.
chmod 600 /secure/devboard-quota.identity.key /secure/devboard-quota.aliases

# No files are changed and no Hub mutation is made.
devboard product node onboard \
  --node-id mac-b \
  --display-name "Laptop" \
  --hub-endpoint https://nas.example.test \
  --admin-token-file /secure/devboard-admin.token \
  --quota-identity-key-file /secure/devboard-quota.identity.key \
  --quota-alias-file /secure/devboard-quota.aliases \
  --dry-run

# Idempotently register the Node, write local node.yaml, merge both Hooks,
# install/repair the LaunchAgent, and report installation plus closure phases.
# The command can return pending/degraded after installation when first data
# has not arrived; that is not a Hub-closed result.
devboard product node onboard \
  --node-id mac-b \
  --display-name "Laptop" \
  --hub-endpoint https://nas.example.test \
  --admin-token-file /secure/devboard-admin.token \
  --quota-identity-key-file /secure/devboard-quota.identity.key \
  --quota-alias-file /secure/devboard-quota.aliases

# Re-run the local checks without changing config, Hooks, or LaunchAgents.
devboard product node onboard --check \
  --quota-identity-key-file /secure/devboard-quota.identity.key \
  --quota-alias-file /secure/devboard-quota.aliases
```

The quota key path must be absolute and point to an existing regular mode-0600
file containing at least 32 bytes. The alias path must also be absolute and
mode 0600; onboarding stores only its canonical HMAC account-key mappings in
`node.yaml`. It never prints or copies key contents. Use the same identity key
and safe user-managed display aliases on every Mac so one Codex account has
one stable cross-host key. `--dry-run` only validates these references. `--check` reports
local CodexBar/alias/source health separately from the Hub global check; the
Hub stage requires current coverage for two Codex accounts and one Z.ai account;
their display names are not identity. `pending` means
no first snapshot exists, while an expired or unavailable snapshot is
`degraded`.

Quota acceptance has two independent stages. The new Mac's local check only
needs to validate the quota sources that Mac actually observes (for example,
Codex A+B on one Mac or GLM on another), including accountKey, alias coverage,
source status, sampledAt, and usable windows. The Hub global check reads the
deduplicated `/api/dashboard` quota projection and requires fresh coverage for
two Codex accounts and one Z.ai account from online Nodes. Formal onboard and `--check`
report `installationStatus=complete` separately from `closureStatus`; an empty
first snapshot is `pending`, while unconfigured, stale, unavailable, expired,
or malformed quota data is not complete.

The first successful registration returns the independent Node token only to
the authenticated onboarding process, which writes it to the mode-0600 local
Node config. A repeated run reuses the existing registry token and does not
create a second registry entry. The result contains only phase names and
bounded statuses; it does not contain the admin secret, Node token, email,
private path, or provider output.

Manual two-Mac acceptance then proceeds in this order:

```text
Node A and Node B local health + activity.sock
→ Codex and Claude Code hooks emit one real event on each Mac
→ each Node uplink accepts its own snapshot/session
→ Hub /admin registry shows both IDs online
→ Hub /api/dashboard contains both host wrappers
→ Pad /display shows host-scoped tasks and grouped Host Health
```

`--check` reports the concrete failing phase. It does not uninstall or disable
anything. A fake-node test pass must not be reported as a physical second-Mac
pass until the second machine completes this workflow and the Hub accepts its
first snapshot.
