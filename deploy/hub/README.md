# DevBoard Hub for a NAS

This is the source-free DevBoard Hub product bundle for a `linux/amd64` NAS.
The NAS loads the included immutable image archive offline. It does not clone
source, install Go, build an image locally, or contact a registry.

## Bundle and verification

The bundle contains exactly these product files:

- `docker-compose.yml`, `bootstrap.sh`, `install.sh`, `rollback.sh`;
- `README.md`, `manifest.json`, `SHA256SUMS`;
- `devboard-hub-linux-amd64-image.tar`.

`manifest.json` records schema version, product version, source commit, the
`linux/amd64` platform, immutable image tag/digest, image archive SHA-256, and
the exact inventory. The image tag is bound to the full source commit, and the
runtime receives the same product version and commit through build args and
ldflags. The read-only `devboard version --json` output is checked inside the
built image before the manifest is written; authoritative builds reject
`development`/`unknown` provenance. `SHA256SUMS` covers every product file except itself. The
outer `DevBoard-Hub-linux-amd64.tar.gz.sha256` is a sidecar checksum for the
compressed artifact and avoids a self-referential checksum.

`manifest.json.imageDigest` is the portable `sha256:<digest>` of the Config
blob referenced by the final `docker save` archive's top-level `manifest.json`.
It is deliberately not the pre-save Docker engine's `docker image inspect
.Id`, because Docker versions may report different local image IDs for the
same shipped archive. The installer loads the archive and remains fail-closed
when the loaded image's Config digest does not match this manifest value.

The NAS needs Docker, Docker Compose v2, and `sha256sum` or `shasum`. Keep the
extracted directory together, preferably at:

```text
/volume1/docker/DevBoard/DevBoard-Hub
```

## Offline install

```sh
cd /volume1/docker/DevBoard/DevBoard-Hub
./install.sh
```

Before `docker load`, the installer fail-closes on malformed manifest,
inventory mismatch, any internal checksum mismatch, wrong image archive,
wrong image digest, or wrong platform. It never runs `docker build` and does
not use a registry. It then prepares private persistent data and starts with
`docker compose up -d --no-build --force-recreate`.

Compose has no development-image fallback. `DEVBOARD_HUB_IMAGE` must be
written by `install.sh` from the verified manifest before startup; a manual
`docker compose config` or `up` without that exact immutable tag fails closed.
The authoritative bundle builder also refuses a dirty source worktree.

The default published port is `8787`. A private `.env` beside the bundle may
set `DEVBOARD_HUB_PORT`, `DEVBOARD_UID`, and `DEVBOARD_GID`. The installer also
maintains the verified immutable image selection and rollback markers there;
host port, UID/GID, volume wiring, TLS proxy, and Docker log rotation remain
bundle/Compose settings, not Web settings.

## Operator Console

Open `http://<NAS>:8787/admin` on the trusted LAN. After the existing 12-hour
signed admin session is authenticated, `/admin` redirects to:

- **Overview** — Hub role/health, product provenance, uptime, registry and
  online/stale/offline counts, last accepted snapshot time, persistence
  readiness booleans, and the `/display` entry point;
- **Nodes** — the existing add/enable/disable/reset registry operations and
  one-time Node token display;
- **Settings** — only `operator.console_refresh_seconds` (5–60, default 10),
  `operator.diagnostics_min_level` (`info|warn|error`, default `info`), and
  `operator.diagnostics_capacity` (50–500, default 200);
- **Logs** — a bounded in-process ring of explicit, allow-listed, redacted
  application diagnostic events with level/component/limit filters.

Overview and Logs reconcile on the configured interval. Nodes refresh their
status, but a dirty Add Node form is preserved while only the status region is
updated. Settings, one-time token results, and POST result pages are static
until the operator navigates or explicitly reloads them. The Pad `/display`
refresh remains fixed at 2 seconds.

Settings are parsed, range-checked, unknown/duplicate/oversized fields are
rejected, and saved atomically before a supervised restart is requested. The
2-second Pad `/display` refresh and its Host/Agent/Quota semantics cannot be
changed here. Logs do not read arbitrary files, Docker logs, stdout history,
snapshots, prompts, task contents, account identifiers, tokens, cookies,
OAuth/API keys, private paths, or the Docker socket.

## Display and Node pairing

The always-on Pad surface is:

```text
http://<NAS>:8787/display
```

In **Nodes**, add a stable identity such as `mac-a`, copy the generated token
once, and complete pairing from the Mac's loopback Settings page. The Hub
accepts authenticated outbound snapshots; it never polls a Mac LAN address.

## Persistence, health, and logs

Persistent state lives beside Compose:

```text
DevBoard-Hub/data/config.yaml   Hub configuration and Node Registry
DevBoard-Hub/data/admin.token   private Admin credential
DevBoard-Hub/.env               host/container identity, port, image markers
```

`data/` is mode `0700`; config, admin credential, and `.env` are mode `0600`
and symlinks are rejected. Registry/config/admin credential survive restart.
Accepted snapshots are intentionally in-memory and repopulate when Nodes push
again. Compose uses a native healthcheck, `restart: unless-stopped`, a
persistent volume, read-only root filesystem, non-root UID/GID, tmpfs `/tmp`,
no-new-privileges, dropped capabilities, and bounded Docker JSON log rotation.

Use `docker compose ps` and the bounded `/health` endpoint for status. The
direct HTTP port is trusted-LAN only; public Internet/TLS product deployment
is not part of this V1 bundle. Put a separately managed HTTPS proxy/private
overlay in front before any untrusted-network access.

## Upgrade and rollback

1. Back up `data/` and `.env` to private NAS storage.
2. Verify the new outer sidecar checksum, then extract the new bundle without
   deleting `data/` or `.env`.
3. Run `./install.sh`; it validates manifest/checksums before image load,
   records a locally present verified previous image, and force-recreates with
   `--no-build`.
4. Wait for healthy status and verify `/display`, `/admin/overview`, and Nodes.

To roll back, run:

```sh
./rollback.sh
```

Rollback is fail-closed unless `.env` contains a verified previous immutable
image, its 64-character manifest marker, and the local image is present as
`linux/amd64`. It only switches the running image and force-recreates the
container; it never deletes or overwrites `data/`, config, admin credential,
or Node registry. Manual backup/restore of persistent data remains the
operator's responsibility.

## Security boundary

The direct HTTP Admin/Node port carries credentials without transport
encryption and is explicitly trusted-LAN-only. Do not expose it to the public
Internet. Keep tokens out of URLs, command history, screenshots, chat, support
archives, and logs.
