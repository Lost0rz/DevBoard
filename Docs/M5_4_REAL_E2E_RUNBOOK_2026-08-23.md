# M5.4 Real Mac A → NAS Hub E2E Runbook (2026-08-23)

> Status:
>
> ```text
> M5_4_MAC_A_NODE_HUB_E2E = PENDING
> ```
>
> This runbook is the frozen §41 acceptance procedure. It may only be flipped
> to `PASS` after the real run has been executed on real hardware with the
> evidence checklist below completed. This document itself must never contain
> a real bearer token, a real NAS address, or any private credential.

Authority: [`Docs/contracts/m5-2-node-hub-ingestion-v1.md`](contracts/m5-2-node-hub-ingestion-v1.md) §41.

## 0. Timing and retention expectations (frozen)

| Observation | Expected |
| --- | --- |
| Node → Hub first accepted snapshot | within ~1–2 s of uplink start |
| Dashboard connection status ONLINE | `lastReceivedAt` age ≤ 5 s |
| Dashboard connection status STALE | age > 5 s and ≤ 30 s |
| Dashboard connection status OFFLINE | age > 30 s |
| Last-good state retention | 30 min from last accepted/received success |
| After retention expiry | nested state dropped, registered node wrapper stays OFFLINE |

## 1. Prerequisites and toolchain preflight

- Mac A (the monitored Node) with this repository checked out on the closure
  branch and **Go 1.26.x** installed — required for this closure run.
- The NAS Hub machine, reachable over the LAN at a stable address
  (`<NAS-HUB-ADDRESS>` below), with SSH access and permission to run one
  binary and open one TCP port.
- Claude Code / Codex hooks installed on Mac A per
  [`M2_Agent_Hook_Setup_2026-08-20.md`](M2_Agent_Hook_Setup_2026-08-20.md)
  (only needed for the real-agent acceptance items 5–7).
- `curl` and `jq` on the observation machine (Mac A itself is fine).
- No fixed Mac LAN IP anywhere in any config; the Hub address is the only
  cross-machine address configured.

Toolchain note: `go.mod` remains at Go 1.23.0 as the language/module
compatibility floor; the closure build uses a current supported Go
compiler/linker because modern macOS requires Mach-O `LC_UUID` (emitted by
Go ≥ 1.24; old Go 1.23-era Darwin binaries fail with
`dyld: missing LC_UUID load command`).

Preflight (record the output in the evidence):

```bash
go version
```

Expected: `go version go1.26.x …`. Do not use an old Go 1.23 binary for this
closure validation on macOS 26.

## 2. Token generation (kept in a mode-0600 file)

On Mac A:

```bash
umask 077
openssl rand -hex 32 > /tmp/devboard-m54-token
```

32 cryptographically random bytes → 64 hex characters. The token now lives
only in that file and in the two temporary configs substituted from it
below. Never `cat` the token file, never paste the value into the runbook,
evidence notes or shell history, and never place it literally in a command
line — the steps below read it from the file.

## 3. Temporary Hub config (on the NAS, untracked)

Write the template `/tmp/devboard-hub.yaml.tmpl` on the NAS (no real values
yet):

```yaml
runtime:
  role: hub
server:
  host: "0.0.0.0"
  port: 8787
display:
  dashboard_refresh_seconds: 2
nodes:
  registered: "mac-a=Mac A=<TOKEN_FROM_STEP_2>"
```

Substitute the token from the file without printing it (awk reads the token
file directly; the value never enters the command line or the terminal):

```bash
awk 'NR==FNR { tok = $0; next } { gsub(/<TOKEN_FROM_STEP_2>/, tok); print }' \
  /tmp/devboard-m54-token /tmp/devboard-hub.yaml.tmpl > /tmp/devboard-hub.yaml \
  && rm /tmp/devboard-hub.yaml.tmpl
```

(Transfer `/tmp/devboard-m54-token` to the NAS first — for example via
`scp` with the same 0600 mode — or generate the token on the NAS and copy it
to Mac A instead; pick one side as the source of truth.)

## 4. Temporary Node config (on Mac A, untracked)

Write the template `/tmp/devboard-node.yaml.tmpl`:

```yaml
runtime:
  role: node
server:
  host: "127.0.0.1"
  port: 8787
host:
  id: "mac-a"
  display_name: "Mac A"
agent:
  stale_after_seconds: 900
network:
  probe_address: "1.1.1.1:443"
  probe_timeout_milliseconds: 1500
uplink:
  enabled: true
  endpoint: "http://<NAS-HUB-ADDRESS>:8787"
  node_id: "mac-a"
  token: "<TOKEN_FROM_STEP_2>"
```

Substitute exactly as on the hub side:

```bash
awk 'NR==FNR { tok = $0; next } { gsub(/<TOKEN_FROM_STEP_2>/, tok); print }' \
  /tmp/devboard-m54-token /tmp/devboard-node.yaml.tmpl > /tmp/devboard-node.yaml \
  && rm /tmp/devboard-node.yaml.tmpl
```

Notes:

- `uplink.node_id` must equal `host.id` (`mac-a`).
- Production prefers `https://`; explicit `http://` here is the documented
  trusted-LAN engineering exception (frozen §30). If the Hub terminates TLS
  in front of the binary, use the https URL instead and keep everything else
  identical.
- The config parser does not strip inline comments — keep `key: value`
  lines clean.

## 5. Build

On Mac A (for the Node), with the Go 1.26.x toolchain from the preflight:

```bash
go version
go build -o /tmp/devboard ./cmd/devboard
```

For the NAS Hub, cross-compile for the NAS CPU (check with `uname -m` on the
NAS; typical values `x86_64` → `amd64`, `armv7`/`aarch64` → `arm`/`arm64`):

```bash
GOOS=linux GOARCH=<nas-arch> go build -o /tmp/devboard-hub ./cmd/devboard
scp /tmp/devboard-hub /tmp/devboard-m54-token <nas-user>@<NAS-HUB-ADDRESS>:/tmp/
```

(If the token was generated on the NAS instead, scp only the binary and copy
the token back to Mac A.)

## 6. Start the Hub

On the NAS:

```bash
chmod 600 /tmp/devboard-m54-token
/tmp/devboard-hub serve --config /tmp/devboard-hub.yaml > /tmp/devboard-hub.log 2>&1 &
```

Smoke check from Mac A:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/health" | jq .
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | jq '.hosts[] | {configuredHostId, displayName, source: .source.status, hasState: (.state != null)}'
```

Expected before any Node starts: exactly one host `mac-a`, status `offline`,
`hasState: false` — the registered wrapper with no fabricated NAS host card
(§41 item 4, first half).

## 7. Start the Node (§41 items 1–3)

On Mac A, record the startup session identity first (info-level log; the
session id identifies the uplink process and is not a credential):

```bash
/tmp/devboard serve --config /tmp/devboard-node.yaml > /tmp/devboard-node.log 2>&1 &
grep 'uplink session started' /tmp/devboard-node.log | tail -1
```

The Node starts local System/Network collectors and the agent ingest socket
on its own, without any Hub involvement, then the uplink begins pushing.

Observation from Mac A:

```bash
# Within ~1–2 s: status flips to online with state present.
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
  | jq '.hosts[] | {id: .configuredHostId, status: .source.status, freshness: .snapshotFreshness, lastSuccessAt: .source.lastSuccessAt}'
```

Evidence: `status == "online"`, `freshness == "fresh"`, `lastSuccessAt`
advancing every ~1 s (heartbeat). Record one `jq` output with a timestamp.

Local collectors independent of the Hub (§41 item 8):

```bash
curl -fsS "http://127.0.0.1:8787/api/state" | jq '.system.cpuPercent, .network.quality'
# repeat after ~10 s; values must keep updating while the Hub is untouched
```

## 8. Real agent task reaches the Hub (§41 items 5–7)

On Mac A, drive one real Claude Code or Codex session that is hooked into
DevBoard (a short prompt that produces a checkpoint / attention state and
then completes).

Observe on the Hub until the lifecycle appears and completes:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
  | jq '.hosts[0].state.tasks[]? | {id, title, lifecycle, freshness}'
```

Evidence to record:

1. the task/agent appears on the Hub within ~1 s of the local event
   (checkpoint/attention wake ahead of the 1 s heartbeat where observable —
   compare the local event time with `lastSuccessAt` progression);
2. the completion state reaches the Hub;
3. the hub-side `state.sources` messages are only the generic public values
   (`"Source available."` / `"Source degraded."` / `"Source unavailable."`),
   never raw internal messages.

## 9. Stop the Node: ONLINE → STALE → OFFLINE, last-good (§41 items 9–10)

On Mac A, stop the Node cleanly:

```bash
pkill -f 'devboard serve --config /tmp/devboard-node.yaml'
```

Timestamp the kill (`date -u +%FT%TZ`), then poll the Hub:

```bash
watch -n 2 'curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | jq -c ".hosts[0] | {status: .source.status, freshness: .snapshotFreshness, hasState: (.state != null)}"'
```

Evidence to record:

- `online` for the first ≤ 5 s after the last accepted snapshot;
- `stale` between > 5 s and ≤ 30 s, with `hasState: true` and
  `freshness: "stale"` — last-good remains visible and clearly stale;
- `offline` after > 30 s, still with `hasState: true` (retention window).

Optional (30 min retention boundary, §41 item 10 full depth): keep observing
~31 min after the stop; at > 30 min the nested state is dropped
(`hasState: false`) while the registered wrapper remains.

## 10. Node restart creates a new session (§41 item 11)

Before restarting, record the previous session id from the Node log
(info-level; the session id is not a credential and must not be confused
with the bearer token):

```bash
grep 'uplink session started' /tmp/devboard-node.log | tail -1
# record the session=<...> value as old_session
```

Restart the Node with the same config:

```bash
/tmp/devboard serve --config /tmp/devboard-node.yaml >> /tmp/devboard-node.log 2>&1 &
grep 'uplink session started' /tmp/devboard-node.log | tail -1
# record the session=<...> value as new_session
```

Evidence: `old_session != new_session`, and the Hub returns to `online`
within ~1–2 s **without any Hub restart or reconfiguration**:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | jq '.hosts[0].source.status'
```

## 11. Temporary network interruption reconnects (§41 item 12)

Safe, reversible interruption of the Node only (never touch the Hub or other
machines) — turn the Mac's Wi-Fi off for ~20 s (or unplug the Ethernet cable),
then back on.

```bash
# during the interruption, from Mac A, the uplink retries are visible as
# periodic attempts in /tmp/devboard-node.log with bounded backoff
tail -f /tmp/devboard-node.log
```

Evidence: after connectivity returns, the dashboard flips back to `online`
automatically (record the recovery `jq` output). If the pending envelope aged
past the 30 s admission window during the interruption, the uplink abandons
it and rebuilds a fresh snapshot — also automatic, no manual action.

Restore state: Wi-Fi back on — nothing else to undo.

## 12. Hub restart repopulates from heartbeat (§41 item 13)

On the NAS:

```bash
pkill -f 'devboard-hub serve'          # stop the hub
/tmp/devboard-hub serve --config /tmp/devboard-hub.yaml >> /tmp/devboard-hub.log 2>&1 &
```

The Node is NOT restarted. Evidence: immediately after the Hub is back, the
dashboard shows `offline` with no state (in-memory store lost), then within
~1–2 s the Node heartbeat repopulates `mac-a` to `online` with state.

## 13. Privacy / log evidence (§41 item 15)

The checks below match the ACTUAL secret via grep's pattern-file mode
(`-f /tmp/devboard-m54-token`) — never the literal placeholder, and the
token is never printed, echoed or placed on the command line. Evidence
records only counts:

```bash
# Token never appears in any log:
grep -F -c -f /tmp/devboard-m54-token /tmp/devboard-node.log /tmp/devboard-hub.log || true

# Token never appears in Dashboard/API output:
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" > /tmp/m54-dash.json
grep -F -c -f /tmp/devboard-m54-token /tmp/m54-dash.json || true
rm /tmp/m54-dash.json

# Authorization headers are never logged:
grep -Ei 'authorization|bearer' /tmp/devboard-node.log /tmp/devboard-hub.log || true

# Raw snapshot/public-state bodies are never logged:
grep -Ei 'nodeSnapshot|schemaVersion' /tmp/devboard-node.log /tmp/devboard-hub.log || true
```

Expected: every count `0` / no matches (`grep -c` prints `0` per file; the
`|| true` only keeps the script going because grep exits 1 on zero matches).
Record the counts verbatim — there is nothing else to redact.

Also confirm no absolute private source path crosses the projection:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | grep -E '/Users/' || true
```

Expected: no matches.

## 14. Authority-boundary and no-poller evidence (§41 items 14, 16)

The no-fixed-Mac-IP proof is structural — grep-ing for a private subnet
would be wrong because the legitimate Hub address itself may live there.
The authority boundary is proven by WHAT each config is allowed to contain:

```bash
# Hub config: the only node references are registry entries
# (node_id=display_name=token). There is no node endpoint, no per-node
# address field and no peer list:
grep -c 'registered' /tmp/devboard-hub.yaml
grep -c -E 'endpoint|peers' /tmp/devboard-hub.yaml || echo "0: no node endpoint / no peers (PASS)"

# Node config: the ONLY cross-machine address is uplink.endpoint pointing
# at the HUB; the local server binds loopback and the node has no registry:
grep -c 'endpoint' /tmp/devboard-node.yaml
grep -c 'registered' /tmp/devboard-node.yaml || echo "0: node runs no registry (PASS)"
grep -n 'host:' /tmp/devboard-node.yaml   # shows the loopback server host line
```

Evidence: hub has `registered: 1` and `endpoint|peers: 0`; node has
`endpoint: 1` (the hub address), `registered: 0`, `server host 127.0.0.1`.
The Mac A address appears in NO config on either machine. (The Mac A IP may
appear transiently in diagnostics below — that is observation, not
configuration authority.)

Hub-runs-no-poller evidence — connection-initiation direction. A bare SYN
(`Flags [S]`) is always the connection INITIATOR; the reply is SYN+ACK
(`Flags [S.]`, tcp flag byte `0x12`, excluded by the bare-SYN filter
`tcp[13] = 2`). Normal reply traffic therefore cannot fake an initiation:

```bash
# On the NAS, capture bare SYNs to/from the hub port for ~30 s of normal
# heartbeat traffic, then stop it:
sudo tcpdump -i any -n 'tcp port 8787 and tcp[13] = 2' > /tmp/m54-syn.log 2>&1 &
sleep 30 && sudo pkill -f 'tcpdump -i any -n tcp port 8787'
cat /tmp/m54-syn.log
```

Interpretation (record with the log):

- lines `IP <MAC-A-IP>.<ephemeral> > <NAS-IP>.8787: Flags [S]` — Mac A
  initiating toward the Hub: expected, this IS the push topology;
- lines `IP <NAS-IP>.8787 > <MAC-A-IP>.<ephemeral>: Flags [S]` — the Hub
  initiating toward Mac A: must be ZERO. A historical hub poller would be
  exactly this shape.

The primary proof that the historical poller is not production authority
remains the source code and runtime plan (the hub binary contains no
poller); the SYN capture is confirming network evidence. Observation-time
IPs in this diagnostic evidence are allowed — they are not configured
identity.

## 15. Cleanup

```bash
# Mac A
pkill -f 'devboard serve' ; rm -f /tmp/devboard /tmp/devboard-node.yaml /tmp/devboard-node.log
# NAS
pkill -f 'devboard-hub serve' ; rm -f /tmp/devboard-hub /tmp/devboard-hub.yaml /tmp/devboard-hub.log /tmp/m54-syn.log
# both machines: remove the token file and any copies
rm -f /tmp/devboard-m54-token
```

## 16. Evidence checklist

Copy this checklist into the closure record and tick every line:

- [ ] 0. Preflight `go version` output recorded (Go 1.26.x)
- [ ] 1. Mac A Node started collectors + agent ingest with no Hub dependence
- [ ] 2. Node reached the configured Hub address; Hub never needed Mac A's address
- [ ] 3. Token authenticated `mac-a` (200 acks; 401 with a wrong token spot-check optional)
- [ ] 4. Dashboard showed only registered `mac-a`, no NAS host card
- [ ] 5. Real Claude/Codex task state reached the Hub
- [ ] 6. Checkpoint/attention wake observed ahead of heartbeat where observable
- [ ] 7. Completion reached the Hub
- [ ] 8. Local System/Network kept updating throughout
- [ ] 9. ONLINE → STALE (≤30 s) → OFFLINE (>30 s) after Node stop, with timestamps
- [ ] 10. Last-good stayed visible and clearly stale through retention
- [ ] 11. Node restart recovered without Hub restart (old_session != new_session recorded)
- [ ] 12. ~20 s network interruption reconnected automatically
- [ ] 13. Hub restart repopulated from Node heartbeat within ~1–2 s
- [ ] 14. Structural authority-boundary checks recorded (hub: registry only, no node endpoint; node: hub endpoint only, loopback server)
- [ ] 15. Privacy greps all clean — actual-token pattern-file counts all 0
- [ ] 16. Bare-SYN capture: only Mac A → Hub initiations; zero Hub → Mac A bare SYNs

## 17. Final closure

Only after every checklist line above carries real recorded evidence may this
be changed:

```text
M5_4_MAC_A_NODE_HUB_E2E = PENDING
```

to:

```text
M5_4_MAC_A_NODE_HUB_E2E = PASS
```

— and only by the auditor who reviewed the evidence. This remediation batch
does NOT set it.
