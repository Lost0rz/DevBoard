# DevBoard Hub for a NAS

This is the source-free DevBoard Hub product bundle for a Synology-class
`linux/amd64` NAS. Normal installation is offline: the NAS loads the included
image and does not clone a repository, install Go, build an image, or contact a
container registry.

## Before you install

The NAS needs:

- Docker with access for the operator account;
- Docker Compose v2, available as `docker compose`;
- either `sha256sum` or `shasum` locally;
- enough free space for the compressed bundle, extracted image archive, loaded
  Docker image, and persistent Hub data.

Keep the six files in `DevBoard-Hub/` together. The canonical dogfood location
is:

```text
/volume1/docker/DevBoard/DevBoard-Hub
```

You may use an equivalent persistent local NAS path. Do not place the Hub data
directory on temporary storage.

## Install

Extract `DevBoard-Hub-linux-amd64.tar.gz` under
`/volume1/docker/DevBoard`. Then run from the extracted directory:

```sh
cd /volume1/docker/DevBoard/DevBoard-Hub
./install.sh
```

The installer checks Docker, Compose, and the SHA-256 utility; validates the
image archive checksum before any `docker load`; verifies the exact
`devboard/hub:dogfood` linux/amd64 image; prepares private persistent data; and
starts the Hub with `docker compose up -d --no-build`. Re-running the installer
loads the verified image again and recreates the container without deleting
persistent data.

The default published port is `8787`. To keep a different port across future
restarts, create a private `.env` file beside `docker-compose.yml` before the
first install and add, for example:

```text
DEVBOARD_HUB_PORT=8788
```

Bootstrap preserves that file and adds the non-secret container UID/GID
settings when missing. Keep `.env` private even though it must not contain
credentials.

## Status and health

From the extracted directory:

```sh
docker compose ps
docker compose logs --tail 100 devboard-hub
```

`docker compose ps` should report the `devboard-hub` service as running and,
after startup, healthy. You can also open the bounded health endpoint on the
trusted LAN:

```text
http://<NAS>:8787/health
```

Replace `8787` with the configured host port. A healthy Hub returns status
`ok` with role `hub`.

## Display and Admin

Open the always-on board:

```text
http://<NAS>:8787/display
```

Open Hub administration:

```text
http://<NAS>:8787/admin
```

The admin credential is stored only in the private local file
`data/admin.token`. Read that file through a secure local NAS session or private
file viewer and paste the value into the Admin login form. Do not put it in a
URL, command history, issue, chat, screenshot, or log. The installer and
bootstrap intentionally never print the credential.

## Create and pair a Node

In Admin, choose **Add Node**, enter a stable Node ID such as `mac-a` and a
display name, then save the generated Node token immediately. It is shown only
in the successful Admin mutation result.

On that Mac, open its loopback DevBoard Settings page, normally
`http://127.0.0.1:8787/settings`. Enter the same Node ID, display name, this
Hub's endpoint, and the one-time Node token; enable uplink and save. The Mac
then sends authenticated snapshots outbound to the Hub. The Hub never polls a
Mac LAN address, and an IP address is not Node identity.

## Persistent data and routine operation

The bundle stores durable Hub state beside Compose:

```text
DevBoard-Hub/data/config.yaml   Hub configuration and Node Registry
DevBoard-Hub/data/admin.token   private Admin credential
DevBoard-Hub/.env               port and stable container UID/GID settings
```

The container mounts `data/` at `/var/lib/devboard`. Bootstrap keeps the data
directory private and config/credential files mode `0600`. Hub snapshots are
current in-memory state and repopulate from outbound Node traffic after a Hub
restart; the Registry, enable/disable state, and credentials persist on disk.

Restart the service without changing data:

```sh
docker compose restart devboard-hub
docker compose ps
```

Stop and start it later with:

```sh
docker compose down
docker compose up -d --no-build
```

Do not add `--build` and do not run `docker compose build` on the NAS.

## Upgrade or reinstall from a newer bundle

1. Back up the existing `data/` directory and `.env` file using private NAS
   storage. Never include them in a support bundle or source archive.
2. Verify that the new artifact came from the expected delivery channel.
3. Extract the newer `DevBoard-Hub/` over the existing product directory, or
   replace only its six shipped product files. Do not delete `data/` or `.env`.
4. Run `./install.sh` again from the product directory.
5. Run `docker compose ps`, wait for healthy status, and open `/display` and
   `/admin`.

The installer verifies and loads the newer prebuilt image, preserves existing
Hub configuration/Registry/Admin credentials, and force-recreates the service
with `--no-build` so the same canonical image tag moves to the newly loaded
image. There is no automatic updater and no NAS-local source build path.

## Network and security boundary

The direct HTTP port is for an explicitly trusted LAN only. HTTP carries Admin
and Node credentials without transport encryption. Never expose the raw Hub
port directly to the public Internet.

Before any access across an untrusted network or the Internet, put the Hub
behind a trusted HTTPS/TLS reverse proxy or private encrypted overlay, restrict
access, and configure Nodes to use the protected HTTPS endpoint. Keep the
container hardening in `docker-compose.yml`: non-root execution, read-only root
filesystem, private persistent mount, tmpfs `/tmp`, no-new-privileges, all
capabilities dropped, bounded logs, native healthcheck, and
`restart: unless-stopped`.
