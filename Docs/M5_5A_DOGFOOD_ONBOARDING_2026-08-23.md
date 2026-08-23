# M5.5A Dogfood Onboarding

> Status: deployment assets built and locally validated; actual Mac/NAS
> installation remains blocked on auditor review.

M5.5A keeps the frozen Node → Hub push topology. The Hub never needs a Mac
LAN address, and normal onboarding does not require hand-editing Node YAML.

## 1. Start the NAS Hub

On the NAS checkout, from the repository root:

```bash
cd deploy/hub
./bootstrap.sh
docker compose up -d --build
```

Open:

```text
http://<NAS>:<PORT>/admin
```

Log in with the admin credential stored in the private
`deploy/hub/data/admin.token` file. Never paste it into a URL or log.

## 2. Add Mac A

In Hub Admin, choose **Add Node** and enter:

```text
Node ID: mac-a
Display Name: Mac A
```

Copy the generated Node token immediately. It is displayed only in that
mutation response and is not shown by later Admin pages.

## 3. Install and pair Mac A

On Mac A, from the repository checkout:

```bash
deploy/macos/install-node.sh
```

The per-user installer uses no `sudo`, starts the LaunchAgent unpaired, and
opens:

```text
http://127.0.0.1:8787/settings
```

Enter:

```text
Node ID: mac-a
Display Name: Mac A
Hub Endpoint: http://<NAS>:<PORT>
Token: <the one-time mac-a Node token>
Enable uplink: checked
```

Save. DevBoard writes the private config atomically, returns a success page,
and requests a graceful exit. LaunchAgent restarts the service; Mac A should
then appear online in Hub Admin and the display.

The stable installed provider-hook binary is:

```text
~/Library/Application Support/DevBoard/bin/devboard
```

M5.5A does not alter Claude or Codex hook configuration automatically. Use
the existing manual hook instructions in
[`M2_Agent_Hook_Setup_2026-08-20.md`](M2_Agent_Hook_Setup_2026-08-20.md)
with that stable binary path.

## 4. Add Mac B

Repeat the Hub Admin operation with:

```text
Node ID: mac-b
Display Name: Mac B
```

Run the same installer on Mac B and pair it through that Mac's own
`http://127.0.0.1:8787/settings` page using the one-time `mac-b` token.

## 5. Open the iPad display

Open:

```text
http://<NAS>:<PORT>/display
```

The existing Display remains the dogfood UI. Safe Navigation, Hub → Node
commands, remote execution controls, native SwiftUI packaging, signed DMG,
notarization, and the final visual redesign are deferred.

## Service operations

Mac status and uninstall:

```bash
deploy/macos/status-node.sh
deploy/macos/uninstall-node.sh
```

Uninstall preserves `node.yaml` by default. `--purge` explicitly removes the
private Node config; neither mode changes provider hook settings.

Hub operations and hardening details are in
[`../deploy/hub/README.md`](../deploy/hub/README.md).
