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
diagnostic.

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

Committed activation is reconciled with the physical marker at startup. Cleanup
preserves all referenced and in-flight generations, keeps at most one previous
generation per scope, and removes abandoned builders after their handles close.
Badger remains authoritative; deleting canonical records is not an index repair
procedure.

A search requesting `min_scope_change_seq` can return a retryable `conflict`
while its checkpoint lags, is dirty, or changes during the query. Let normal
synchronization/repair complete and retry; readiness alone does not certify a
particular scope's search watermark.

Boundary model views support up to 2048 active records across all layers,
collected in pages of at most 256. Count overflow and serialized input beyond
the independent 256 KiB default budget return `limit_exceeded`; increasing a
record count does not increase the byte budget. Large scopes also increase
reconciliation scan time, while page and retained-match memory stay bounded.

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
