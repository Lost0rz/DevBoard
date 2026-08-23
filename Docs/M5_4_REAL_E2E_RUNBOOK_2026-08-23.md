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
  (`<NAS>` below — use the existing SSH config/alias; never invent an
  address), with SSH access and permission to run one binary and open one
  TCP port.
- Claude Code / Codex hooks installed on Mac A per
  [`M2_Agent_Hook_Setup_2026-08-20.md`](M2_Agent_Hook_Setup_2026-08-20.md)
  (only needed for the real-agent acceptance items 5–7).
- `curl` and `jq` on the observation machine (Mac A itself is fine).
- No `watch` or other non-default tooling is required: every polling step
  below is shell-native.
- No fixed Mac LAN IP anywhere in any config; the Hub address is the only
  cross-machine address configured.

Toolchain note: `go.mod` remains at Go 1.23.0 as the language/module
compatibility floor (CI additionally proves that floor on Linux); the
closure build uses a current supported Go compiler/linker because modern
macOS requires Mach-O `LC_UUID` (emitted by Go ≥ 1.24; old Go 1.23-era
Darwin binaries fail with `dyld: missing LC_UUID load command`).

Preflight (record the output in the evidence):

```bash
go version
```

Expected: `go version go1.26.x …`. Do not use an old Go 1.23 binary for this
closure validation on macOS 26.

## 2. Token generation (Mac A, kept in a mode-0600 file)

```bash
umask 077
openssl rand -hex 32 > /tmp/devboard-m54-token
chmod 600 /tmp/devboard-m54-token
ls -l /tmp/devboard-m54-token   # verify mode 600; never cat the file
```

32 cryptographically random bytes → 64 hex characters. The token now lives
only in that file and in the two temporary configs substituted from it
below. Never `cat` the token file, never paste the value into the runbook,
evidence notes or shell history, and never place it literally in a command
line — every step below reads it from the file.

## 3. Token transfer, then Hub config (on the NAS)

Transfer the token file to the NAS BEFORE any NAS-side config substitution,
and fix the mode on arrival:

```bash
# from Mac A
scp -p /tmp/devboard-m54-token <NAS>:/tmp/devboard-m54-token
```

```bash
# on the NAS
chmod 600 /tmp/devboard-m54-token
```

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

## 4. Node config (on Mac A, untracked)

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
go build -o /tmp/devboard-m54-node ./cmd/devboard
```

For the NAS Hub, cross-compile from the same commit for the NAS CPU (check
with `uname -m` on the NAS; mapping: `x86_64` → `amd64`, `aarch64`/`arm64`
→ `arm64`, `armv7*` → `arm` + `GOARM=7`):

```bash
GOOS=linux GOARCH=<nas-arch> go build -o /tmp/devboard-m54-hub ./cmd/devboard
scp /tmp/devboard-m54-hub <NAS>:/tmp/devboard-m54-hub
```

Record the binary hashes on both sides — the remote hash must match the
local one, proving the executed binary is this build:

```bash
shasum -a 256 /tmp/devboard-m54-node /tmp/devboard-m54-hub
ssh <NAS> 'sha256sum /tmp/devboard-m54-hub'
```

## 6. Start the Hub

On the NAS:

```bash
/tmp/devboard-m54-hub serve --config /tmp/devboard-hub.yaml > /tmp/devboard-hub.log 2>&1 &
```

Smoke check from Mac A (Node not started yet):

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
/tmp/devboard-m54-node serve --config /tmp/devboard-node.yaml > /tmp/devboard-node.log 2>&1 &
grep 'uplink session started' /tmp/devboard-node.log | tail -1
```

The Node starts local System/Network collectors and the agent ingest socket
on its own, without any Hub involvement, then the uplink begins pushing.

Observation from Mac A (shell-native poll, no `watch` needed):

```bash
i=0
while [ "$i" -lt 10 ]; do
  date -u +%FT%TZ
  curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
    | jq -c '.hosts[0] | {status: .source.status, freshness: .snapshotFreshness, hasState: (.state != null), lastSuccessAt: .source.lastSuccessAt}'
  i=$((i + 1))
  sleep 1
done
```

Evidence: `status == "online"`, `freshness == "fresh"`, `lastSuccessAt`
advancing every ~1 s (heartbeat), within ~1–2 s of uplink start. Record the
first online timestamp and the delta from node start.

Local collectors independent of the Hub (§41 item 8):

```bash
curl -fsS "http://127.0.0.1:8787/api/state" | jq '.system.cpuPercent, .network.quality, .generatedAt'
# repeat after ~10 s; values must keep updating while the Hub is untouched
```

## 8. Real agent task reaches the Hub (§41 items 5–7)

On Mac A, drive one REAL Claude Code or Codex session that is hooked into
DevBoard — a short, safe top-level task. Never fake this item: no manual
NodeSnapshot POSTs, no mock mode, no synthetic handlers, no unit-test
fixtures.

Observe on the Hub with bounded high-frequency polling (100 ms) until the
task appears and completes:

```bash
i=0
while [ "$i" -lt 100 ]; do
  curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
    | jq -c --args '.hosts[0].state.tasks[]? | {id, lifecycle, updatedAt}' "$(date -u +%FT%TZ)"
  i=$((i + 1))
  sleep 0.1 || sleep 1
done
```

Evidence to record:

1. the task/agent appears on the Hub within ~1 s of the local event
   (§41 item 6: a real checkpoint/attention/working change delivered ahead
   of the next 1 s heartbeat where observable — compare the local
   `task.updatedAt` with the first Hub observation timestamp and record the
   lag; do not fabricate an attention event just to tick the box — a real
   checkpoint change is acceptable evidence if no attention occurs);
2. the completion state reaches the Hub (§41 item 7);
3. the hub-side `state.sources` messages are only the generic public values
   (`"Source available."` / `"Source degraded."` / `"Source unavailable."`),
   never raw internal messages.

## 9. Stop the Node: ONLINE → STALE → OFFLINE, last-good (§41 items 9–10)

On Mac A, stop the Node cleanly and timestamp it:

```bash
date -u +%FT%TZ
pkill -f 'devboard-m54-node serve'
```

Poll the Hub with the shell-native 2 s loop:

```bash
i=0
while [ "$i" -lt 20 ]; do
  date -u +%FT%TZ
  curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
    | jq -c '.hosts[0] | {status: .source.status, freshness: .snapshotFreshness, hasState: (.state != null)}'
  i=$((i + 1))
  sleep 2
done
```

Evidence to record (each line with its timestamp):

- `online` for the first ≤ 5 s after the last accepted snapshot;
- `stale` after > 5 s and ≤ 30 s, with `hasState: true` and
  `freshness: "stale"` — last-good remains visible and clearly stale;
- `offline` after > 30 s, still with `hasState: true` and
  `freshness: "stale"` (retention window; this closure does not require
  waiting out the 30 min expiry).

## 10. Node restart creates a new session (§41 item 11)

Before restarting, record the current session-line count and the previous
session id (info-level; the session id is not a credential and must not be
confused with the bearer token):

```bash
before=$(grep -c 'uplink session started' /tmp/devboard-node.log || true)
grep 'uplink session started' /tmp/devboard-node.log | tail -1
# record the session=<...> value as old_session
```

Restart the Node with the same config, then bounded-poll (up to 10 s) for
the NEW session line — do not grep once and assume the log is flushed:

```bash
/tmp/devboard-m54-node serve --config /tmp/devboard-node.yaml >> /tmp/devboard-node.log 2>&1 &

i=0
while [ "$i" -lt 100 ]; do
  after=$(grep -c 'uplink session started' /tmp/devboard-node.log || true)
  [ "$after" -gt "$before" ] && break
  sleep 0.1 || sleep 1
  i=$((i + 1))
done
[ "$after" -gt "$before" ] || { echo "FAIL: new session line never appeared"; exit 1; }
grep 'uplink session started' /tmp/devboard-node.log | tail -1
# record the session=<...> value as new_session; evidence: old_session != new_session
```

Then verify the Hub returns to `online` WITHOUT any Hub restart:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | jq '.hosts[0].source.status'
```

## 11. Temporary network interruption reconnects (§41 item 12)

Safe, reversible interruption of the Node only — the operator manually
turns the Mac's Wi-Fi (or current node network link) off for ~20 s, then
back on. Never change router or NAS firewall settings for this.

While interrupted, from Mac A, the uplink retries are visible as periodic
attempts in the Node log with bounded backoff:

```bash
tail -f /tmp/devboard-node.log
```

Evidence: record the disconnect and reconnect times, observe the Hub go
`stale` (if the gap exceeds 5 s), and after connectivity returns the
dashboard flips back to `online` automatically WITHOUT restarting the Node:

```bash
i=0
while [ "$i" -lt 20 ]; do
  date -u +%FT%TZ
  curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
    | jq -c '.hosts[0] | {status: .source.status, hasState: (.state != null)}' || echo "hub unreachable"
  i=$((i + 1))
  sleep 2
done
```

Restore state: Wi-Fi back on — nothing else to undo.

## 12. Hub restart repopulates from heartbeat (§41 item 13)

The Node stays RUNNING throughout. On the NAS:

```bash
pkill -f 'devboard-m54-hub serve'          # stop the hub
# confirm the hub process is gone, then restart with the same registry config
/tmp/devboard-m54-hub serve --config /tmp/devboard-hub.yaml >> /tmp/devboard-hub.log 2>&1 &
```

Immediately after the Hub is back, its in-memory store has no accepted
snapshot; within ~1–2 s the Node heartbeat repopulates `mac-a`:

```bash
i=0
while [ "$i" -lt 10 ]; do
  date -u +%FT%TZ
  curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
    | jq -c '.hosts[0] | {status: .source.status, hasState: (.state != null)}'
  i=$((i + 1))
  sleep 1
done
```

Evidence: `offline` with `hasState: false` right after restart, then
`online` with state within ~1–2 s. Record the repopulate delta.

## 13. Privacy / log evidence (§41 item 15)

The checks match the ACTUAL secret via grep's pattern-file mode
(`-f /tmp/devboard-m54-token`) — never a literal placeholder, and the token
is never printed, echoed or placed on the command line. The two machines
have separate filesystems: run each machine's checks ON that machine.
Evidence records only counts (expected `0`).

On Mac A (Node):

```bash
grep -F -c -f /tmp/devboard-m54-token /tmp/devboard-node.log || true
grep -Ei 'authorization|bearer' /tmp/devboard-node.log || true
grep -Ei 'nodeSnapshot|schemaVersion' /tmp/devboard-node.log || true
```

On the NAS (Hub):

```bash
grep -F -c -f /tmp/devboard-m54-token /tmp/devboard-hub.log || true
grep -Ei 'authorization|bearer' /tmp/devboard-hub.log || true
grep -Ei 'nodeSnapshot|schemaVersion' /tmp/devboard-hub.log || true
```

Dashboard output — capture on Mac A into a temp file, check, remove:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" > /tmp/m54-dash.json
grep -F -c -f /tmp/devboard-m54-token /tmp/m54-dash.json || true
grep -E '/Users/' /tmp/m54-dash.json || true
rm /tmp/m54-dash.json
```

Expected: token counts `0`, no authorization/bearer log lines, no raw
snapshot body markers, no private paths. Also confirm the dashboard's
`state.sources[].message` values are only the generic public texts
(`Source available.` / `Source degraded.` / `Source unavailable.`):

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" \
  | jq -c '[.hosts[0].state.sources[] | .message] | unique'
```

## 14. Authority-boundary and no-poller evidence (§41 items 14, 16)

The no-fixed-Mac-IP proof is structural — grep-ing for a private subnet
would be wrong because the legitimate Hub address itself may live there.
The authority boundary is proven by WHAT each config is allowed to contain.

Node config on Mac A (precise assertions):

```bash
grep -n 'host: "127.0.0.1"' /tmp/devboard-node.yaml
grep -c 'endpoint:' /tmp/devboard-node.yaml
grep -c 'registered:' /tmp/devboard-node.yaml || echo "0: node runs no registry (PASS)"
```

Hub config on the NAS (precise assertions):

```bash
grep -c -E 'endpoint|peers' /tmp/devboard-hub.yaml || echo "0: no node endpoint / no peers (PASS)"
grep -c 'registered' /tmp/devboard-hub.yaml
```

Evidence: node has `server.host` exactly `127.0.0.1`, exactly one
`endpoint:` (the hub uplink address) and no registry; hub has `registered: 1`
and zero `endpoint|peers` lines. Manually confirm the hub registry line
contains only `node_id=display_name=token` — no Mac address field of any
kind. The Mac A address appears in NO config on either machine. (The Mac A
IP may appear transiently in diagnostics below — that is observation, not
configuration authority.)

No-poller (§41 item 16) — REQUIRED primary evidence, all of it already
collected above:

1. `multi_host` production peer path disabled/empty in both configs (the
   structural greps show zero `peers`);
2. the hub config contains no node address (zero `endpoint` lines);
3. every cross-machine interaction in items 1–15 succeeded with the Node
   initiating all traffic (the push topology working end to end IS the
   proof that no Hub→Node pull path was needed).

The historical poller's absence from the production runtime is verified by
the auditor from source; the run does not depend on it.

OPTIONAL corroboration — bare-SYN direction capture. Run this ONLY if the
NAS already has tcpdump AND sufficient permission, and only if capturing
does not disturb the NAS. If tcpdump/root is unavailable: SKIP OPTIONAL
NETWORK CAPTURE — item 16 must NOT be judged failed for missing it. Never
install packages or change the NAS firewall for this.

```bash
# on the NAS, ~30 s of normal heartbeat traffic:
sudo tcpdump -i any -n 'tcp port 8787 and tcp[13] = 2' > /tmp/m54-syn.log 2>&1 &
sleep 30 && sudo pkill -f 'tcpdump -i any -n tcp port 8787'
cat /tmp/m54-syn.log
```

Interpretation: a bare SYN (`Flags [S]`) is always the connection
INITIATOR; the SYN+ACK reply has flag byte `0x12` and is excluded by the
bare-SYN filter, so normal replies cannot fake an initiation. Expected:
only `IP <MAC-A-IP>.<ephemeral> > <NAS-IP>.8787: Flags [S]` lines (Mac A
initiating toward the Hub); ZERO `IP <NAS-IP>.8787 > <MAC-A-IP>…: Flags [S]`
lines (a hub poller would be exactly that shape). Observation-time IPs in
this diagnostic evidence are allowed — they are not configured identity.

## 15. Cleanup

```bash
# Mac A
pkill -f 'devboard-m54-node serve'
rm -f /tmp/devboard-m54-node /tmp/devboard-node.yaml /tmp/devboard-node.log /tmp/m54-dash.json
# NAS
pkill -f 'devboard-m54-hub serve'
rm -f /tmp/devboard-m54-hub /tmp/devboard-hub.yaml /tmp/devboard-hub.log /tmp/m54-syn.log
# both machines: remove the token file and any copies
rm -f /tmp/devboard-m54-token
```

Do not touch any pre-existing user configuration or services.

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
- [ ] 15. Privacy greps all clean — actual-token pattern-file counts all 0, per machine
- [ ] 16. Push-only acceptance succeeded; optional bare-SYN capture only if available

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
