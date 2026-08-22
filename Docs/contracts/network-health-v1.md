# DevBoard M3.2 — Network Health V1 Technical Contract

> Date: 2026-08-22  
> Base: `codex/mvp-monitoring-business-contract-freeze` @ `e256ff56b01016086bb6294df58757b475c7c0e8`  
> Reference audit: `Docs/contracts/network-health-reference-audit-v1.md`  
> Status: **M3.2 TECHNICAL CONTRACT FROZEN**  
> Scope: lightweight local per-host Network Health only. No Process Groups and no M4 work.

## 1. Goal

M3.2 adds one independent local Network Health source that answers:

- can the configured network probe target be reached now;
- how long a bounded TCP connection takes when reachable;
- how often recent bounded probes fail;
- what current receive/send load is observed on the route interface when reliably measurable;
- whether the network collector itself is healthy/fresh.

It does not claim to provide full Internet SLA monitoring or true IP-layer packet loss.

## 2. Explicit non-goals

M3.2 does not implement:

- Process Groups;
- per-process traffic;
- packet capture;
- DNS analytics;
- traceroute;
- Wi-Fi signal diagnostics;
- speed tests;
- provider/API-specific health checks;
- persistent network history;
- remote host aggregation;
- multi-host transport;
- M4 AI Task Observability;
- Browser AI Watch;
- Quota;
- Safe Navigation/control;
- new Kindle network presentation.

## 3. Reference-first implementation choice

The frozen implementation route is:

```text
TCP reachability/latency
  → Go standard net.Dialer

Traffic counters
  → existing github.com/shirou/gopsutil/v4 v4.25.9

Rate semantics
  → DevBoard delta calculation following proven Glances-style repeated-counter semantics

Probe semantics
  → small embedded bounded-probe pattern inspired by Prometheus blackbox_exporter
```

M3.2 adds no new Go dependency.

`prometheus-community/pro-bing` is deferred because true ICMP semantics are not required for MVP and may introduce network/privilege false negatives.

## 4. State contract extension

M3.2 adds one additive `network` object to InternalRootState and PublicState.

The existing V1 schema version remains `1`; this is an additive V1 Monitoring MVP field, not an incompatible semantic rewrite. All current in-repo consumers are updated atomically with the schema addition.

Conceptual internal/public value shape:

```json
"network": {
  "quality": "good",
  "reachable": true,
  "connectLatencyMs": 42.7,
  "probeFailurePercent": 0.0,
  "receiveBytesPerSecond": 1258291.2,
  "sendBytesPerSecond": 307201.8
}
```

### 4.1 NetworkQuality

Frozen values:

```text
unknown
good
degraded
offline
```

This is network-condition state, not SourceHealth.

### 4.2 Nullability

The following are nullable:

- `reachable` before a completed probe observation exists;
- `connectLatencyMs` when the current probe did not connect;
- `probeFailurePercent` before any probe has completed;
- receive/send rate until a valid counter delta can be calculated.

Unknown/unavailable values are `null`, never fabricated as zero.

Legitimate measured zero receive/send rate is `0`.

Legitimate zero recent probe failure is `0`.

## 5. Probe target configuration

M3.2 adds a small scalar config section compatible with the existing config parser:

```yaml
network:
  probe_address: "1.1.1.1:443"
  probe_timeout_milliseconds: 1500
```

Defaults:

```text
probe_address = 1.1.1.1:443
probe_timeout_milliseconds = 1500
```

Rules:

- `probe_address` must be a valid `host:port`-style endpoint acceptable to the TCP dialer;
- timeout must be positive and bounded;
- no URL, shell command, arbitrary probe script, credentials, headers, or payload are accepted;
- the configured target is server-owned configuration and is not exposed in PublicState/Kindle.

The target is overridable because network environments differ.

## 6. Probe schedule

Default Network Health sampling interval:

```text
5 seconds
```

Each cycle performs at most one bounded TCP connection attempt.

The probe timeout defaults to 1500 ms, therefore normal cycles do not overlap.

Collection is serialized in one Network Health runtime goroutine.

There is no per-request collection and no goroutine fan-out per probe.

## 7. Startup behavior

Network probing must not add its worst-case timeout to the HTTP server startup critical path.

The Network Health runtime therefore starts asynchronously and performs its first sample immediately in its own loop.

Before the first completed sample:

```text
quality = unknown
reachable = null
connectLatencyMs = null
probeFailurePercent = null
receiveBytesPerSecond = null
sendBytesPerSecond = null
```

The rest of DevBoard remains available while the first network sample is in progress.

## 8. TCP reachability semantics

One probe uses a context-bounded direct TCP connection to the configured address.

### Success

If the connection is established within the deadline:

```text
reachable = true
connectLatencyMs = elapsed connect duration
```

The connection is immediately closed. No application payload is sent.

### Failure

A bounded connection failure/timeout/no-route/name-resolution failure is an observed reachability failure:

```text
reachable = false
connectLatencyMs = null
```

It is not automatically a collector SourceHealth failure.

Context cancellation due normal DevBoard shutdown is not recorded as a network-quality observation.

## 9. Probe failure window

M3.2 retains only a bounded in-memory window of recent probe outcomes.

Frozen window:

```text
12 completed probe attempts
```

At the default 5-second interval this represents approximately one minute.

`probeFailurePercent` is:

```text
failed attempts / completed attempts × 100
```

within the current bounded window.

This metric is explicitly **TCP probe failure percentage**, not ICMP/IP packet loss.

The rolling window is runtime state only and does not need persistence in M3.2. It resets on DevBoard restart.

## 10. Network quality derivation

Quality is derived from current/recent probe observations.

Frozen MVP thresholds:

```text
offline:
  3 consecutive completed probe failures

degraded:
  at least one recent probe exists and not offline, AND any of:
  - latest probe failed
  - rolling probeFailurePercent > 10
  - latest successful connectLatencyMs > 500

good:
  latest probe succeeded
  AND rolling probeFailurePercent <= 10
  AND connectLatencyMs <= 500

unknown:
  no completed probe observation exists
  OR the probe subsystem cannot produce a valid observation
```

The 500 ms threshold intentionally detects severe latency rather than labeling normal long-distance AI traffic as degraded.

These are MVP defaults, not a universal networking standard.

## 11. Traffic source semantics

Traffic rate uses per-interface cumulative byte counters from gopsutil:

```text
BytesRecv
BytesSent
```

M3.2 does not sum every interface.

### Why

VPN/TUN/virtual interfaces can represent the same logical traffic at multiple layers. Naive summation can double-count.

### Selected route interface

When a TCP probe succeeds, the connection's local address identifies the local IP chosen by the OS route.

DevBoard maps that local IP to an OS network interface and uses the matching gopsutil per-interface counters.

The public state does not expose:

- interface name;
- local IP;
- hardware/MAC address;
- interface inventory.

These are implementation facts only.

## 12. Traffic rate calculation

For one selected interface:

```text
receiveBytesPerSecond = (recvNow - recvPrevious) / elapsedSeconds
sendBytesPerSecond    = (sentNow - sentPrevious) / elapsedSeconds
```

Use real elapsed monotonic/wall duration between accepted baselines; do not assume exactly 5 seconds.

### First sample

The first successful counter read establishes a baseline.

Rates remain `null` until a second compatible sample is available.

This is not a source failure.

### Interface/route change

If the selected route interface changes, reset the baseline and report rate `null` until the next compatible sample.

### Counter reset/decrease

If a cumulative counter decreases:

- do not underflow;
- reset the baseline;
- report the affected rate as `null` for that sample.

### No reliable route interface

If a route interface cannot be established reliably, traffic rates are `null` rather than a sum of all interfaces.

## 13. SourceHealth separation

M3.2 creates/uses:

```text
sources["network"]
```

It does not reuse or degrade `sources["system"]`.

This preserves M0's SourceHealth isolation.

### Important distinction

```text
NetworkState.quality / reachable
  = condition of the measured network path

sources.network
  = health/capability of the collector
```

Therefore:

```text
reachable = false
quality = offline
sources.network.status = available
```

is valid and expected: the collector successfully measured an unavailable network path.

### SourceHealth success

A completed probe observation is valid whether reachable is true or false.

A successful interface-counter read is also a valid collection component even when it only establishes a first baseline.

### Source health reduction

For supported live macOS operation:

- valid probe observation + valid counter collection → `available`;
- one collection component usable and one collector component unavailable → `degraded`;
- no valid probe observation and no valid counter collection → `unavailable`.

`lastAttemptAt` advances each cycle.

`lastSuccessAt` advances when the complete collector sample is healthy, independent of whether the measured route is reachable.

Public messages remain short/sanitized.

Raw dial/library/OS errors remain local diagnostic logs only.

## 14. Collector/runtime package boundary

M3.2 should use a separate package:

```text
internal/networkmetrics/
```

Recommended responsibilities:

```text
backend.go
  interface counters abstraction

probe.go
  bounded TCP probe abstraction

collector.go
  rolling probe window
  quality derivation
  route-interface selection
  traffic rate calculation
  state/source-health reduction

runtime.go
  one asynchronous periodic lifecycle
```

Exact filenames are not authority.

Network collection must not be implemented in web handlers, state models, or `cmd/devboard/main.go` beyond startup wiring.

## 15. Atomic State integration

Sampling work occurs outside the Store lock.

One completed network cycle performs one `Store.Update` that changes only:

- current `Network` state;
- `sources["network"]`;
- root `GeneratedAt`.

The update mutates the latest locked state, preserving concurrent:

- M2 Agent reducer updates;
- M3.1 System metrics updates.

Network collection must never replace unrelated root state from an older snapshot.

## 16. Live and mock behavior

### Live

Start one Network Health runtime in normal `serve` mode.

### Mock

Mock mode starts no live network collector.

Mock state receives deterministic synthetic Network Health values so display/API tests remain stable and do not make outbound connections.

## 17. Public projection/privacy

PublicState explicitly projects only:

```text
quality
reachable
connectLatencyMs
probeFailurePercent
receiveBytesPerSecond
sendBytesPerSecond
```

M3.2 MUST NOT expose:

- probe address;
- connection error string;
- DNS result;
- interface name;
- local IP;
- MAC address;
- network route details;
- proxy configuration;
- environment variables;
- socket details.

## 18. Display scope for M3.2

### `/display`

M3.2 may add a compact Network Health section/row with values such as:

```text
NETWORK  GOOD · 43 ms · FAIL 0% · ↓1.2 MiB/s · ↑0.3 MiB/s
```

Labels must not call TCP probe failure raw packet loss.

### `/display/kindle`

M3.2 does **not** modify the frozen M2.3 Kindle System bar.

Kindle network presentation is deferred to M8 Monitoring MVP Display Closure, where the presentation contract may be explicitly revised with the full MVP information hierarchy.

M3.2 must prove Kindle has no regression.

## 19. Error and stale-value policy

Network measurements represent the current sample.

Do not retain stale current values as if they were fresh when the current measurement cannot establish them.

Examples:

- current TCP failure → latency null, not previous latency;
- interface route changes → rates null until new baseline;
- counter read fails → rates null;
- source messages do not include raw errors.

Rolling `probeFailurePercent` legitimately includes prior recent outcomes because it is explicitly a bounded history metric.

## 20. Mandatory deterministic tests

M3.2 implementation must cover at least:

1. valid default config;
2. invalid probe address rejected;
3. invalid timeout rejected;
4. successful TCP probe → reachable true + latency;
5. failed TCP probe → reachable false + latency null;
6. shutdown cancellation does not create false failure observation;
7. rolling 12-sample failure percentage;
8. three consecutive failures → offline;
9. one transient failure → degraded, not immediately offline;
10. high latency >500ms → degraded;
11. healthy latency/loss → good;
12. first traffic counter sample establishes baseline with null rates;
13. second sample computes Rx/Tx using actual elapsed duration;
14. legitimate zero traffic → zero rates;
15. counter decrease/reset → null rate + baseline reset;
16. route/interface change → baseline reset;
17. no reliable route interface → rate null, no aggregate-all fallback;
18. probe offline is not SourceHealth failure;
19. counter collector failure can degrade network SourceHealth without affecting system SourceHealth;
20. complete healthy collection advances network LastSuccessAt;
21. concurrent Agent update preserved;
22. concurrent System update preserved;
23. mock starts no outbound network runtime;
24. PublicState exposes only allow-listed network fields;
25. probe target/error/interface/IP never leak;
26. `/display` network rendering;
27. Kindle M2.3 presentation remains unchanged.

## 21. Full validation

Before M3.2 closes:

```text
gofmt
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
```

Real Mac validation must additionally confirm:

- first network sample appears without delaying server startup materially;
- configured default probe works in the validation environment or a documented override is used;
- live connect latency is plausible/non-negative;
- rolling probe failure behaves over repeated cycles;
- route-interface traffic rates become numeric after baseline;
- generating network traffic changes the relevant rate;
- temporary unreachable-target test reaches degraded/offline semantics without degrading `sources.system`;
- restoring target/network returns quality to healthy state;
- Agent lifecycle remains intact while network/system collectors run concurrently;
- `/api/state` and `/display` contain no target/interface/IP/raw error leakage;
- `/display/kindle` remains the frozen M2.3 layout;
- shutdown leaves no network goroutine/process behind.

## 22. M3.2 boundary

M3.2 closes only Network Health.

Do not start M4 AI Task Observability in the same implementation branch.

Do not revive Process Groups.

Do not add multi-host transport.

Do not add browser monitoring, quota, or control.

## 23. Freeze conclusion

M3.2 is frozen as:

```text
one independent local network source
→ bounded direct TCP probe every 5s
→ current reachability
→ current TCP connect latency
→ rolling 12-probe failure percentage
→ good/degraded/offline condition
+
existing gopsutil per-interface byte counters
→ route-selected interface
→ honest Rx/Tx bytes/sec
+
independent sources.network
→ explicit PublicState projection
→ desktop read-only display
→ Kindle unchanged until M8
```

No new dependency.

No Glances daemon.

No ICMP privilege setup.

No Process Groups.

**M3.2 NETWORK HEALTH V1 TECHNICAL CONTRACT FROZEN.**
