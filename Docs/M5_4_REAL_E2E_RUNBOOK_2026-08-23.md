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

## 1. Prerequisites

- Mac A (the monitored Node) with this repository checked out on the closure
  branch and a working Go toolchain.
- The NAS Hub machine, reachable over the LAN at a stable address
  (`<NAS-HUB-ADDRESS>` below), with SSH access and permission to run one
  binary and open one TCP port.
- Claude Code / Codex hooks installed on Mac A per
  [`M2_Agent_Hook_Setup_2026-08-20.md`](M2_Agent_Hook_Setup_2026-08-20.md)
  (only needed for the real-agent acceptance items 5–7).
- `curl` and `jq` on the observation machine (Mac A itself is fine).
- No fixed Mac LAN IP anywhere in any config; the Hub address is the only
  cross-machine address configured.

## 2. Token generation

On any machine:

```bash
openssl rand -hex 32
```

32 cryptographically random bytes → 64 hex characters. Record the value only
in the temporary, untracked config files below. Never commit it, never paste
it into the runbook or evidence notes, never echo it into a shell transcript
that will be saved.

## 3. Temporary Hub config (on the NAS, untracked)

Create `/tmp/devboard-hub.yaml` on the NAS — outside the repo, deleted after
the run:

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

## 4. Temporary Node config (on Mac A, untracked)

Create `/tmp/devboard-node.yaml` on Mac A:

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

Notes:

- `uplink.node_id` must equal `host.id` (`mac-a`).
- Production prefers `https://`; explicit `http://` here is the documented
  trusted-LAN engineering exception (frozen §30). If the Hub terminates TLS
  in front of the binary, use the https URL instead and keep everything else
  identical.
- Keep the token out of shell history where practical
  (`history -d` / edit the file with an editor instead of `echo`).

## 5. Build

On Mac A (for the Node):

```bash
go build -o /tmp/devboard ./cmd/devboard
```

For the NAS Hub, cross-compile for the NAS CPU (check with `uname -m` on the
NAS; typical values `x86_64` → `amd64`, `armv7`/`aarch64` → `arm`/`arm64`):

```bash
GOOS=linux GOARCH=<nas-arch> go build -o /tmp/devboard-hub ./cmd/devboard
scp /tmp/devboard-hub <nas-user>@<NAS-HUB-ADDRESS>:/tmp/devboard-hub
```

## 6. Start the Hub

On the NAS:

```bash
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

On Mac A:

```bash
/tmp/devboard serve --config /tmp/devboard-node.yaml > /tmp/devboard-node.log 2>&1 &
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

Restart the Node with the same config:

```bash
/tmp/devboard serve --config /tmp/devboard-node.yaml >> /tmp/devboard-node.log 2>&1 &
```

Evidence: recovery to `online` within ~1–2 s **without any Hub restart or
reconfiguration** — the new uplink process generates a new random session id
and the Hub accepts it as a session switch. The Hub log line for the first
accepted snapshot after restart shows a new session/sequence pair (debug
log); at info level, simply record the recovery timestamp and the unchanged
Hub process start time.

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

From Mac A and the NAS:

```bash
# Token never appears in any log or API response:
grep -c '<TOKEN_FROM_STEP_2>' /tmp/devboard-node.log /tmp/devboard-hub.log || true
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | grep -c '<TOKEN_FROM_STEP_2>' || true

# Authorization headers are never logged:
grep -Ei 'authorization|bearer' /tmp/devboard-node.log /tmp/devboard-hub.log || true

# Raw snapshot/public-state bodies are never logged:
grep -Ei 'nodeSnapshot|schemaVersion' /tmp/devboard-node.log /tmp/devboard-hub.log || true
```

Expected: all counts `0` / no matches. Record the command outputs verbatim
(redacting nothing else — there must be nothing to redact).

Also confirm no absolute private source path crosses the projection:

```bash
curl -fsS "http://<NAS-HUB-ADDRESS>:8787/api/dashboard" | grep -E '/Users/' || true
```

Expected: no matches.

## 14. No fixed Mac LAN IP / no historical poller (§41 items 14, 16)

Evidence:

- `grep -R '192\.168\.' /tmp/devboard-node.yaml /tmp/devboard-hub.yaml` →
  no Mac LAN IP in any config (the Node config contains only the Hub address;
  the Hub config contains no node addresses at all);
- the Hub runs no poller: `grep -i poll /tmp/devboard-hub.log` → nothing, and
  acceptance above succeeded with only Node→Hub POST traffic (verify once
  with `sudo tcpdump -i any host <MAC-A-ADDRESS> and port 8787` if desired:
  only Mac A → NAS SYNs, never NAS → Mac A connections).

## 15. Cleanup

```bash
# Mac A
pkill -f 'devboard serve' ; rm -f /tmp/devboard /tmp/devboard-node.yaml /tmp/devboard-node.log
# NAS
pkill -f 'devboard-hub serve' ; rm -f /tmp/devboard-hub /tmp/devboard-hub.yaml /tmp/devboard-hub.log
```

## 16. Evidence checklist

Copy this checklist into the closure record and tick every line:

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
- [ ] 11. Node restart recovered without Hub restart (new session evidence)
- [ ] 12. ~20 s network interruption reconnected automatically
- [ ] 13. Hub restart repopulated from Node heartbeat within ~1–2 s
- [ ] 14. No fixed Mac LAN IP in any config or contract
- [ ] 15. Privacy greps all clean (token / Authorization / raw bodies / private paths)
- [ ] 16. No Hub-originated polling observed or required

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
