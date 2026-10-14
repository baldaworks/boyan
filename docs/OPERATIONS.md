# Operations

Boyan is a local-first service. The current operational contract is one process
owning one local state root on a loopback listener.

## Start the service

```bash
boyan serve --config ./boyan.yaml
```

Or override key paths explicitly:

```bash
boyan serve --config ./boyan.yaml --state-dir /var/lib/boyan --listen 127.0.0.1:8686
```

## Configuration sources

Boyan loads configuration in this order:

1. built-in defaults
2. YAML passed by `--config`
3. `BOYAN_*` environment variables
4. `--state-dir` and `--listen` command flags

See [../boyan.example.yaml](../boyan.example.yaml).

## Current defaults

- `state_dir`: `./.state/boyan`
- `http.listen`: `127.0.0.1:8686`
- `http.request_timeout`: `15s`
- `http.shutdown_timeout`: `10s`
- `http.max_body_bytes`: `1048576`
- `log.level`: `info`
- `maintenance.value_log_gc_interval`: `6h`
- `maintenance.discard_ratio`: `0.5`
- `derivation.timeout`: `30s`
- `derivation.max_output_bytes`: `262144`

## Environment examples

```bash
export BOYAN_STATE_DIR=/srv/boyan
export BOYAN_HTTP_LISTEN=127.0.0.1:8787
export BOYAN_LOG_LEVEL=debug
export BOYAN_DERIVATION_PROVIDER=memory
```

`BOYAN_DERIVATION_PROVIDER` works without a YAML file or a `derivation.provider`
key in YAML. Environment values override YAML values. Nested runtime provider
configuration is usually easiest to manage in YAML.

## HTTP body limits

`http.max_body_bytes` bounds POST bodies on ingest and `/mcp` before decoding or
tool execution. `http.request_timeout` bounds upload time, including stalled
partial bodies. Once the body is fully read, that socket deadline is cleared.
Ingest processing then has its own context bounded by the same configured timeout;
MCP tools retain their tool timeouts. Long-lived MCP GET/SSE streams have no
upload or blanket write deadline. Oversized or unreadable MCP bodies receive the
SDK's HTTP 400 response; ingest retains its structured error envelope.

## Health and readiness

- `GET /livez` checks process responsiveness
- `GET /readyz` checks that Boyan can accept work safely

Readiness fails if the standalone runtime reports a current readiness
diagnostic. Startup finishes pending canonical erases, then prepares source
privacy before projection repair, sequenced writers and public readers can serve
work. It reconciles retained index metadata and synchronously repairs durable
scopes in bounded pages before accepting work. Missing, v1, dirty and never-created indexes recover without a
new ingest or manual Sync call. Canonical versions and change sequences are not
advanced merely to repair an index.

Recovery time grows with unfinished work and durable scope contents. A canceled
or failed repair fails startup explicitly; construct a new host to retry after
cleanup. Shutdown cancels blocked startup recovery and drains its work. A ready
host still cannot promise an arbitrary future minimum search watermark.

## Listener safety

Boyan rejects non-loopback listener addresses in the current standalone build.

Valid examples:

- `127.0.0.1:8686`
- `[::1]:8686`
- `localhost:8686`

Unsafe wildcard or remote bind addresses are rejected at startup.

## State ownership

Current layout:

```text
<state_dir>/
├── badger/
└── bleve/
```

Do not point two Boyan processes at the same writable state root.

## Logging

Logs are structured through Zerolog and currently include:

- service startup and shutdown
- request method/path/status
- request latency
- configured listen address
- state root

Set `log.level` to `debug`, `info`, `warn`, or `error`.

## Graceful shutdown

`boyan serve` drains through ordered shutdown:

1. Stop admission and cancel active operation contexts.
2. Shut down HTTP and drain embedded operations.
3. Close sequenced ingest and projection work.
4. Close the derivation invoker and Bleve applier.
5. Stop canonical maintenance and close Badger.

`http.shutdown_timeout` bounds how long the caller waits. A timeout reports a
typed error and leaves one cleanup owner running while the process stays alive;
it does not claim that resources were released. Embedders can call `Stop` again
or `Wait` to join completion. Every `Wait` caller can observe the final result.
Closing a constructed host before startup also releases its storage.

## Projection recovery and retention

Clean Bleve generations apply changes after their durable watermark; unchanged
syncs are no-ops. Dirty, missing or incompatible generations rebuild from canonical
active state. Disposable Bleve v1 generations are incompatible with the v2
kind-field mapping and rebuild through this same path; canonical memory is
preserved. A retention floor advancing beyond the committed watermark also forces a fresh
rebuild. Exact scopes
serialize synchronization while independent scopes can progress separately.

After pending erase recovery, startup enumerates durable scope-state keys,
including scopes without a first checkpoint, and invokes serialized Sync outside
storage callbacks. Portable stores without this optional enumeration capability
retain lazy per-scope synchronization. Truncated large erases carry a durable
rebuild marker instead of an unbounded revision list; adapters that cannot
rebuild fail explicitly without advancing a false checkpoint.

Committed activation is reconciled with the physical marker at startup. Cleanup
preserves all referenced and in-flight generations, keeps at most one previous
generation per scope, and removes abandoned builders after their handles close.
Badger remains authoritative; deleting canonical records is not an index repair
procedure.

A search requesting `min_scope_change_seq` can return a retryable `conflict`
while its checkpoint lags, is dirty, or changes during the query. Let normal
synchronization/repair complete and retry; readiness alone does not certify a
particular scope's search watermark.

Boundary collection examines up to 2048 structural active heads across all
layers, in pages of at most 256. Only eligible content enters model views;
filtered records do not trigger an unbounded refill scan. Count overflow and serialized input beyond
the independent 256 KiB default budget return `limit_exceeded`; increasing a
record count does not increase the byte budget. Large scopes also increase
reconciliation scan time, while page and retained-match memory stay bounded.

## Interrupted erase and replay compatibility

A scope erase journals bounded progress and fences new writes to its exact key
and kind. Cancellation or a transient failure can leave pending work; repeat the
same request or restart to resume. Successful response requires payload scrub
and journal completion, not just logical denial. Replaying an old completed
request cannot erase later writes. Scope-free raw payload maintenance writes are
blocked while any journal is pending.

Large outcomes report exact counts and omit both reference arrays above 2048
combined references. This does not cap erasable scope history. Source forget
still rejects excessive outcome references before denial. See the
[API contracts](API.md#forget-outcomes-and-retries) for result fields and limits.

New native raw references include scope key and kind. Legacy shared references
cannot safely be scrubbed while preserving another owner. Such requests fail
with a permanent conflict before denial, release their fence, and preserve
foreign data. Unverifiable original input likewise conflicts before replay or a
model call; replacing the producer fingerprint cannot bypass canonical content
verification. There is no general foreign-record migration or operator repair
command. Historical crash windows that never recorded their selected targets
cannot reconstruct an unknowable old snapshot.

Physical backups and supported logical exports retain erase recovery journals
and fences, certificates and erased-payload tombstones. Restoring interrupted state
still requires normal startup recovery. Payload API absence is logical cleanup;
it does not promise forensic removal from disk, old backups or previous
disposable index generations. Existing retention cleanup remains responsible
for disposable generations.

## Boundary contract upgrades

Structured provider replies now follow the complete existing candidate shapes
strictly. Malformed output returns redacted permanent `invalid_derived` instead
of committing a successful empty stage. Correct the provider output and retry
the same operation; valid empty arrays and explicit profile skip remain success.

Boundary eligibility uses current processing time, with one captured T per
attempt, and inherited parent validity/expiry. `occurred_at` does not request a
historical snapshot. A delayed boundary cannot revive an expired fact;
`event_at` remains descriptive.

A successful boundary stage permanently binds normalized authoritative input.
Changed reason, occurrence time or other same-exact-scope authoritative fields
return permanent `conflict`, including new delivery sequences and partial
retries. Use the original normalized input to resume unfinished profile work.
Completed replay requires certificate verification capability; portable adapters
without it receive an explicit conflict rather than unverified success.

Old scopes containing any uncertified derived-operation history are restricted,
including atom-only history. Old native-semantic-only scopes remain usable after
a bounded exact namespace check. Scope forget/reset, altered derivation or
producer fingerprints cannot bypass this restriction. There is no automatic
backfill, metadata deletion procedure or migration/repair CLI.

Take a trustworthy backup before upgrading a writer. Supported physical and
logical roundtrips preserve boundary proof, capability markers and inherited
bounds/trust metadata, including forgotten replay evidence. Old proof-free
imports remain unproven. Integrity checks reject malformed new structural proof
records. Native restore and logical import validate integrity and reconstruct
privacy before success. They are not atomic replacements; follow the
[restore recovery rules](#source-privacy-and-restore-upgrades) rather than serving
partially loaded state.

Do not run an older writer against a store written under these contracts; it can
bypass certification or inherited-bound enforcement. A deployed rollback needs
a trustworthy pre-upgrade backup. Never strip certificates, capability markers
or grounding/privacy metadata to bypass a conflict.

## Source privacy and restore upgrades

Take a trustworthy pre-upgrade backup. Do not run an older writer against an
upgraded store, including during rollback: it can add history without required
privacy/proof metadata. A deployed downgrade requires a trustworthy pre-upgrade
backup. Never remove readiness, privacy aggregates, scope-incomplete flags,
certificates or grounding metadata to bypass a denial or conflict.

Source privacy follows validated classification history, not automatic secret
recognition. Sensitive or unknown history denies a complete raw trace even when
its root is benign; benign recall remains available. Supported complete old
native capture (a bound certificate or complete validated owned message history
with classification evidence) and full certifiable legacy history can reconstruct
standard classification. Missing, opaque, ambiguous or unsupported history
remains unknown. Unattributable opaque history can mark its exact scope key/kind
incomplete and deny later new sources there; unrelated scopes remain independent.
Known sensitive/unknown classification and scope-incomplete restrictions remain
monotone through writes, reconstruction and restore.

Older stores require `PrepareSourcePrivacy(ctx)` before direct canonical Trace
or writers can operate; unprepared Trace returns an empty `unavailable` result.
Standalone startup calls preparation after pending erase recovery and before
serving work. The exclusive pass scans authoritative sources, messages and
revisions in pages of at most 32 keys, bounds per-source membership at 32 IDs,
and checks 4 MiB encoded-value/payload bounds before copying bytes. It publishes
the fixed-version completion marker last. Work and startup delay grow with
stored history, while page memory stays bounded. Cancellation/failure leaves
readiness unpublished; the next attempt restarts the full bounded pass. Trace
itself performs source-local bounded reads rather than replaying full history.

Supported native full/incremental backups and logical exports retain privacy and
proof metadata. Integrity validates original imported metadata, including member
ownership and complete-capture bindings, before forced reconstruction. Copied
standard labels, membership bits and ready markers never establish authority.
Restrictive destination and retained incoming versions survive native restore,
including tombstones; incremental chains use the returned backup cursor as
`since`, without adding one.

Restore maintains `.boyan-restore-privacy`, a fixed local loading/validated fence
outside the copied Badger MVCC keyspace. Trace, recall and readiness remain
unavailable while fenced, including after reopening, so copied higher-version
ready markers cannot publish partially loaded data. Native restore spools one
unlinked, mode-0600 temporary artifact for two sequential passes. Temporary disk
use grows linearly with backup size. Encoded native frames have a 256 MiB
operational limit, checked before allocation and on rewritten output; this is
Boyan's bound, not an upstream Badger hard maximum. Privacy values retain their
separate 4 MiB bound. Oversized native frames return `limit_exceeded` and leave
the destination fenced.

A complete load/replay passes integrity and syncs Badger before syncing the
validated fence. Forced privacy preparation then syncs Badger again before
removing the fence and syncing its directory. A crash or cancellation during
loading requires a complete valid backup/import retry, or restoration into a
fresh destination from trustworthy data; `PrepareSourcePrivacy` alone cannot
certify a partial stream. An interruption after validation can resume preparation
through startup or `PrepareSourcePrivacy(ctx)`, after rechecking integrity. Do
not delete the fence or manually publish readiness. These are library/startup
recovery mechanisms; this release adds no restore or repair CLI.

Concurrent derivation calls use one cryptographically random ephemeral ADK
session ID per invocation, independently of stage, operation and scope. Occupied
sessions are never deleted to create another call's state. Only a successful
creator installs cleanup, using an independent context even after cancellation
or malformed output. Canonical operation IDs and durable stage outcomes remain
unchanged; entropy/create failures are retryable model failures and deadline
expiry is a timeout.

New HTTP Turn fingerprints use the canonical normalized input hash in a
versioned Turn namespace. Old raw-hash receipts remain immutable and replay
without effects only with a full certificate or full stored Turn proof. Lossy
uncertified native headers cannot authorize receipt replay, even when the raw
fingerprint matches; unprovable history returns safe HTTP 409. Boundary
fingerprint rules remain unchanged. See [canonical replay](API.md#canonical-turn-replay).

## Diagnostics workflow

Helpful local checks:

```bash
boyan serve --help
curl -sS http://127.0.0.1:8686/livez
curl -sS http://127.0.0.1:8686/readyz
go test ./cmd/boyan
go test ./...
go test -race ./...
go build ./...
```

## Current non-goals

The current standalone service does not claim:

- multi-tenant auth
- public internet exposure
- hosted control plane behavior
- container deployment metadata
- remote release publication
