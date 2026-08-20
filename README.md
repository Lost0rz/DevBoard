# DevBoard

DevBoard is a local-first development status aggregation and safe-navigation system. The current implementation milestone is **M1 — Core + State + Mock Display**.

The frozen V1 authority is [`Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`](Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md).

## M1 scope

M1 is the first runnable vertical slice and uses **synthetic mock data only**. It includes typed internal/public state, an explicit public projector, an in-memory store, and read-only HTTP displays.

Projection invariant: `PublicState.navigationTargets` is the complete allow-listed public summary of currently exposed trusted targets; entity-level `navigation` objects are convenience references to entries in that list. M1 exposes this metadata only as contract-preview data and does not execute navigation.

Not implemented yet: agent hooks, event ingestion/reducers, system collectors, Git/GitHub collectors, quota collectors, safe-navigation runtime, macOS focus, persistence, or production service management.

## Build and test

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/devboard
```

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

The default remains `127.0.0.1`. To expose the **status-only M1 display** on the local LAN, copy `config.example.yaml` and explicitly set:

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

M1 does **not** implement authentication or navigation actions. `safeNavigationEnabled` is intentionally `false` until the later safe-navigation runtime milestone.
