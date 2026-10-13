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

Construction acquires storage resources. Call `Stop` or `Close` even if `Start`
was never called; failed startup also releases owned resources. A stopped host
cannot restart: construct a new host. `Wait` observes cleanup completion for all
concurrent and subsequent callers; canceling one wait does not consume that
result. `Stop(ctx)` stops admission and cancels active operation contexts. If its
deadline expires, it returns a typed `timeout` error while cleanup remains owned
by the host. While the process stays alive, a later `Stop` or `Wait` can join that
cleanup. Storage is released after active HTTP and embedded operations drain.

## Scope contract

Every ingest, recall, trace and forget request supplies an exact key and kind.
Keys use `<channel_type>:<address_key>`, for example `telegram:123:0`. The channel
is nonempty and lowercase, with no spaces, tabs, carriage returns or line feeds.
The address is nonempty and has no
leading or trailing whitespace. Extra colons in the address are allowed. The key
has no leading or trailing whitespace. Kind must be `personal` or `group`; the
same key with a different kind denotes a separate scope.

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

## Recall consistency, time and ranking

`boyan.search` and `Host.Search` operate on current active revisions in one
exact scope. MCP `memory_kind` (Go `RecallRequest.Kind`) selects native
`state` or `event` memories. Candidates are
validated against canonical storage and ranked before applying the requested
result limit. Ranking covers the admitted bounded candidate set (up to 256),
not every possible match in an arbitrarily large scope.

One effective time is used per request: `as_of` when supplied, otherwise the
current time captured at search start. A revision must have been observed by
that time. Optional validity windows are half-open `[valid_from, valid_until)`;
`expires_at` also excludes equality. Absent bounds add no restriction.

`as_of` filters current heads; it does not reconstruct superseded state. For
example, after “I live in Bishkek” at T1 is replaced by “I live in Osh” at T2,
a request between T1 and T2 may return no residence. It cannot return the
future Osh assertion; it does not recover the former Bishkek revision.

With `min_scope_change_seq=N`, successful search requires a physical search
view certified through N by its clean active checkpoint. Canonical sequence or
one returned record's sequence alone is insufficient. During index lag,
rebuild or a concurrent checkpoint change, the request returns a retryable
`conflict`; retry after projection synchronization. A certified empty result
remains empty. Without a nonzero minimum, bounded canonical fallback remains
available. Portable `RecallProjection` adapters must implement the optional
`ConsistentRecallProjection` capability to serve nonzero minimum requests.

## Trace representation

Trace revisions contain exactly one revision arm and no search score. Native
revisions use the additive `canonical` arm with canonical item/revision IDs,
revision number, scope, native kind, state, text, temporal metadata, creation
time, sensitivity, retention, provenance and optional same-item `supersedes`.
The root identity is the identity returned by native search. Existing migrated
revisions retain their `atom`, `scenario` or `profile` representation and strict
legacy identity validation. A closed graph can contain both representations.

Sensitive or forgotten records/sources are unavailable to trace; a failure
returns no partial graph. Recalled text and raw source messages remain
untrusted reference data.

## Boundary model-input limits

Boundary synthesis collects active memory in bounded pages of at most 256,
with a combined model-view limit of 2048 atoms, scenarios and profiles.
Overflow returns `limit_exceeded` rather than silently omitting eligible
records. Turn reconciliation also traverses bounded active pages to locate
state-key or event-evidence matches beyond the first page.

The serialized model-input byte limit is independent: at most 256 KiB by
default. A valid view of 2048
records can exceed this byte limit and fail before invoking the model. The
count limit does not promise that arbitrary content of that size will fit.

## Safe defaults

- loopback listener only
- no tenant or account layer
- no implicit scope resolution inside Boyan
- no automatic execution of recalled text
- no framework-specific wrapper in the current product surface
