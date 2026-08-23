# M5.5A Dogfood Onboarding

> Status: code readiness has passed independent core audit. Persistent Mac A +
> NAS installation is authorized for the real M5.5A dogfood acceptance run.
> M5.5A now closes on the stable single-node dogfood loop defined by the
> auditor-owned scope amendment; real Mac B validation is deferred to M5.5B.

Authoritative implementation contract:

`Docs/contracts/m5-5a-dogfood-deployment-v1.md`

Closure-scope amendment:

`Docs/contracts/m5-5a-single-node-closure-scope-amendment-v1.md`

```text
M5_5A_DOGFOOD_DEPLOYMENT_CONTRACT = FROZEN_V1
M5_5A_SINGLE_NODE_CLOSURE_SCOPE = FROZEN_V1
M5_5A_CODE_READINESS = PASS
M5_5A_REAL_DOGFOOD_ACCEPTANCE = PENDING
```

M5.5A keeps the frozen Node → Hub push topology. The Hub never needs a Mac
LAN address, and normal onboarding does not require hand-editing Node YAML.
The implementation remains multi-node-capable; only the second real-machine
acceptance is deferred.

The authorized M5.5A dogfood loop is:

```text
Mac A LaunchAgent
→ Node /settings
→ outbound authenticated Node → Hub uplink
→ NAS canonical Docker Compose
→ Hub Admin / dashboard / display
```

Do not treat installation alone as closure; the single-node acceptance gate in
the scope amendment still has to pass.

## Security boundary for Hub Admin

The direct `http://<NAS>:<PORT>/admin` form is permitted only for an
explicitly trusted-LAN dogfood environment. The admin credential travels over
that HTTP transport.

Outside that controlled LAN, use HTTPS or a trusted reverse proxy that
terminates TLS before forwarding to the Hub. Never expose the raw cleartext
Hub Admin port directly to the public Internet.

## 1. Start the NAS Hub

The accepted Synology dogfood path uses a prebuilt, audited `linux/amd64`
image. The image is built from the audited source on Mac, saved and hashed,
transferred to the NAS, then loaded there. The NAS does not need to contact a
container registry or compile Go source during normal deployment.

From `deploy/hub` on the NAS, after loading the verified image and tagging it
as `devboard/hub:dogfood`:

```sh
docker compose up -d --no-build
```

The canonical deployment definition remains:

`deploy/hub/docker-compose.yml`

Open on the trusted LAN:

```text
http://<NAS>:<PORT>/admin
```

Log in with the admin credential stored in the private
`deploy/hub/data/admin.token` file. Never paste it into a URL, issue, log, or
repository file.

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

A cleartext `http://` Hub endpoint is trusted-LAN dogfood only. Prefer HTTPS
or trusted TLS termination outside that boundary.

Save. DevBoard writes the private config using the frozen M5.5A atomic-save
semantics, returns a success page, and requests a graceful exit. LaunchAgent
restarts the service; Mac A should then appear online in Hub Admin and the
display.

The stable installed provider-hook binary is:

```text
~/Library/Application Support/DevBoard/bin/devboard
```

M5.5A does not alter Claude or Codex hook configuration automatically. Use
the existing manual hook instructions in
[`M2_Agent_Hook_Setup_2026-08-20.md`](M2_Agent_Hook_Setup_2026-08-20.md)
with that stable binary path.

## 4. Use the browser display

On the trusted LAN, open:

```text
http://<NAS>:<PORT>/display
```

For M5.5A this surface must remain usable as the always-on view of current Mac
A state. The implementation continues to retain multi-node data/UI support;
M5.5A simply does not require a second physical Mac to close.

Safe Navigation, Hub → Node commands, remote execution controls, native
SwiftUI packaging, signed DMG, notarization, and the final visual redesign are
deferred.

## 5. Mac B is deferred to M5.5B

Do not add or pair Mac B as part of M5.5A closure.

Real Mac B pairing and independent Mac A/Mac B presence on Hub/Display are
tracked separately in Issue #5. Existing multi-node Registry, API, state and
Display interfaces must remain intact; no single-node-only shortcut is
permitted.

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
