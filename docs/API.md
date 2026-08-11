# API

Boyan exposes four official usage surfaces:

- an in-process host API in `pkg/boyan`
- a deterministic HTTP surface for ingest and health probes
- a narrower MCP surface for agent memory operations
- an operator CLI in `cmd/boyan`

## Host API

`pkg/boyan` is the public in-process embedding surface.

It exports:

- `Config`, `DefaultConfig()`, and `Config.Validate()`
- `New(ctx, config)` and `NewHost(ctx, config)`
- lifecycle methods: `Start`, `Run`, `Wait`, `Stop`, `Close`
- runtime state and capability access: `Ready`, `Addr`, `Handler`, `MCP`, `Search`, `Trace`, `ForgetSource`, `ForgetScope`

The host API keeps transport wiring behind host methods and does not require
`internal/*` imports by embedders.

The machine-readable contract for the deterministic HTTP surface lives at
[`docs/openapi.yaml`](openapi.yaml).

## Listener

`boyan serve` binds one loopback HTTP listener. The default address is
`127.0.0.1:8686`.

## Health endpoints

### `GET /livez`

Returns `200 OK` with `ok` when the process event loop is responsive.

### `GET /readyz`

Returns `200 OK` with `ok` when Boyan is ready to accept work.

Readiness currently checks:

- the standalone runtime has no current readiness diagnostic

## Ingest endpoints

### `POST /v1/ingest/turns`

Accepts a strict JSON envelope:

```json
{
  "schema_version": "boyan-ingest/v1",
  "producer_id": "producer-1",
  "scope_sequence": 1,
  "payload": {
    "schema_version": "boyan/v1"
  }
}
```

The payload is a validated `boyan.Turn`.

### `POST /v1/ingest/boundaries`

Accepts the same envelope shape, but the payload is a validated
`boyan.Boundary`.

## Ingest behavior

- requests must use `Content-Type: application/json`
- unknown JSON fields fail closed
- request size is bounded
- request processing is time-bounded
- successful outcomes are idempotent by producer, scope, sequence, and payload
- same-scope ordering is enforced by the sequenced ingest runtime

## MCP endpoint

### `POST /mcp`

Boyan serves MCP over Streamable HTTP at `/mcp`.

Current tool set:

- `boyan.search`
- `boyan.trace`
- `boyan.forget_source`
- `boyan.forget_scope`

### Tool notes

- search and trace return untrusted reference data
- forget operations require explicit exact scope input
- scope forget also requires `confirm_scope_key`
- forgotten timestamps are assigned server-side

## Safe defaults

- loopback listener only
- no tenant or account layer
- no implicit scope resolution inside Boyan
- no automatic execution of recalled text
- no framework-specific wrapper in the current product surface
