# DevBoard PC1 NAS / Hub Product — Long-Running Construction Specification

Status: **AUDITOR-OWNED / FROZEN FOR PC1-NAS**  
Workstream: **NAS / Hub Product Delivery**  
Parent: GitHub Issue #8 / child Issue #11  
Construction branch: `codex/pc1-nas`  
Purpose: finish the NAS/Hub side as a real self-contained, source-free product bundle that can be transferred to the Synology-class NAS and installed without repository checkout, Go, or NAS-local image build. This is product construction, not Hub architecture redesign and not long-run reliability/hardening.

```text
PC1_NAS_CONSTRUCTION_SPEC = FROZEN_V1
```

## 1. Final product outcome

The NAS workstream is complete when a user/operator can receive one artifact:

```text
DevBoard-Hub-linux-amd64.tar.gz
```

extract it on the NAS, run the documented installer flow, and obtain a running DevBoard Hub using the existing runtime architecture without needing source code or a build toolchain on the NAS.

The extracted product bundle is self-contained:

```text
DevBoard-Hub/
  docker-compose.yml
  bootstrap.sh
  install.sh
  README.md
  devboard-hub-linux-amd64-image.tar
  devboard-hub-linux-amd64-image.tar.sha256
```

The expected operational result is:

```text
prebuilt audited linux/amd64 Hub image
      ↓
verified archive + checksum
      ↓
Synology NAS
      ↓
docker load
      ↓
devboard/hub:dogfood
      ↓
docker compose up -d --no-build
      ↓
DevBoard Hub
  ├─ /api/node/v1/snapshot
  ├─ /display
  └─ authenticated /admin
```

This workstream prepares the product bundle. Real user NAS installation and Mac-to-NAS acceptance are performed later during the full integration audit/real-hardware phase.

## 2. Authority order

When requirements appear to conflict, use this order:

1. This PC1-NAS construction specification for NAS productization/delivery scope.
2. `Docs/contracts/m5-5a-dogfood-deployment-v1.md` — frozen Hub Admin, registry mutation/restart, Docker hardening and NAS bootstrap behavior.
3. `Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md` — accepted single-node-first real deployment path and prebuilt-image provenance chain.
4. `Docs/contracts/m5-2-node-hub-ingestion-v1.md` — frozen Node->Hub push topology, receiver/auth identity, Hub authority, liveness and privacy.
5. `Docs/contracts/m5-1-always-on-hub-v1.md` — inherited Hub runtime/deployment semantics where not superseded by M5.2/M5.5A.
6. `Docs/contracts/mvp-monitoring-v1.md` — read-only monitoring product intent; NAS is infrastructure/aggregation authority, not a monitored Mac.
7. `Docs/M5_5A_DOGFOOD_ONBOARDING_2026-08-23.md` and `Docs/M5_5A_SINGLE_NODE_DOGFOOD_EVIDENCE_2026-08-23.md` are operational/historical evidence only, not higher authority than frozen contracts.

Do not use older pull-based multi-host behavior as production authority. M5.2 supersedes Hub->Mac polling with authenticated outbound Node->Hub push.

## 3. Existing foundation to reuse

The Hub runtime already exists. Reuse it rather than rebuilding product logic:

- Hub runtime role;
- Registry and independent Node credentials;
- authenticated `POST /api/node/v1/snapshot` receiver;
- NodeStateStore/liveness/last-good retention;
- DashboardState and browser handlers;
- authenticated `/admin` with CSRF and one-time Node token mutation result;
- persisted Hub config/admin token model;
- container healthcheck and non-root runtime;
- current Compose/bootstrap files;
- current image/bundle builder and installer foundation.

NAS work owns packaging, operator flow and deployment correctness. It does not own receiver, registry, state, frontend or Mac runtime design.

## 4. Design freedom

The NAS Agent may freely improve/refactor the NAS-owned delivery layer to make the bundle reliable, understandable and repeatable. It may:

- reorganize `deploy/hub` scripts/docs;
- make bootstrap/install more portable and idempotent;
- improve preflight/error messages;
- improve checksum/image-tag verification;
- improve upgrade/reinstall instructions;
- improve bundle build verification;
- add focused shell/static tests within the owned area where practical.

The Agent may not change the Hub runtime contract, receiver/auth/state behavior, Dockerfile, Web product UI or Mac product to make packaging easier.

If a deployment requirement appears to need a Hub runtime change, Dockerfile change, new machine API, new credential model, frontend change or Mac-side change, STOP and report the exact integration need.

## 5. Owned implementation boundary

Owned paths:

```text
deploy/hub/docker-compose.yml
deploy/hub/bootstrap.sh
deploy/hub/install.sh
deploy/hub/README.md
scripts/build-hub-bundle.sh
```

Additional files may be added under `deploy/hub/` only when they are product-delivery assets/scripts and do not create a second conflicting canonical deployment path. If adding a file changes the exact final product bundle contents, STOP and request Auditor approval first because the bundle shape is frozen below.

Do not modify:

```text
Dockerfile
internal/hub/**
internal/uplink/**
internal/state/**
internal/config/**
internal/web/**
internal/product/**
macos/**
Docs/contracts/**
.github/workflows/**
```

Product artifact CI integration is owned by the later integration lane, not by this workstream.

## 6. Frozen topology and identity boundary

Production cross-machine state remains:

```text
Mac Node -> authenticated outbound snapshot -> NAS Hub
```

NAS/Hub must not:

- poll Mac LAN IP addresses;
- use a Mac IP address as Node identity;
- require inbound connectivity to Mac Nodes;
- restore the historical pull poller as production authority.

Hub is the multi-node aggregation authority. Node identity remains configured Node ID + independent credential binding. PC1 real acceptance is one Mac first, but NAS packaging must not simplify the runtime into a one-node-only product.

## 7. Frozen runtime image contract

Canonical runtime image tag:

```text
devboard/hub:dogfood
```

The NAS Compose model must reference the prebuilt image and must not include a runtime `build:` stanza for the product install flow.

The image is built from audited repository source on a capable build machine/CI as linux/amd64, then saved into the bundle.

The target NAS must not need:

- repository checkout;
- Go toolchain;
- Docker buildx/build;
- source files;
- registry access for the normal offline/local dogfood installation path.

## 8. Exact product bundle

The extracted bundle contents are exactly:

```text
DevBoard-Hub/
  docker-compose.yml
  bootstrap.sh
  install.sh
  README.md
  devboard-hub-linux-amd64-image.tar
  devboard-hub-linux-amd64-image.tar.sha256
```

The final packaged artifact is:

```text
dist/DevBoard-Hub-linux-amd64.tar.gz
```

`build-hub-bundle.sh` must stage the exact files above, save an image archive that contains the exact `devboard/hub:dogfood` tag, generate SHA-256, create the tar.gz and fail if expected output is missing/empty.

The builder may use Docker Buildx on the build machine and should explicitly build for:

```text
linux/amd64
```

## 9. NAS installer flow

`deploy/hub/install.sh` is the user/operator installation entrypoint inside the extracted bundle.

Required logical order:

```text
verify docker executable
-> verify docker compose works
-> verify local SHA-256 tool
-> verify image archive/checksum files exist
-> verify checksum successfully
-> only then docker load
-> verify exact devboard/hub:dogfood tag exists
-> run bootstrap.sh
-> docker compose up -d --no-build
-> print bounded next steps
```

Checksum failure must happen before `docker load`.

The installer must not invoke:

```text
docker build
docker compose build
docker compose up -d --build
```

No routine NAS product instruction may require a local source build.

## 10. Bootstrap contract

`bootstrap.sh` remains POSIX-oriented for Synology-class environments:

```text
#!/bin/sh
set -eu
```

Avoid Bash-only requirements such as arrays, `[[ ... ]]`, `BASH_SOURCE` or mandatory `pipefail`.

Bootstrap must:

- create private persistent data when missing;
- preserve existing config and admin secret on rerun;
- create a secure admin secret when absent;
- keep data/config/secret permissions private according to the frozen deployment contract;
- preserve stable UID/GID configuration expected by Compose;
- be idempotent;
- never print the admin secret or Node credentials.

Repeated installation/upgrade must not silently erase existing Hub configuration/registry data.

## 11. Compose final state

`docker-compose.yml` remains the single canonical NAS product runtime entrypoint.

It must preserve the accepted hardened model, including as applicable in the current file:

- `image: devboard/hub:dogfood`;
- `restart: unless-stopped`;
- non-root user/runtime;
- persistent `/var/lib/devboard` data/config/admin state;
- healthcheck using the DevBoard binary and Hub-role expectation;
- read-only root filesystem where already supported;
- tmpfs `/tmp`;
- `no-new-privileges`;
- drop all Linux capabilities;
- not privileged;
- bounded Docker log rotation;
- only the intended published Hub port.

Do not weaken a security setting simply to make installation easier. If a real Synology limitation conflicts with a frozen hardening property, report it for integration audit instead of silently removing the property.

## 12. Operator README final state

`deploy/hub/README.md` is a product operator guide, not a developer source-build tutorial.

It must clearly explain:

1. prerequisites: Docker + Docker Compose on the NAS;
2. where to place/extract the bundle (canonical dogfood location `/volume1/docker/DevBoard` unless the operator deliberately chooses an equivalent local path);
3. how to run `install.sh`;
4. how to inspect `docker compose ps`/health;
5. how to open `/display`;
6. how to open/login to `/admin` without printing the secret in normal output;
7. where persistent data lives;
8. how a Node is created in Admin and paired from Mac Settings at a conceptual level;
9. how to restart the Hub;
10. how to install an updated bundle/image without rebuilding on the NAS while preserving persistent data;
11. trusted-LAN HTTP boundary and requirement for HTTPS/trusted TLS proxy before Internet exposure.

Do not document raw Admin HTTP as Internet-safe.

## 13. Credential/privacy boundary

NAS scripts/output must never print or embed in logs/output:

- `admin.token` contents;
- Node bearer tokens except existing one-time Admin Web mutation semantics owned by Hub runtime;
- full Hub config contents;
- raw snapshot payloads;
- prompt/transcript/provider data.

The product bundle itself must not include private dogfood config, `.env`, credentials or local coordination artifacts.

## 14. Upgrade and reinstall behavior

The workstream should make the source-free product flow usable repeatedly.

A normal upgrade should conceptually be:

```text
receive new verified DevBoard-Hub bundle
-> stop/replace runtime image through docker load
-> keep persistent data/config/admin credential
-> docker compose up -d --no-build
-> verify health
```

The exact operator commands may be refined inside the README/install flow, but must preserve persistent data and avoid NAS-local source builds.

Do not create an automatic updater or remote registry dependency in PC1.

## 15. Real-hardware handoff boundary

Bundle construction is not proof that the real NAS is currently installed correctly.

This workstream may prove:

- archive builds;
- exact contents;
- checksum correctness;
- Compose validity;
- scripts' shell/static behavior;
- local Docker image/tag/build behavior where Docker is available.

It must not claim real NAS completion until the later integration/real-hardware phase performs the actual Synology install and validates:

- Hub container healthy;
- `/display` reachable;
- `/admin` reachable/authenticated;
- persistent registry/config survives restart;
- Mac Node pairs and pushes real state.

## 16. Explicit non-goals

PC1-NAS does not implement:

- Hub receiver/Registry/NodeStateStore redesign;
- Web UI redesign;
- macOS application/service/provider integration;
- Browser AI Watch;
- Quota collector;
- Safe Navigation/remote control;
- Process Groups;
- Mac B real-hardware acceptance;
- Kubernetes;
- database/Redis/MQ;
- NAS-local source build;
- public-Internet auth/TLS product beyond documenting the trusted-LAN/TLS-proxy boundary.

## 17. Validation expectations

At minimum:

```text
sh -n deploy/hub/bootstrap.sh
sh -n deploy/hub/install.sh
bash -n scripts/build-hub-bundle.sh
docker compose -f deploy/hub/docker-compose.yml config
git diff --check
```

When Docker/buildx is available:

```text
scripts/build-hub-bundle.sh
```

Then extract and verify:

- artifact exists and is non-empty;
- top-level extracted directory is `DevBoard-Hub`;
- exactly six required files are present;
- image archive checksum matches;
- image archive can be loaded and contains exact tag `devboard/hub:dogfood`;
- installer contains no NAS-side image build path;
- Compose contains image reference and no product runtime `build:` stanza.

Full repository Go tests may also be run as regression validation, but this workstream must not alter Go runtime code to make packaging pass.

## 18. Definition of NAS-complete handoff

The Agent hands back the workstream only when:

- `DevBoard-Hub-linux-amd64.tar.gz` is reproducibly buildable;
- extracted bundle has the exact six-file product shape;
- checksum is verified before image load;
- exact image tag is verified;
- bootstrap/install are idempotent, bounded and secret-safe;
- Compose remains hardened and source-free at runtime;
- README is a complete NAS operator flow rather than a source build guide;
- normal install/upgrade does not require source, Go or NAS Docker build;
- no Hub runtime/frontend/Mac contract was redefined;
- validation passes and branch is committed/pushed cleanly.

The Agent reports exact START_HEAD, FINAL_HEAD, REMOTE_HEAD, changed files, bundle contents, image/checksum/install/Compose validation and remaining concerns. It must not merge, edit frozen contracts, claim real user NAS installation, or claim full PC1 product acceptance. Core Auditor performs the later three-workstream integration audit and real Mac+NAS acceptance.
