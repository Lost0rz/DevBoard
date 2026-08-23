# DevBoard PC1 Unified Product Integration — Closure Construction Specification

Status: **AUDITOR-OWNED / FROZEN FOR PC1-INT**

Workstream: **Frontend + macOS + NAS unified integration closure**

Parent: **GitHub Issue #8**

Baseline branch: `codex/pc1-unified-baseline`

Construction branch: `codex/pc1-integration-closure`

```text
PC1_UNIFIED_INTEGRATION_SPEC = FROZEN_V1
```

## 1. Purpose

This specification freezes the narrow construction step after the three PC1
product modules have completed their separate delivery branches. It combines
the accepted module outputs into one common baseline and authorizes only the
remaining integration-gate corrections.

This is not a new feature milestone, not a redesign, not a real-hardware
deployment authorization, and not permission to reopen module product scope.

The exact construction START_HEAD is the pushed head of
`codex/pc1-unified-baseline` containing this specification. The Core Auditor
reports that full SHA to the Construction Agent out of band so the document
does not contain a self-referential commit identifier.

## 2. Frozen module inputs

The unified baseline contains these exact audited construction heads, all
descending from the common frozen specification commit
`fe85d3505cf729b9410037ea373a4d9259da45af`:

```text
Frontend  3583cdccdd5cbe386ddd40f92475e8c89dd7e4ba
macOS     dbb6efd0dae357bf402e3692ac22ff9a9293a47e
NAS       5ba2ba73e1978380140e49f553cfc861ff2faac0
```

The integration Agent must preserve all three module outcomes. It must not
rebase away their provenance, replace one module with an older branch, or
silently omit a module change.

## 3. Audited starting state

The Core Auditor has established the following facts before this freeze:

- all three module worktrees were clean and their delivery heads matched the
  corresponding remote heads;
- Frontend full Go test/race/vet/build validation passed, and real browser
  desktop/mobile inspection found no console warning or horizontal overflow;
- the macOS universal product artifact built successfully, both the App and
  embedded helper contained `arm64` and `x86_64`, and strict ad-hoc signature
  verification passed;
- the NAS source-free bundle built successfully, contained exactly the six
  frozen product files, passed checksum verification, and contained the exact
  `devboard/hub:dogfood` `linux/amd64` image;
- the three module heads merge automatically, and the combined tree passed
  local Go test/race/vet/build plus shell and Compose validation;
- the current real Mac Node and NAS Hub remain on the earlier dogfood product,
  so real installation of this unified product is still a later acceptance
  phase rather than evidence available to this code-construction step.

## 4. Remaining audited blockers

### 4.1 Portable product CLI grammar

On non-Darwin systems, this invalid public command:

```text
devboard product service invalid
```

currently reaches the platform implementation and returns
`unsupported_platform`. The frozen public grammar requires malformed or
unsupported action names to return `invalid_command` before platform dispatch.
This breaks both Linux Go CI and the Go 1.23 compatibility lane.

Required outcome:

- validate the service action against exactly
  `install|status|restart|uninstall` before calling `product.RunService`;
- retain `unsupported_platform` for a syntactically valid service operation on
  an unsupported operating system;
- retain one bounded schema-v1 JSON result and exit code 1 for invalid input;
- add or refine portable tests so grammar behavior does not depend on GOOS.

### 4.2 Hub deployment CI authority mismatch

The final NAS product correctly separates responsibilities:

```text
bootstrap.sh  -> prepare/preserve private persistent state only
install.sh    -> verify/load the product image, bootstrap, then start Compose
```

The existing Go CI workflow still requires the literal routine start command
inside `bootstrap.sh`. That check reflects the older deployment layout and now
fails every PC1 module branch.

Required outcome:

- keep `bootstrap.sh` non-starting;
- make CI require `docker compose up -d --no-build` semantics in the operator
  README and installer, not in bootstrap;
- continue rejecting routine NAS-side `--build` deployment paths;
- keep shell syntax and Compose-model validation;
- do not weaken the source-free image, checksum, tag, non-root, persistence,
  healthcheck or bounded-log requirements.

The CI assertion may account for safe additional options such as
`--force-recreate`; it should test the product invariant instead of forcing an
obsolete file ownership model.

## 5. Authorized implementation boundary

The Construction Agent may modify only:

```text
cmd/devboard/product.go
cmd/devboard/product_test.go
.github/workflows/ci.yml
```

No other source, test, workflow, documentation, deployment or contract file is
authorized for this construction step.

If a required correction appears to need any other file, the Agent must stop
and report the exact need instead of expanding scope.

## 6. Frozen interfaces and non-goals

The Agent must not change:

- Node -> Hub topology, wire format, auth, identity or liveness semantics;
- PublicState, DashboardState or state reduction;
- Hub Registry/Admin/NodeStateStore behavior;
- Frontend templates, CSS, JavaScript or presentation view models;
- macOS SwiftUI product, service ownership, provider configuration mutation or
  Node status API;
- NAS Compose, bootstrap, installer, bundle builder or operator README;
- any frozen contract under `Docs/contracts/`;
- product artifact shapes;
- Provider hook event sets or trust semantics.

This step does not implement Quota, Browser AI Watch, Mac B, Safe Navigation,
Process Groups, Developer ID signing/notarization, auto-update or long-run
hardening.

## 7. Required validation

At minimum run from the unified construction tree:

```text
gofmt -w cmd/devboard/product.go cmd/devboard/product_test.go
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
git diff --check
bash -n deploy/macos/*.sh
sh -n deploy/hub/*.sh
docker compose -f deploy/hub/docker-compose.yml config
```

The Agent must also prove on Linux, through CI or an equivalent clean Linux
environment:

```text
go test ./...
go vet ./...
go build ./cmd/devboard
```

and prove the declared Go 1.23 compatibility lane remains green.

Do not claim the remote gate is green from a local Darwin-only run.

## 8. Definition of construction-complete handoff

The construction handoff is complete only when:

- invalid product service actions are rejected before platform dispatch;
- valid service actions retain platform-specific behavior;
- Go CI no longer requires bootstrap to start the Hub;
- routine NAS installation remains source-free and no-build;
- all local validation passes;
- Linux/current-Go and Linux/Go-1.23 validation pass;
- the working tree is clean;
- the construction branch is committed and pushed;
- the Agent reports START_HEAD, FINAL_HEAD, REMOTE_HEAD, changed files, exact
  commands/results and remaining concerns.

The Agent must not merge, deploy to the real Mac or NAS, close GitHub issues,
edit this frozen specification, or claim full PC1 acceptance. The Core Auditor
performs post-construction audit, product-artifact CI, and the later real
Mac + NAS upgrade/dogfood acceptance.
