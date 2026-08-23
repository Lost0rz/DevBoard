# DevBoard Hub dogfood deployment

This Compose deployment runs the push-only DevBoard Hub on a NAS with a
persistent registry/config directory and a separate admin credential file.
It does not poll Mac addresses and does not deploy anything to a Mac.

## Bootstrap and start

From the repository root, enter the deployment directory so Compose loads
the bootstrap-generated `.env` identity file:

```bash
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

```bash
DEVBOARD_HUB_PORT=18787 docker compose up -d --build
```

The container runs without root privileges or Linux capabilities, with a
read-only root filesystem, a temporary `/tmp`, `no-new-privileges`, and
`restart: unless-stopped`. Its native healthcheck is:

```bash
devboard healthcheck --url http://127.0.0.1:8787/health --expect-role hub
```

## Admin and display

Open `http://<NAS>:<PORT>/admin` and enter the credential stored locally in
`deploy/hub/data/admin.token`. Do not put the credential in a URL, command
line, issue, log, or repository file.

Use `/admin` to add, enable, disable, or reset a Node. Add/reset returns a new
Node token once; copy it immediately into that Mac's loopback-only Settings
page. The Hub saves its config atomically and exits gracefully, then Docker
restarts it and Nodes repopulate the in-memory state through push heartbeats.

The iPad dogfood display remains `http://<NAS>:<PORT>/display`.

## Operations

```bash
docker compose ps
docker compose logs --tail=100 devboard-hub
docker compose restart devboard-hub
docker compose down
```

`down` leaves the bind-mounted data directory intact. Back up the private
`data` directory through the NAS's normal protected backup mechanism; it
contains Node bearer credentials and the separate admin secret.
