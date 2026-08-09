# Architecture

Boyan is a standalone local service for grounded temporal memory. It separates
canonical truth, disposable search state, ingest transport, and memory
operations.

## Composition

`cmd/boyan serve` composes one process with these runtime-owned pieces:

- Badger canonical store
- canonical maintenance worker
- canonical reader
- Bleve projection and projection runtime
- isolated derivation invoker
- turn processor
- boundary processor
- sequenced ingest runtime
- recall, trace, and forget services
- HTTP ingest handler
- Streamable HTTP MCP handler

The process is started and stopped through a small Cobra/Viper/Fx composition
root in `cmd/boyan`.

## External surfaces

### HTTP ingest

HTTP ingest is the write surface:

- `POST /v1/ingest/turns`
- `POST /v1/ingest/boundaries`
- `GET /livez`
- `GET /readyz`

HTTP requests are validated strictly and routed through the sequenced ingest
runtime so same-scope ordering stays deterministic.

### MCP

MCP is the memory operation surface and is served by Streamable HTTP at `/mcp`.

The current tool set is:

- `boyan.search`
- `boyan.trace`
- `boyan.forget_source`
- `boyan.forget_scope`

Search and trace return untrusted reference data. Forget operations are exact
scope mutations with explicit target confirmation.

## State ownership

Boyan owns one explicit state root:

```text
<state_dir>/
├── badger/
├── bleve/
└── migration/
```

- `badger/` is authoritative canonical state.
- `bleve/` is disposable and rebuildable from canonical records.
- `migration/` stores handoff receipts and logical export artifacts used during
  Balda state transfer.

One process owns one writable Badger directory. Boyan does not support shared
writer access.

## Canonical and projection boundary

Badger is authoritative. Bleve is never returned directly to callers without
canonical hydration through the recall or trace path.

The projection can lag and be repaired, but canonical state remains safe and
authoritative. Readiness checks migration state and canonical maintenance
diagnostics before accepting work.

## Scope isolation

Scope keys are opaque exact identifiers supplied by the host. Boyan does not
parse Balda locators and does not implement inheritance, fallback, or
cross-scope promotion.

All ingest, search, trace, and forget paths stay within one exact scope.

## Derivation boundary

Boyan owns derivation orchestration through `internal/derivation` and
runtime/v2-backed provider configuration.

The model runtime does not choose:

- scope
- durable IDs
- authoritative timestamps
- lifecycle state
- provenance authority outside validated evidence

## Standalone safety defaults

- default listener: `127.0.0.1:8686`
- non-loopback listeners are rejected
- request bodies and timeouts are bounded
- shutdown drains through ordered lifecycle closure
- logs are structured and intended to stay content-safe

## Migration boundary

`boyan migrate balda` is an offline handoff from current Balda canonical state
into an empty Boyan state root. It is restart-safe, verifies integrity, and
rebuilds Bleve before the target is considered ready to serve.
