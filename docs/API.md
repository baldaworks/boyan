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

## Canonical turn replay

Turn identity is independent of the producer delivery fingerprint. Before a
model call or replaying a saved TURN receipt, Boyan verifies a versioned content
certificate covering the complete normalized Turn: exact scope, conversation
and lineage fields, timestamps, status, ordered messages and tool fields.
Generated message/source IDs, an omitted success status and equivalent UTC
instants normalize consistently. Reusing a canonical turn identity with changed
content returns `conflict`, even with an unchanged delivery fingerprint.

If canonical commit succeeded but receipt persistence failed, retrying the same
verified turn completes the receipt without invoking the model again. This also
works after reopening storage and when the model is unavailable. A certified
forgotten turn can replay an existing canonical operation; it cannot create a
new derivation or restore erased content.

Old data requires verifiable original source/message evidence. Missing or
ambiguous evidence returns an explicit `conflict` before model execution or
mutation. Supported uniquely owned native legacy records retain their original
payload references. Native legacy tool metadata alone cannot prove the order of
multiple tools; full original Turn evidence is required in that case. The older
ExportID-keyed source namespace is rejected when its original identity cannot
be certified safely.

## Structured derivation output

All four provider adapters require exactly one JSON object with the required
`output` member. Missing output, unknown members, duplicate keys at any depth,
a trailing JSON value, wrong case, wrong types, unsupported enums and required
nulls return a permanent, redacted `invalid_derived` error. Runtime validation
and provider schemas describe the same fixed shapes; Go's permissive field-name
matching does not apply to model replies.

| Stage | Output and required candidate members |
| --- | --- |
| Atoms | Array; `category`, `text`, `relation`. Categories: `fact`, `preference`, `constraint`, `decision`, `event`. Relation `new` permits absent/null `target`; `coexist` and `supersede` require `target`. |
| Canonical semantics | Array; outer `Kind`, `Subject`, `Predicate`, `Memory`; optional nullable `Qualifiers`. `Kind` is `state` or `event`. These outer names retain their exported PascalCase spelling. |
| Scenarios | Array; `topic_key`, `title`, `summary`, `atoms`; optional nullable `supersedes`. |
| Profile | Object; `disposition` is `skip` or `upsert`. Upsert requires a nonempty `summary` and a nonempty `atoms` or `scenarios` array; optional nullable `supersedes`. |

The semantic `Memory` object uses the existing snake_case names: required
`kind`, `statement`, `temporal`, `evidence`, `sensitivity`, `retention`.
Sensitivity is `standard` or `sensitive`; retention is `standard` or `ephemeral`.
`Qualifiers` is an array of strings, and `Memory.kind` is `state` or `event`.
`evidence` and scenario `atoms` are arrays of nonnull evidence and reference
objects respectively. A reference requires `item_id` and `revision_id`. Temporal metadata requires
`observed_at`; optional nullable members are `valid_from`, `valid_until`,
`event_at`, `expires_at`, `temporal_text`, `temporal_precision`, `timezone`.
`valid_from`, `valid_until`, `event_at` and `expires_at` are timestamps; the
other optional temporal members are strings. Timestamps use uppercase RFC3339
syntax. Evidence requires `source_id`,
`message_id`, `role`, `start_byte`, `end_byte`, `assertion_mode`; optional
`text_digest` is nullable. Roles are `user`, `assistant`, `tool`; assertion modes
are `user`, `trusted_tool`. Byte offsets are mathematical integers in the uint32
range. Existing semantic/evidence validation still applies after shape checks.

For array stages, `{"output":[]}` is a successful empty result; object, scalar,
null output and null array elements fail. Profile `{"output":{"disposition":"skip"}}`
succeeds without a revision. Skip permits only absent/null/empty optional parent
arrays and summary, and absent/null `supersedes`; it cannot hide an upsert.
Optional nullable members accept omission without requiring explicit null.

Malformed output creates no successful operation for that failed stage. A
corrected retry uses the same operation identity. Scenario success remains
durable if profile fails; an identical retry runs only unfinished profile work.

## Boundary synthesis and replay

Each attempt captures one UTC processing time T, independently of boundary
`occurred_at`. Eligibility requires `observed_at <= T`, `valid_from <= T` when
present, and `T < valid_until` / `T < expires_at` when present. Comparisons for
both model stages use that same T; an unfinished retry captures a new T.
`event_at` describes an event, including a historical or planned event, and
alone never makes a revision ineligible. Temporal qualifiers remain in model
input. The current-head contract does not reconstruct historical state.

Model input contains only eligible active revisions. Separate bounded structural
head metadata preserves revision numbering and supersession even when a head is
ineligible. A result inherits the intersection of its actual semantic parents:
latest valid-from, earliest valid-until and earliest expiry. Structural
supersession and unrelated records do not add lifetime constraints. Scenario to
profile synthesis cannot refresh a parent's expiry. Sensitivity and retention
remain conservative: any sensitive parent makes the result sensitive, and any
ephemeral parent makes it ephemeral. Untrusted legacy ancestry is
resolved through exact reads with independent node, depth and payload-byte
budgets. Excessive proof returns permanent `limit_exceeded`; missing, cyclic
or temporally unavailable proof returns `invalid_derived` rather than inventing
unrestricted knowledge.

The first successful scenario or profile stage atomically binds a content-free
certificate to the complete normalized Boundary in its exact Scope(key, kind)
and ExportID. Equivalent timestamp instants and surrounding transition
whitespace normalize; other structurally valid authoritative changes within
the same exact scope return permanent `conflict` before a model call or
successful replay. Invalid stable identities fail structural validation. Personal and group are independent
scope partitions even when their ExportID strings coincide. Changing producer
sequence, derivation or supplied delivery fingerprint cannot bypass proof.

Completed stage and receipt replay requires an existing matching certificate,
checked after reading the outcome as well as before replay. Portable adapters
without that verification capability fail closed for protected replay. Matching
full retries, including after restart, return original outcomes without new
model calls or mutations. Matching partial retries preserve the scenario and
attempt profile only. Forgetting retains immutable replay evidence and matching
replay recreates no forgotten payload.

Uncertified scopes containing any old derived-operation namespace, including
atom-only history, return permanent `conflict` before model execution or replay.
Clean native-semantic-only old scopes can establish certification on their first
successful boundary transaction using a bounded exact namespace existence
check. Forget/reset cannot manufacture proof. There is no automatic backfill or
repair command; see [upgrade and restore restrictions](OPERATIONS.md#boundary-contract-upgrades).

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

## Forget outcomes and retries

`Host.ForgetScope` and `boyan.forget_scope` process history in fixed storage
pages, including records already denied by an interrupted attempt. Completion
means original selected source, message and revision payloads are absent through
the payload API, and the exact scope's durable erase journal has completed.
Logical denial alone does not report successful cleanup.

Outcomes expose `source_count`, `revision_count` and `references_truncated`.
At most 2048 combined source/revision references are returned. Within that bound,
arrays are exhaustive. Above it, **both** arrays are omitted and
`references_truncated=true`; counts remain exact. For example, 1025 sources and
1025 revisions produce counts of 1025 each with no partial reference arrays.
Zero-valued fields may be omitted in JSON; old stored outcomes without counts
remain decodable. This result bound is separate from the model-input limit.
Source forget retains its one-source outcome contract; excessive revision
references return `limit_exceeded` before denial.

Retry the same scope request identity after cancellation or a retryable failure.
A durable fence rejects new writes to that exact key and kind until recovery
finishes. Completed old-request replay retains its original outcome and leaves
later writes intact. Startup resumes unfinished journals automatically.
Scope-free raw payload maintenance writes are also blocked while an erase is
pending, since those APIs cannot establish a scope.

New native payload IDs include exact scope key and kind. Some old records share
a global payload reference across scopes or sources. Boyan preserves those
foreign records and returns an explicit permanent `conflict` before denial,
releasing rejection locks and the fence; it does not claim a successful scrub or
migrate foreign data. Uniquely owned legacy payloads remain erasable. Old crash
windows without a durable target snapshot cannot recover an unknowable earlier
selection; recovery does not invent that snapshot.

Truncated erases persist a bounded projection rebuild marker. Supported Bleve
synchronization rebuilds from current canonical state, preserving later writes
and removing stale active candidates. Portable projection adapters without
rebuild support return a conflict and retain lag rather than certify that marker
as applied.

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

Boundary synthesis examines structural active heads in bounded pages of at most
256, with a combined limit of 2048 examined heads across atoms, scenarios and
profiles. Temporally ineligible or privacy-filtered records do not cause an
unbounded refill scan; only eligible admitted records enter the model view.
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
