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

`cmd/boyan serve` loads operator config through `internal/standalonecfg` and
then runs the public `pkg/boyan` host path. `internal/standaloneruntime`
remains the private engine behind that host.

## Layer model

The repository is organized as one standalone service plus an explicit public
Go contract. The architecture is defined by four layers, not by every
`internal/*` package.

Layers define dependency direction and ownership. Usage surfaces define
audience and stability. One layer may participate in multiple usage surfaces,
and usage surfaces are documented here rather than encoded as lint components.

### Public contracts

The public-contract layer contains the exported reusable contract and
consumer-facing helpers:

- `pkg/memory`
- `client/http`
- `boyantest`

This layer owns the reusable memory types, the typed HTTP client, and the test
helpers intended for external consumers of the module.

### Memory engine

The memory engine owns Boyan's internal semantics and orchestration:

- semantic processing in `internal/canonicalcore` and `internal/boundarycore`
- application orchestration in `internal/ingestapp`, `internal/memoryread`,
  `internal/memoryops`, and `internal/portableapp`
- ordered ingest and projection coordination in `internal/orderedingest`,
  `internal/scopelanes`, `internal/projection`, and
  `internal/projectionsync`
- boundary snapshot shaping stays inside `internal/boundarycore`
- derivation contracts stay in `internal/structuredderive`

Packages inside this layer may be small, but they are implementation modules,
not separate architectural peers.

### Infrastructure

The infrastructure layer owns concrete adapters to persistence, projection
storage, and runtime-backed derivation:

- `internal/canonicalops`
- `internal/forgetops`
- `internal/derivation`
- `store/badger`
- `index/bleve`

This layer binds the memory engine to Badger, Bleve, and provider-backed
derivation runtimes.

### Standalone shell

The standalone shell owns process concerns, transport, and composition:

- `pkg/boyan`
- `internal/standalonecfg`
- `internal/httpingest`
- `internal/standaloneruntime`
- `mcp`
- `cmd/boyan`

This is the only layer that composes concrete adapters into the runnable
service. `internal/standaloneruntime` is a process shell and composition root,
not a domain or application layer.

## Usage surfaces

### Public Go Contract

Boyan exposes two public Go surfaces:

- `pkg/boyan` for host embedding and lifecycle
- `pkg/memory` for transport-neutral contracts and validation

Audience:

- other Go code that embeds Boyan in-process
- other Go code that needs Boyan wire types
- validation and semantic contracts shared across the shipped surfaces

`pkg/boyan` is the standalone-shell public host API. `pkg/memory` stays
focused on exported contracts and validation helpers and is not a host
lifecycle API.

### HTTP/OpenAPI

The deterministic remote wire surface is the standalone HTTP API.

Audience:

- services
- automation
- typed clients
- non-MCP integrations

Current endpoints:

- `POST /v1/ingest/turns`
- `POST /v1/ingest/boundaries`
- `GET /livez`
- `GET /readyz`

HTTP requests are validated strictly and routed through the sequenced ingest
runtime so same-scope ordering stays deterministic.

The machine-readable contract for this surface lives in `docs/openapi.yaml`.

`client/http` is the typed client for this surface, not a separate product
surface.

This surface is intentionally narrow. Boyan does not expand it into a broader
admin or control plane unless product requirements demand that later.

### MCP

MCP is the agent-tool surface and is served by Streamable HTTP at `/mcp`.

Audience:

- generic agents

The current tool set is:

- `boyan.search`
- `boyan.trace`
- `boyan.forget_source`
- `boyan.forget_scope`

Search and trace return untrusted reference data. Forget operations are exact
scope mutations with explicit target confirmation.

This surface stays narrower and simpler than Boyan's internal application
capabilities and narrower than the HTTP/domain contract.

### CLI

The CLI is the operator surface.

Audience:

- developers
- operators
- human workflows

It provides service start and operational convenience commands. It is a shell
around the standalone runtime and public remote surfaces, not a place for
separate business logic.

### Internal implementation surfaces

The remaining internal packages are implementation surfaces for Boyan
maintainers only:

- `internal/*`
- store adapters
- provider adapters
- standalone composition packages

These are not audience-facing usage surfaces and are not part of Boyan's
public compatibility promise.

## State ownership

Boyan owns one explicit state root:

```text
<state_dir>/
├── badger/
└── bleve/
```

- `badger/` is authoritative canonical state.
- `bleve/` is disposable and rebuildable from canonical records.

One process owns one writable Badger directory. Boyan does not support shared
writer access.

## Canonical and projection boundary

Badger is authoritative. Bleve is never returned directly to callers without
canonical hydration through the recall or trace path.

Clean projection generations resume from their durable watermark. Unchanged
syncs leave the generation and checkpoint untouched; updates apply new changes.
Dirty, missing, incompatible or retention-floor-invalid indexes rebuild from
canonical active state and replay changes committed during the scan. Exact-scope
synchronization serializes ingest, repair and forget-triggered projection work.
Physical retention keeps active and at most one previous generation per scope,
while preserving durable references and in-flight builders. Startup reconciles
committed marker switches before removing abandoned generations.

Native kind is indexed by the disposable v2 schema; physical v1 generations
rebuild from canonical state. Nonzero-minimum recall pins the physical
generation and verifies the clean exact-scope checkpoint before and after the
query. Adapters without this optional consistency capability fail closed.

Boundary views page active metadata and aggregate at most 2048 records,
independently of the model serializer byte limit. Reconciliation scans the same
encoded item-ID order and retains only candidate matches; the scope version
captured before scanning guards the final atomic mutation.

The projection can lag and be repaired, but canonical state remains safe and
authoritative. Readiness checks canonical maintenance diagnostics before
accepting work.

## Scope isolation

Scope keys are exact canonical locators supplied by the host in
`<channel_type>:<address_key>` form, for example `telegram:123:0`. The channel
must be nonempty and lowercase, with no spaces, tabs, carriage returns or line feeds. The address must be
nonempty with no leading or trailing whitespace; additional colons are allowed.
The entire key must have no leading or trailing whitespace. Scope kind is
explicitly `personal` or `group`; the pair of key and kind defines isolation.
Boyan validates this shape without resolving host-specific address semantics.
It does not implement inheritance, fallback, or cross-scope promotion.

All ingest, search, trace, and forget paths stay within one exact scope.

## Derivation boundary

Boyan owns derivation orchestration through `internal/derivation` and
runtime-backed provider configuration.

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

Boyan does not ship migration or historical import as part of its supported
CLI or runtime surface. If that capability is needed later, it belongs in a
separate migration tool outside this repository's primary service boundary.
