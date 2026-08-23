# DevBoard Hub dogfood deployment

This Compose deployment runs the push-only DevBoard Hub on a NAS with a
persistent registry/config directory and a separate admin credential file.
It does not poll Mac addresses and does not deploy anything to a Mac.

The authoritative deployment contract is:

`Docs/contracts/m5-5a-dogfood-deployment-v1.md`

Actual persistent installation remains blocked until core-auditor PR/CI
acceptance.

## Bootstrap and start

From the repository root, enter the deployment directory so Compose loads
the bootstrap-generated `.env` identity file:

```sh
cd deploy/hub
./bootstrap.sh
docker compose up -d --build
```

Bootstrap creates these private, untracked files only when absent:

- `deploy/hub/data/config.yaml`
- `deploy/hub/data/admin.token`
- `deploy/hub/.env` with the non-secret UID/GID used for the bind mount

Existing config and admin credentials are never overwritten. The secret is
generated from 32 random bytes, stored mode `0600`, and never printed.

To choose a host port, set it in the Compose environment, for example:

```sh
DEVBOARD_HUB_PORT=18787 docker compose up -d --build
```

The container runs without root privileges or Linux capabilities, with a
read-only root filesystem, a temporary `/tmp`, `no-new-privileges`, and
`restart: unless-stopped`. Its native healthcheck is expected to use the
absolute installed binary path:

```sh
/usr/local/bin/devboard healthcheck --url http://127.0.0.1:8787/health --expect-role hub
```

## Admin transport boundary

Direct Hub Admin over:

```text
http://<NAS>:<PORT>/admin
```

is **trusted-LAN dogfood only**. The admin credential travels over that
cleartext transport.

For any environment outside the explicitly controlled LAN, prefer HTTPS or a
trusted reverse proxy that terminates TLS before forwarding to DevBoard. Do
not expose the raw cleartext Hub Admin port directly to the public Internet.

Do not put the admin credential in a URL, command line, issue, log, or
repository file.

## Admin and display

Use `/admin` to add, enable, disable, or reset a Node. Add/reset returns a new
Node token once; copy it immediately into that Mac's loopback-only Settings
page. The Hub saves its config according to the frozen M5.5A atomic-save
contract and exits gracefully, then Docker restarts it and Nodes repopulate
the in-memory state through push heartbeats.

The iPad dogfood display remains `http://<NAS>:<PORT>/display` on the trusted
LAN.

## Operations

```sh
docker compose ps
docker compose logs --tail=100 devboard-hub
docker compose restart devboard-hub
docker compose down
```

`down` leaves the bind-mounted data directory intact. Back up the private
`data` directory through the NAS's normal protected backup mechanism; it
contains Node bearer credentials and the separate admin secret.
