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

YAML keys are decoded strictly at every static level, including fields inside
provider definitions; unknown keys fail startup. Provider IDs remain dynamic and
may contain dots. Negative timeout, byte-limit, interval and discard-ratio values
are invalid. Zero-valued optional bounds retain their documented default behavior.

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
key in YAML. Environment values override YAML values. For a provider already
listed under `derivation.runtime.providers` in YAML, supported optional fields
can also come from the environment when omitted from YAML. For example:

```bash
export BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_OPENAI_API_KEY=your-key
```

This requires a YAML provider ID `memory`; environment variables alone do not
create provider registry entries. Prefix the following field paths with
`BOYAN_DERIVATION_RUNTIME_PROVIDERS_<ID>_`, uppercase them and replace dots with
underscores:

| Provider fields | Supported paths |
| --- | --- |
| Common | `type`, `mcp_servers`, `system_instructions` |
| Hosted API | `openai.api_key`, `openai.model`, `aistudio.api_key`, `aistudio.model` |
| Pool | `pool.members` |
| ACP blocks | `cmd`, `extra_args`, `model`, `reasoning_effort`, `mode`, `bridge_version` |

ACP paths include the block name: `generic_acp`, `gemini_acp`, `codex_acp`,
`opencode_acp`, `copilot_acp`, `claude_code_acp` or `grok_acp`. List fields use
comma-separated values, rather than shell argument splitting. This finite catalog
matches the pinned runtime schema; it does not discover arbitrary nested maps.
Unset or empty overrides leave YAML values and absent optional blocks unchanged.
Attempted overrides for dotted provider IDs or IDs that collide after case
folding and dot-to-underscore conversion fail explicitly. Invalid typed values
return a contextual decode error without echoing their values.

## HTTP body limits

`http.max_body_bytes` bounds POST bodies on ingest and `/mcp` before decoding or
tool execution. `http.request_timeout` bounds upload time, including stalled
partial bodies. Once the body is fully read, that socket deadline is cleared.
Ingest processing then has its own context bounded by the same configured timeout;
accepted work still queued behind another request in the same scope returns a
retryable timeout promptly when that context ends and does not execute. Once a
worker owns the callback, the caller waits for completion so callback resources
and returned outcomes retain their safe lifetime; an active callback must honor
its context for prompt termination.
MCP tools retain their tool timeouts. Long-lived MCP GET/SSE streams have no
upload or blanket write deadline. Oversized or unreadable MCP bodies receive the
SDK's HTTP 400 response; ingest retains its structured error envelope.

## Health and readiness

- `GET /livez` checks process responsiveness
- `GET /readyz` checks that Boyan can accept work safely

Readiness fails if the standalone runtime reports a current readiness
diagnostic. Startup first rejects incomplete loading restores or resumes validated
restore preparation, then finishes pending canonical erases and prepares source
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
while its checkpoint lags, is dirty, or changes during the query. A canonical
commit and projection synchronization after the request captures its initial
watermark can also return a retryable conflict; retry the same search. Let normal
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
For ordinary stores, standalone startup calls preparation after pending erase
recovery and before serving work. Restoring stores first complete the narrow
validated-restore preflight; loading restores cannot advance erase journals.
The exclusive pass scans authoritative sources, messages and
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
certify a partial stream. A loading-fenced logical destination accepts a complete
valid retry even if the interrupted stream imported pending erase metadata.
Erase recovery and mutations remain fenced during loading, including failed
startup attempts, so they cannot alter that metadata before retry validation.
Ordinary live stores retain their pending-erase import guard. An interruption
after validation can resume preparation
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
- remote release publication

## Embeddings configuration and bounds

This is the authoritative table for optional embeddings. All byte limits are
UTF-8/encoded bytes; runtime bounds are fixed, not additional YAML settings.
The complete example is [boyan.example.yaml](../boyan.example.yaml). Every
embedding key accepts the corresponding `BOYAN_EMBEDDINGS_<UPPERCASE_KEY>`
environment override. Empty `BOYAN_*` values are ignored by the configuration
loader and leave YAML values unchanged; clear a setting explicitly in YAML.
Defaults omit embeddings; `enabled: false` ignores all
other embedding fields, resolves no credential, makes no provider calls and
does not create an embeddings directory.

| Setting or bound | Contract |
| --- | --- |
| `enabled` | Boolean, default false; explicit opt-in required |
| `endpoint` | Full HTTP(S) embeddings URL, required when enabled, at most 2,048 bytes, host required, no userinfo or fragment; reference `http://127.0.0.1:8080/v1/embeddings` |
| `model` | Nonempty UTF-8 without control characters, at most 256 bytes; reference `intfloat/multilingual-e5-base` |
| `revision` | Nonempty UTF-8 without control characters, at most 256 bytes; operator must identify actual immutable weights; reference `d128750597153bb5987e10b1c3493a34e5a4502a` |
| `dimensions` | Integer 1–4,096; reference 768 |
| `query_prefix`, `passage_prefix` | UTF-8, no NUL, at most 256 bytes each; reference `'query: '` and `'passage: '` (trailing spaces matter) |
| `api_key_env` | Empty or portable environment name `[A-Za-z_][A-Za-z0-9_]*`, at most 256 bytes; reference empty |
| Credential value | When named, environment value must exist, be nonempty, at most 4,096 bytes, without CR/LF; held only in memory |
| `failure_policy` | Omitted or `lexical`; `strict` is unsupported |
| Provider request | 16 inputs, 2,048 bytes/input including prefix, 65,536-byte serialized body, float encoding |
| Provider response | 1,048,576 bytes; exact model, count, unique indexes, dimensions, finite nonzero vectors required |
| Provider scheduling | 4 concurrent calls, 10-second call budget including slot admission (caller deadline/cancellation also applies), 0 automatic retries, 0 redirects; proxies and connection reuse disabled |
| Text preparation | CRLF→LF, NFC, trim outer whitespace; 384-rune windows, 64-rune overlap; 128 passage windows, 4 query windows |
| Text limits | Record 32,768 bytes; query 4,096 bytes; query omissions reported in Go while full query remains lexical |
| Projection per scope | 8,192 chunks, 8,192 scanned records, 67,108,864 encoded bytes including metadata; complete publication only |
| Build scheduling | 2 workers, 64 queued scopes, 1 concurrent build/scope, 15-minute build deadline; startup enumerates 128-scope pages |
| Ranking | At most 100 candidates/channel, 200 fused, within existing 256 admitted-candidate ceiling; channel request `min(100,max(20,4*limit))`; final caller limit at most 100 |
| Fusion | RRF constant 60; unit float32 normalization, tolerance 0.001; deterministic identity tie breaks |
| Model-space identity | SHA-256 over model, revision, dimensions, both prefixes, `boyan-semantic-nfc-window-v1`, `unit-float32-v1`; endpoint and credentials excluded |
| TEI reference resources | Linux amd64 CPU, 2 CPUs, 4 GiB RAM; tokenizer/Rayon/OMP/MKL workers 2; float32, mean pooling |
| TEI reference bounds | 4 concurrent requests, client batch 16, server batch 8 requests/8,192 tokens, payload 65,536 bytes; `AUTO_TRUNCATE=false` |
| TEI health/cache | `/health` every 10s, 3s timeout, 12 retries, 10m start period; named cache at `/data` |

Only standard-sensitivity records with supported retention are eligible.
Enabling a remote endpoint exports eligible memory text and query text to that
operator-chosen service. Classification is not secret detection. Choose and
secure the recipient accordingly. Credentials, endpoint, source text and
upstream error prose are excluded from diagnostics and semantic persistence.
Vectors and IDs/filter metadata can still reveal information: protect the
embeddings directory and backups as sensitive disposable derived state.
The projection byte limit is per active scope generation; it is not a global
disk or process-memory quota. Scope count, database overhead and retained
generations/cache also consume storage.

## Optional TEI sidecar

The default [Compose file](../deploy/sidecar/compose.yaml) has no TEI service or
dependency. Supply `BOYAN_IMAGE`, an operator-built image containing
`/usr/local/bin/boyan` and the chosen derivation runtime. There is no published
Boyan image or automatic container build here. Set `BOYAN_CONFIG` to an absolute
path to your configured YAML; the repository example requires a working
derivation provider before ingestion can succeed. State lives in `boyan-state`.

Before enabling the local TEI overlay, the YAML mounted through `BOYAN_CONFIG`
must explicitly contain:

```yaml
embeddings:
  api_key_env: ''
```

When reusing a remote-provider configuration, replace its credential reference
(for example, `api_key_env: OLD_TOKEN`) with this empty YAML value. The overlay's
empty `BOYAN_EMBEDDINGS_API_KEY_ENV` leaves the YAML value unchanged; it cannot
clear a remote credential reference. The local TEI profile requires no API key.
The [repository example](../boyan.example.yaml) already has the required empty
value; preserve it when configuring the derivation provider.

```bash
export BOYAN_IMAGE=your-local-boyan-image
export BOYAN_CONFIG=/absolute/path/to/boyan.yaml
docker compose -f deploy/sidecar/compose.yaml up -d
# Explicit semantic opt-in:
docker compose -f deploy/sidecar/compose.yaml \
  -f deploy/sidecar/embeddings.compose.yaml up -d
```

The [overlay](../deploy/sidecar/embeddings.compose.yaml) pins Knowl's TEI
`cpu-1.9.0` image by SHA-256 digest and the E5 weight revision above. TEI shares
Boyan's network namespace and listens on `127.0.0.1:8080`; Boyan listens on
`127.0.0.1:8686`. Neither service publishes a host port. API/MCP consumers must
join Boyan's network namespace, for example a Compose consumer with
`network_mode: service:boyan`. Ordinary host `localhost:8686` cannot reach this
container deployment. The private bridge has outbound access for model download
and configured derivation providers; it is not an air-gap. TEI's healthcheck
reports provider health independently and never gates Boyan readiness. The
model cache persists across restarts. Avoid adding a TEI default prompt: Boyan
already adds the asymmetric prefixes.

Options were checked against the [TEI v1.9.0 router contract](https://github.com/huggingface/text-embeddings-inference/blob/v1.9.0/router/src/main.rs).
The reference profile is amd64 only; other architectures require a compatible
operator-supplied endpoint and separate evaluation. Rune windows do not promise
the same token count for every language; overlong tokenizer inputs fail safely
with lexical fallback instead of server-side truncation.

## Semantic lifecycle and rollback

Canonical Badger and lexical Bleve remain authoritative. Startup repairs Bleve
before serving. With embeddings enabled, host construction also opens the
semantic database, validates every active generation and collects startup
garbage synchronously before `Host.Start`. An open failure, corruption, active
generation validation failure or startup garbage-collection failure is fatal:
the host cannot serve even lexical recall. Store open uses
`context.Background` for validation and collection; the context supplied to
`Host.Start` does not bound this work.

After the store opens successfully, optional semantic builds and repair run in
the background. Cold models, empty/incompatible model-space state, rebuilds,
capacity overflow and provider failure permit ordinary recall through the
lexical path. A semantic generation is eligible only for the exact active
lexical generation/watermark, canonical
sequence and model-space fingerprint. Changes to model/revision/dimensions,
prefixes or preprocessing/normalization invalidate old vectors and require a
complete rebuild. Partial generations are never queried. Queue overflow
degrades safely; a later scope change, restart or in-process repair can retry.
Publication atomically retires obsolete generations, while startup collection
also removes abandoned staging state. There is no new public repair CLI.

`/readyz` remains about required service/canonical readiness after successful
host construction. Provider outages and background semantic build failures do
not make it fail; semantic store open/validation failures prevent the host and
its readiness endpoint from starting. When enabled,
`X-Boyan-Semantic-State` reports
`repairing`, `hybrid-ready`, `degraded` or `stopped`;
`X-Boyan-Retrieval-Mode` reports `hybrid` or `degraded`, with
`X-Boyan-Retrieval-Reason: unavailable` when degraded. Disabled deployments
omit these headers and host scope status reports `lexical`. It is an operational summary, not a
certificate for a particular scope or a live provider probe. Host
`SemanticStatus(ctx, scope)` provides scope-local state, safe reason, a 12-hex
model-space prefix and lexical watermark. Inspect each recall's retrieval
report for the actual effective mode. With enabled embeddings, a nonzero
`min_scope_change_seq` fails with retryable `conflict` while hybrid compatibility
cannot be certified; it does not silently return lexical success. Disabled
embeddings retain existing Bleve certification. Caller cancellation and invalid
configuration remain errors.

To roll back this feature, stop the deployment, omit the overlay, explicitly
set `embeddings.enabled: false` (and remove any enabling environment override),
then start the default deployment with `--remove-orphans` to remove TEI. Do not
use `down -v`: it would remove the canonical state volume. While Boyan is
stopped, you may delete only `<state_dir>/embeddings` inside the state volume;
preserve `<state_dir>/badger` and `<state_dir>/bleve`. The TEI model cache is
separate and optional to retain. This is also recovery for a corrupt or
unopenable semantic store: stop Boyan, then disable embeddings or remove only
the disposable embeddings directory before restarting. Re-enabling rebuilds
semantic state from canonical data without a canonical-format migration. This feature rollback
does not authorize downgrading a writer across the privacy/proof upgrades
documented above. Deletion is not a forensic-erasure guarantee for backups or
old disk blocks.

## Embeddings verification

Render and assert deployment values without starting/pulling any model:

```bash
bash deploy/sidecar/check-compose.sh
go test ./internal/standalonecfg -run 'TestLoadConfig.*(Embedding|Shipped)' -count=1
```

Invoke the separate real-provider evaluation with:

```bash
BOYAN_TEI_EVAL_ENDPOINT=http://127.0.0.1:8080/v1/embeddings \
  go test ./internal/semanticeval -run TestReferenceTEI -count=1 -v
```

Run from a namespace that can reach the provider and its `/info` identity probe.
The [fixed public corpus and thresholds](SEMANTIC_EVALUATION.md) cover
English/Russian/mixed text, exact controls, paraphrases, semantic-only recall,
hard distractors, filters, update/delete and outage behavior through the actual
Host pipeline. Configured identity or retrieval failures fail the test; an
unset endpoint explicitly skips evaluation and supplies no quality proof.
Knowl results are reference evidence only. Latency/resource observations are
measurements for a specific corpus/hardware, never universal quality,
throughput, availability or semantic-correctness guarantees.
