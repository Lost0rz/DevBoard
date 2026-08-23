# M5.4 Real Mac A → NAS Hub E2E Evidence (2026-08-23)

> ```text
> M5_4_MAC_A_NODE_HUB_E2E = PASS
> ```
>
> Audited E2E baseline:
>
> ```text
> 26f16010300ff398411ab7cb38f7ac4545a96fec
> ```
>
> All evidence below is sanitized: no real bearer/admin token, no real NAS
> address, no private path is recorded here. The executing session's report
> and the auditor's review are the closure authority for this file.

## Environment

| Side | Facts |
| --- | --- |
| Mac A (Node) | macOS 26.6.2, arm64, Go 1.26.7 |
| NAS (Hub) | Synology DS920+ class, Linux, x86_64 → `GOARCH=amd64`, identified only as `<NAS>` |

Both binaries were built from the audited baseline commit above; the hub
binary deployed to `<NAS>` was verified by SHA256 before execution.

## Binary SHA256

```text
node  ac8dbb21f2295d745a9592fbf98e43b28e23cb3c199817b5e8a786f4641e4f0b
hub   b1ecff1077e3af0d6bb228df2904e23e7ce92afe2e3afff895e7c2df53dec239
```

## Frozen §41 acceptance — 16/16 PASS

| # | Item | Result | Key evidence (UTC 2026-08-23) |
| --- | --- | --- | --- |
| 1 | Node starts collectors + agent ingest without Hub | PASS | local `/api/state` live (system/network/sources) before any uplink dependency |
| 2 | Node reaches Hub; Hub never needs Mac address | PASS | hub config contains zero node endpoints; node holds only the hub endpoint |
| 3 | Valid token authenticates mac-a | PASS | token from mode-0600 file via awk substitution; first accepted snapshot 02:03:11.120Z |
| 4 | Dashboard only registered mac-a, no NAS host | PASS | pre-node: hostCount=1, offline, `state=null`; no fabricated host |
| 5 | Real Claude/Codex task reaches Hub | PASS | real `claude -p` session; task `task-ea305e262ea3a41fe19d679f209264c3` visible on Hub |
| 6 | Change wake before heartbeat where observable | PASS | working change updatedAt 02:04:18.270516Z → Hub observed 02:04:18.335Z (~65 ms) |
| 7 | Completion reaches Hub | PASS | complete updatedAt 02:04:37.430335Z → observed 02:04:37.539Z (~109 ms) |
| 8 | Local System/Network keeps updating | PASS | cpu 24.70→29.48, mem 73.92→74.30, network good, 10 s apart |
| 9 | ONLINE → STALE → OFFLINE on Node stop | PASS | stop 02:05:38Z; first stale 02:05:44Z (>5 s); first offline 02:06:09Z (31 s) |
| 10 | Last-good remains clearly stale | PASS | `hasState=true` with `freshness="stale"` through stale and offline |
| 11 | Restart creates new session, no Hub restart | PASS | session `731270380884652e349edc5f8e052282` → `eed9715012c525f3916587068bd91745`; Hub online again <1 s |
| 12 | Temporary network interruption reconnects | PASS | real Wi-Fi interruption; same Node process (PID unchanged, no restart); bounded retry ladder observed (1→2→4→8→15 s cap); pending envelopes expired past the 30 s window and rebuilt with fresh sequences (152→155); automatic recovery |
| 13 | Hub restart repopulates from heartbeat | PASS | fresh hub observed offline with no state, repopulated online in ~2 s; Node never restarted |
| 14 | No fixed Mac LAN IP authority | PASS | structural: hub config = registry only (no endpoint, no peers); node config = loopback bind + single hub endpoint |
| 15 | No token/raw sensitive data in Dashboard/logs | PASS | actual-token pattern-file grep: Node log = 0, Hub log = 0, Dashboard = 0; no Authorization/bearer lines; no raw snapshot body; no `/Users/` path; source messages are the three generic public values only |
| 16 | Historical Hub poller not required | PASS | multi_host peer authority absent in both configs; full acceptance succeeded via Node → Hub push only |

## Change-wake delivery statement

The real E2E run observed a real Claude Code task's lifecycle changes
delivered to the Hub in ~65 ms (working) and ~109 ms (completion) against a
1000 ms heartbeat cadence: **real E2E corroborates change-wake delivery;
deterministic scheduler tests establish causality** (`TestM54SchedulerRemembersPublicChangeDuringTransientBackoff`,
`TestM54SchedulerPublicChangeSendsBeforeHeartbeat` and siblings in
`internal/uplink`). No claim of mathematical impossibility of heartbeat
coincidence is made.

## Timing summary (frozen thresholds)

```text
online ≤5 s                observed ~3.1 s process start → online
stale >5 s and ≤30 s       observed first stale at +6 s
offline >30 s              observed at +31 s
hub repopulate ~1–2 s      observed ~2.0 s
change delivery <1 s       observed 65 ms / 109 ms
```

## No-pull evidence

- Hub config had no Node endpoint of any kind.
- multi_host peer authority was absent from both configs.
- The full E2E succeeded with Node → Hub push as the only cross-machine path.
- The optional bare-SYN tcpdump corroboration was skipped because the NAS
  requires root privileges for capture; this is NOT a failure — the frozen
  item's required evidence is the structural and behavioral proof above.

## Environment deviations (non-blocking)

- The E2E Hub used port 8788 because a historical service occupied 8787 on
  the NAS. The frozen contract does not mandate a port.
- An old M5.1-era Mac test Node was stopped before M5.4 acceptance to free
  the agent socket and port; its files were preserved.
- The NAS `/tmp` mount is noexec, so the hub binary ran from the user home
  (hash-verified identical).
- Synology required `scp -O` (legacy SCP protocol).
- The temporary SSH credential used during the run was revoked afterwards.
- No real address, token or private credential is stored in the repository.
