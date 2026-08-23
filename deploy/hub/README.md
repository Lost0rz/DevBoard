# DevBoard Hub product bundle

This directory is a runtime-only, source-free Hub installation bundle. The
operator needs Docker, Docker Compose, and the supplied
`devboard-hub-linux-amd64-image.tar`; the NAS does not need Go, a repository
checkout, or a Docker build.

## Install

Keep the six product files together and run:

```sh
./install.sh
```

The installer verifies Docker, Compose, the local SHA-256 tool, and the image
archive checksum before loading the archive. It then verifies the exact
`devboard/hub:dogfood` tag, bootstraps the private persistent directory, and
starts Compose with `--no-build`.

Bootstrap creates private `data/config.yaml`, `data/admin.token`, and `.env`
files only when absent. Existing configuration and credentials are preserved.
Credential contents are never printed by the installer or bootstrap script.

The resulting service keeps its non-root, read-only-root, tmpfs,
no-new-privileges, dropped-capabilities, restart, healthcheck, logging, and
persistent-volume settings in `docker-compose.yml`.

Hub display: `http://<NAS>:<PORT>/display`

Hub Admin: `http://<NAS>:<PORT>/admin`

For trusted-LAN dogfood, the default port is 8787. Set
`DEVBOARD_HUB_PORT` before running Compose to choose another host port.
