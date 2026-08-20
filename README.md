# DevBoard

DevBoard is a local-first development status aggregation and safe-navigation system. The current implementation milestone is **M2 — Agent Event Ingestion + Lifecycle Runtime**.

The frozen V1 authority is [`Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`](Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md).

## M1 foundation

M1 is the first runnable vertical slice and uses **synthetic mock data only**. It includes typed internal/public state, an explicit public projector, an in-memory store, and read-only HTTP displays.

Projection invariant: `PublicState.navigationTargets` is the complete allow-listed public summary of currently exposed trusted targets; entity-level `navigation` objects are convenience references to entries in that list. M1 exposes this metadata only as contract-preview data and does not execute navigation.

M2 preserves that M1 surface and adds bounded, sanitized Codex and Claude Code lifecycle ingestion through a local Unix-domain socket, a serialized reducer, source health, and live agent alerts/state. `devboard serve --mock` remains the deterministic M1 synthetic mode; `devboard serve` starts live M2 ingestion.

Not implemented yet: system collectors, Git/GitHub collectors, quota collectors, safe-navigation runtime, macOS focus, persistence, or production service management.

## Build and test

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
```

## Run live M2 mode

Defaults bind to localhost only:

```bash
go run ./cmd/devboard serve
```

Or build first:

```bash
go build -o devboard ./cmd/devboard
./devboard serve
```

Live mode starts with no fake agents. It listens for sanitized lifecycle events on `<user-cache-dir>/devboard/activity.sock`; the runtime directory is mode `0700` and the socket is mode `0600`.

Provider helpers are fail-open and intentionally write zero bytes to stdout:

```bash
./devboard agent-hook codex
./devboard agent-hook claude-code
```

Manual provider hook setup is documented in [`Docs/M2_Agent_Hook_Setup_2026-08-20.md`](Docs/M2_Agent_Hook_Setup_2026-08-20.md).

## Run mock mode

Defaults bind to localhost only:

```bash
go run ./cmd/devboard serve --mock
```

Or build first:

```bash
go build -o devboard ./cmd/devboard
./devboard serve --mock
```

Read endpoints:

- `http://127.0.0.1:8787/health`
- `http://127.0.0.1:8787/api/state`
- `http://127.0.0.1:8787/display`
- `http://127.0.0.1:8787/display/kindle`

A config file is optional:

```bash
./devboard serve --config ./config.example.yaml --mock
```

## Explicit LAN / Kindle test mode

The default remains `127.0.0.1`. To expose the **status-only M2 display** on the local LAN, copy `config.example.yaml` and explicitly set:

```yaml
server:
  host: "0.0.0.0"
  port: 8787
```

Then a Kindle on the same LAN can open:

```text
http://<MAC-LAN-IP>:8787/display/kindle
```

Optional explicit layout modes:

```text
http://<MAC-LAN-IP>:8787/display/kindle?layout=portrait
http://<MAC-LAN-IP>:8787/display/kindle?layout=landscape
```

M2 does **not** implement authentication or navigation actions. `safeNavigationEnabled` remains intentionally `false` until the later safe-navigation runtime milestone.
