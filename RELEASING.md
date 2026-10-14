# Releasing

Boyan already supports local builds and local distribution. This document
describes the current standalone release shape only.

## Local release outputs

The standalone artifact is the `boyan` binary built from `./cmd/boyan`.

Build it locally:

```bash
go build -o ./dist/boyan ./cmd/boyan
```

Validate the command surface:

```bash
./dist/boyan --help
./dist/boyan serve --help
```

## Required local gates

Before treating a change as releaseable locally, run:

```bash
go test ./...
go test -race ./...
go build ./...
go tool golangci-lint run ./...
go tool go-arch-lint check --project-path .
go mod verify
git diff --check
```

Recommended additional checks:

`golangci-lint` is pinned as a Go tool dependency in `go.mod`, so the local
lint path stays versioned with the repository.

```bash
go list -deps ./...
```

The dependency graph must not introduce legacy integration-only dependencies.

## Distribution expectations

Current supported distribution modes:

- `go install ./cmd/boyan`
- `go build -o ./dist/boyan ./cmd/boyan`

Current unsupported claims:

- published GitHub release artifacts
- module tags consumable from a remote public repository
- container images
- hosted deployment metadata

Do not document or advertise those until the repository actually supports
them.

## Release checklist

1. CLI help is coherent for `boyan` and `boyan serve`.
2. `boyan.example.yaml` matches the actual loader keys.
3. README, API, architecture, and operations docs match the current binary.
4. Local verification passes, including lint and architecture checks.
5. Runtime regressions cover MCP streaming, bounded uploads, repeated waits,
   resource release, incremental sync, recovery and generation retention.
   Functional regressions also cover native/legacy trace privacy, checkpoint
   consistency, current-head temporal filtering, kind filtering/rebuild, bounded
   ranking and growing boundary/reconciliation scopes.
6. No legacy integration-only dependency is introduced.
7. Recovery regressions cover interrupted payload scrubbing, counted large erases
   with projection rebuilds, immutable legacy/native replay, lost receipts with
   an unavailable extractor, and startup index repair without new ingest.

## Boundary contract compatibility

Release checks cover strict four-stage model replies with valid empty/skip
controls, current-T eligibility and inherited lifetimes, normalized immutable
boundary replay across partial completion/restart/receipts, and native/logical
proof roundtrips. Preserve the existing candidate wire spellings.

Before deployment, follow the [boundary upgrade restrictions](docs/OPERATIONS.md#boundary-contract-upgrades).
Uncertified old derived scopes (including atom-only history) fail closed;
native-semantic-only old scopes may establish certification safely. Do not run
old writers against upgraded state or delete proof/bounds metadata. Deployed
rollback requires a trustworthy pre-upgrade backup.

## Privacy and retry compatibility

Release checks cover same-message and separate-message mixed sources, unknown
legacy history, whole-graph denial with benign recall, bounded source-local
Trace reads and the final privacy barrier under sensitive writes, forget, erase
and cancellation. Supported standard native/legacy history stays available after
preparation. Direct unprepared readers fail unavailable; interrupted preparation
restarts without publishing readiness. Classification labels do not promise
secret detection, and existing native tool-trace limitations remain.

Native full/incremental and logical roundtrips must retain sensitive, completed
unknown and scope-incomplete restrictions. Check forged/malformed imported
metadata, higher-MVCC copied readiness, failed loading across reopen and
validated preparation restart. The external restore fence and both Badger sync
boundaries must precede availability. Preserve the 256 MiB operational native
frame bound, bounded memory and linear temporary spool disk cost. Follow the
[source privacy upgrade restrictions](docs/OPERATIONS.md#source-privacy-and-restore-upgrades);
never use an old writer or strip metadata/fences, including during rollback.

Runtime checks use actual ADK session ownership under overlapping calls,
cancellation and failures. HTTP checks compare Turn transport equality with the
sole canonical fingerprint authority, including UTC/default/generated-ID retries
after restart, changed-input rejection and independent exact-scope lanes. Keep
explicit pre-upgrade raw-hash receipt fixtures: full certificate/full stored Turn
proof authorizes immutable duplicate outcomes with no repeated effects; lossy
uncertified native headers fail HTTP 409 even with a matching fingerprint.
Boundary fingerprints and canonical stable IDs remain unchanged.

## Integrated delivery

For an approved Prism story, delivery includes integrating all approved
changes into target `main` and pushing `origin/main` under the existing user
authorization. Record the integrated revision and confirm the remote contains it.
Run all seven required gates above freshly on the final integrated state; successful branch
checks alone do not establish delivered correctness.

The separate Prism Verify phase checks the integrated evidence and all child
statuses before closing the story. Keep the story open if integration, push or
verification fails. Each Prism phase runs in its own subagent, as required by
[AGENTS.md](AGENTS.md).
