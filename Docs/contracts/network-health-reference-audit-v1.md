# DevBoard M3.2 — Network Health Reference Audit V1

> Date: 2026-08-22  
> Base: `codex/mvp-monitoring-business-contract-freeze` @ `e256ff56b01016086bb6294df58757b475c7c0e8`  
> Status: **REFERENCE AUDIT CLOSED**  
> Parent policy: `Docs/contracts/reference-first-integration-v1.md`

## 1. Business requirement being solved

M3.2 must add lightweight per-host network diagnosis for AI development work:

- Internet/reachability signal;
- latency/response-time signal;
- honest bounded loss/failure indication;
- current Rx/Tx load where reliably available;
- freshness/source health.

M3.2 is not a full network-monitoring product, speed-test service, packet sniffer, or provider-specific API health checker.

## 2. Audit decision summary

| Reference | Revision | Role | Decision |
|---|---|---|---|
| `shirou/gopsutil` | `v4.25.9` | interface I/O counters | **USE DIRECTLY** — already a DevBoard dependency |
| `nicolargo/glances` | `3bda428beca0f62993f7c1b79f2e886ea8334678` | mature interface filtering/rate semantics | **BEHAVIORAL REFERENCE** |
| `prometheus/blackbox_exporter` | `d43d39cc5925a43ec868ffc266063734eea8c627` | bounded black-box TCP probe pattern | **BEHAVIORAL REFERENCE** |
| `prometheus-community/pro-bing` | `v0.7.0` | ICMP RTT/packet loss candidate | **DEFER / NOT REQUIRED FOR MVP** |
| Go standard `net` package | repository Go toolchain | TCP dial, route/local-address observation | **USE DIRECTLY** |

No new runtime daemon is selected.

No Python runtime is selected.

No ICMP privilege/capability setup is selected for M3.2 MVP.

## 3. gopsutil v4.25.9

DevBoard already pins:

```text
github.com/shirou/gopsutil/v4 v4.25.9
```

and the repository Go contract remains:

```text
go 1.23.0
```

### Proven useful behavior

`gopsutil/net.IOCountersStat` provides per-interface cumulative:

- bytes sent;
- bytes received;
- packets sent/received;
- input/output errors;
- drop counters where the OS exposes them.

M3.2 only needs cumulative bytes for rate calculation.

### Darwin implementation note

At v4.25.9, Darwin `IOCountersWithContext` obtains interface counters through the local `netstat` command and may use `ifconfig -l` when interface-name truncation needs resolving.

This remains acceptable because:

- gopsutil is already the audited embedded metrics dependency;
- it executes fixed OS tools without shell-string construction;
- calls honor context through the library invoker;
- M3.2 does not need a new daemon or interpreter.

Real-Mac validation must confirm these calls remain healthy on the supported macOS version.

### License

BSD-style license. Compatible for direct dependency use under the existing project approach.

### Decision

**USE DIRECTLY.**

Use per-interface cumulative counters and calculate DevBoard's own bounded bytes-per-second deltas.

Do not expose cumulative counters or raw interface inventory publicly.

## 4. Glances

Audited revision:

```text
nicolargo/glances
3bda428beca0f62993f7c1b79f2e886ea8334678
```

### Proven useful behavior

The Glances network plugin:

- reads per-interface cumulative byte counters;
- keeps per-second receive/send rates derived from repeated observations;
- filters interfaces that are down when configured;
- filters interfaces without usable addresses when configured;
- treats interface speed as optional because some OSes return zero;
- keeps network monitoring as a separate plugin/domain.

These are good operational semantics for DevBoard.

### Why Glances is not imported

Glances is a Python application/library and can also run server/web modes. Making it a local DevBoard requirement would add a Python/runtime/daemon dependency that the current embedded Go architecture does not need.

Its network plugin is LGPL-3.0-only. DevBoard does not need to copy its implementation source to gain the proven design lessons above.

### Decision

**BEHAVIORAL REFERENCE ONLY.**

Reuse the ideas:

```text
cumulative interface counters
→ repeated observations
→ per-second rate
→ ignore unusable interface states
```

Do not copy the plugin and do not require a Glances daemon for local M3.2.

Glances remains a valid future remote/NAS/VPS adapter reference.

## 5. Prometheus blackbox_exporter

Audited revision:

```text
prometheus/blackbox_exporter
d43d39cc5925a43ec868ffc266063734eea8c627
```

### Proven useful behavior

Blackbox exporter models endpoint reachability as a bounded probe with:

- explicit protocol;
- explicit target;
- explicit timeout;
- `probe_success`;
- probe duration/timing;
- independent HTTP/TCP/ICMP/DNS modes.

Its TCP prober uses a context-bounded dial and treats successful connection as a meaningful black-box reachability result.

### Decision

**BEHAVIORAL REFERENCE ONLY.**

DevBoard needs only a very small subset of this pattern:

```text
configured target
→ context-bounded TCP connect
→ success/failure observation
→ connect duration on success
```

Do not import the exporter, Prometheus registry stack, YAML module system, or its daemon/runtime.

## 6. pro-bing

Candidate:

```text
prometheus-community/pro-bing v0.7.0
```

### Positive findings

- mature Prometheus-community maintained ICMP probing library;
- returns sent/received counts, packet-loss percentage, and RTT statistics;
- `v0.7.0` declares `go 1.23.0`, matching DevBoard's project contract;
- MIT license.

Current upstream has moved further: current main declares Go 1.25, so blindly taking latest would violate DevBoard's current toolchain contract.

### Why it is not selected for M3.2 MVP

ICMP packet loss is attractive but creates product ambiguity:

- ICMP may be filtered even while TCP/HTTPS AI development traffic is functioning;
- privilege/socket behavior differs by platform/environment;
- a failed ICMP test can therefore become a false "Internet unavailable" signal;
- M3.2's business contract explicitly allows an equivalent bounded reachability-loss indication as long as semantics are honest.

The MVP does not need kernel-level packet loss badly enough to justify this additional failure mode.

### Decision

**DEFER / NOT REQUIRED FOR MVP.**

If true ICMP RTT/loss becomes a later requirement, re-audit pro-bing at that time. `v0.7.0` is the known Go-1.23-compatible reference baseline found in this audit.

## 7. Selected M3.2 route

The shortest proven route is:

```text
Reachability / response time
  = bounded direct TCP connect using Go standard net.Dialer

Bounded loss indication
  = rolling percentage of failed TCP probe attempts
  = explicitly named probe failure/loss, not IP packet loss

Traffic load
  = existing gopsutil v4.25.9 per-interface cumulative bytes
  → repeated sample delta
  → bytes/sec

Interface attribution
  = use the local address/route selected for the probe
  → map that local IP to an OS interface
  → avoid summing all interfaces and double-counting VPN/TUN traffic
```

## 8. Why aggregate-all-interface traffic is rejected

A Mac may simultaneously expose:

- Wi-Fi/Ethernet;
- loopback;
- `utun*` VPN/TUN interfaces;
- Tailscale/tunnel adapters;
- transient virtual interfaces.

Summing every interface can count the same logical traffic at multiple layers.

M3.2 therefore does not publish a naive sum of all interface counters.

It attempts to report the interface selected for the configured probe route. If that interface cannot be established reliably, Rx/Tx rate is `null` for that sample rather than a fabricated aggregate.

## 9. Probe semantics

The probe is a **direct TCP reachability probe**, not an AI-provider API request and not an ICMP echo test.

The public state must therefore use names such as:

- `connectLatencyMs`;
- `probeFailurePercent`;
- `reachable`.

It must not label the rolling TCP failure rate as raw IP `packetLossPercent`.

The probe target is server-owned configuration and is not itself required in PublicState.

## 10. Reference intake during implementation

At M3.2 implementation start, the remote coding assistant should re-fetch or clone the selected references into an external temporary audit location and pin the revisions above before modifying DevBoard.

Minimum source paths to re-open:

```text
shirou/gopsutil@v4.25.9/net/net.go
shirou/gopsutil@v4.25.9/net/net_darwin.go
nicolargo/glances@3bda428.../glances/plugins/network/__init__.py
prometheus/blackbox_exporter@d43d39c.../prober/tcp.go
prometheus-community/pro-bing@v0.7.0/README.md
prometheus-community/pro-bing@v0.7.0/go.mod
```

The reference checkouts remain outside the DevBoard repository.

## 11. Closed reference decision

M3.2 does not need a new monitoring framework.

It will:

- reuse the existing gopsutil dependency for traffic counters;
- reuse Go's standard networking primitives for the probe;
- borrow Glances rate/filtering semantics;
- borrow blackbox_exporter bounded-probe semantics;
- defer ICMP/pro-bing unless a later requirement justifies true packet-loss measurement.

**M3.2 NETWORK HEALTH REFERENCE AUDIT CLOSED.**
