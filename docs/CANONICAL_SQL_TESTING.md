# Canonical SQL preservation tests

## Test selection

Run isolated units without database/service fixtures or the integration tag:

```sh
env -u BOYAN_REQUIRE_POSTGRES -u BOYAN_TEST_POSTGRES_DSN go test ./...
env -u BOYAN_REQUIRE_POSTGRES -u BOYAN_TEST_POSTGRES_DSN go test -race ./...
```

Keep `integration` absent from `GOFLAGS` for these default commands. Pure
codecs, unsigned/envelope validation, configuration, fake-port policies and
the preservation inventory remain available by default. Actual database/storage
and service interactions require `-tags=integration`, including embedded SQLite,
native extensions, transactions, reopen, recovery, raw corruption probes and
HTTP/socket/TEI fixtures. Mixed files retain their pure units untagged and move
native declarations and exclusive helpers to tagged files. Dedicated native
fixture support and matched benchmark/TestMain helpers are tagged together.
Production driver registration alone does not open a test database.

Tagged `./...` also includes untagged units, as Go normally does. Build tags
select integration; `-short`, test-name filters and missing-backend skips do
not implement the separation. Discovery is useful but does not establish
successful native execution:

```sh
go list -test -json ./...
go test -list . ./...
go list -tags=integration -test -json ./...
go test -tags=integration -list . ./...
```

The required full native matrix runs separately with both real backends. Set
`BOYAN_TEST_POSTGRES_DSN` privately to an explicitly selected test server before
each invocation; do not log credentials:

```sh
BOYAN_REQUIRE_POSTGRES=1 GOMAXPROCS=4 \
  go test -count=1 -json -tags=integration -timeout=30m -p=1 ./...
BOYAN_REQUIRE_POSTGRES=1 GOMAXPROCS=4 \
  go test -count=1 -json -race -tags=integration -timeout=30m -p=1 ./...
```

Keep the DSN in both processes' environment. Serial package scheduling (`-p=1`)
avoids concurrent package competition for the same host and test server; it is
not a proven timeout fix or speedup. The full race selection and retained
257/1025/8192 workloads remain required. The 30-minute per-package timeout is
a finite engineering execution budget for the unchanged native workload, not
a product latency SLA or a promised passing result.
Neither reduced assertions nor optional TEI/cost skips establish native parity.
No new model or benchmark campaign is required.

`internal/canonicalsqltest` supplies real raw connections. Its `storefixture`
subpackage registers the public SQLite and PostgreSQL owners without introducing
an import cycle in primitive tests. The owner fixture tests lifecycle and the
implemented privacy/preparation, denial-read and payload contracts. Shared
privacy cases use actual owners with bounded structural history fixtures.
Preservation checks run mutation, reader and erase cases against both
engines; the retired Badger package is no longer built.

The SQLite owner opens a separate canonical file through `store/sqlite.Open`.
It holds an OS exclusive lock on the database file itself (including aliases),
uses a single pinned connection configured with WAL/FULL and constraints, and
drains canceled transactions before close/checkpoint/unlock. Opening is distinct
from readiness: structural integrity is checked before pending-erase recovery
and privacy preparation. Both Store pointers expose PrepareSourcePrivacy,
SourcePrivacyReady, PutPayload/LoadPayload/DeletePayload and source/revision
denial reads. Preparation retains exclusive admission across bounded pages,
validates retained completed captures, preserves incomplete/sticky state and
publishes the fixed-version ready marker last.

`store/postgres.Open` acquires a fixed session advisory lock before migration
on one direct pgx connection. Every transaction uses context-aware serialized
admission and checks ownership on that connection. Disconnect, protocol failure
or missing ownership permanently fences that owner until full close/reopen;
there is no automatic reconnect or ambiguous-commit retry. Preflight checks
stored structural integrity. Both owners expose the same
small lifecycle test factory plus the implemented canonical memory ports.

PostgreSQL configuration accepts a resolved private DSN, a bounded connect
timeout (default 5s, maximum 30s), and `AllowLocalInsecure`. Remote DSNs require
`sslmode=verify-full`; local Unix/loopback `sslmode=disable` also requires the
explicit local opt-in. Multiple-host and TLS downgrade fallbacks are rejected.
The session uses a trusted catalog search path and fully qualified canonical
tables. Configuration formatting and driver errors redact connection details.

## Real databases

SQLite uses a fresh on-disk file under `testing.T.TempDir`, a dedicated
`database/sql.Conn`, WAL, FULL synchronous writes and foreign keys. PostgreSQL
uses direct `pgx.Conn` connections and a random dedicated database containing
the fixed `boyan_canonical` schema. `Database.DSN` reconnects to that disposable
database, so later constructors never receive the maintenance database DSN.

Set `BOYAN_TEST_POSTGRES_DSN` to an explicitly selected test server. Its role
needs `CREATEDB`; the harness creates no roles and uses no existing application
schema. Cleanup verifies the generated name, database OID and owner before
dropping its own database. An identity mismatch or leaked connection fails
cleanup visibly; cleanup does not use `DROP ... FORCE`. Never print a fixture's
DSN, which may contain credentials.

```sh
BOYAN_REQUIRE_POSTGRES=1 \
BOYAN_TEST_POSTGRES_DSN='postgres://test_role@127.0.0.1:5432/postgres?sslmode=disable' \
go test -tags=integration -count=1 -v ./internal/canonicalsqltest/...
```

`BOYAN_REQUIRE_POSTGRES=1` makes absent configuration fail before the matrix can
skip PostgreSQL. A configured but unreachable server always fails. Optional
local runs without a DSN skip only PostgreSQL subtests and provide no parity
evidence. The CI integration job uses required mode, a healthy dedicated PostgreSQL
18.6 service and the exact official image index digest pinned in its workflow.
Its password and `sslmode=disable` apply to that isolated test service.

## CI eligibility and static checks

The existing `.github/workflows/test.yml` runs separate default normal/race
and tagged native normal/full-race checks. Only the integration job owns the
PostgreSQL service and required-mode environment. Its job-level condition is
evaluated before services start:

| Event | Default normal/race | Tagged native normal/full race |
| --- | --- | --- |
| Push to `main` or `master` | Yes | No |
| Pull request targeting `main` | Yes | Yes |
| Pull request targeting `master` | Yes | No |
| Published release | Yes | Yes |

The published-release trigger runs verification in this workflow; it does not
publish artifacts. Ordinary tag pushes do not select this workflow. The native
job retains the pinned PostgreSQL 18.6-bookworm image digest and health checks;
its generated databases still use the owned disposable fixture safeguards above.
The default job has no PostgreSQL service or test DSN.

`.github/workflows/lint.yml` retains architecture checks and golangci-lint
v2.12.2 without `--fix`, inspecting both default and integration-tagged code.
Lint needs no PostgreSQL service. Local equivalents are:

```sh
go build ./...
go tool go-arch-lint check --project-path .
golangci-lint run
golangci-lint run --build-tags=integration
```

Use the repository-pinned v2.12.2 binary. Static workflow inspection is not a
successful hosted CI run, and pre-merge checks do not replace verification of
the actual merged target state.

Raw probes cover committed versus rolled-back writes, binary values and
eight-byte unsigned ordering above MaxInt64, reconnecting the disposable DSN,
independent fixture cleanup and secret-free configuration failures. These probes
establish infrastructure; they do not claim canonical conformance.

## Preservation registration

`coverage.json` is a fixed, executable registration of the current native SQL surfaces.
`TestPreservationInventory` compares it against a deterministic standard-library
AST scan. It registers all38 typed SQL durable family constants, key constructors/direct tuple sites,
write primitives/record helpers, raw accesses/fault hooks and every test/helper
in canonicalsql, both public SQL adapters and their transitive consumer packages.
Complete package registration includes
shared fixtures that construct a backend indirectly. Benchmarks are recorded as
preservation evidence, not executed by the ordinary test command.

The scan is tag-agnostic and checks inactive tagged files too; its synthetic
tagged-file discovery control remains part of the default inventory test.
Pure inventory check/regeneration commands below deliberately stay untagged.

Each entry names its source, symbol, relevant raw calls and primary migration
owner. Registration is not a record that a case already passed on SQL.
Historical maintenance-only cases are explicitly retired because the unrequested
application archive/export/import subsystem was removed; they are not passing
retained behavior. Review every changed registration before
regenerating it:

```sh
go test -count=1 -run TestPreservationInventory ./internal/canonicalsqltest
go test -count=1 -run TestPreservationInventory ./internal/canonicalsqltest -args -write-coverage
```

Do not remove retained-behavior registrations merely to make this check pass.
The pre-retirement2668-entry inventory remains immutable at commit `4b146d7`,
path `internal/canonicalsqltest/coverage.json`; it records retired implementation
symbols, not2668 independent behavioral assertions. TASK-019's Beads evidence
records concrete SQL counterparts; TASK-020 removes the obsolete Badger
implementation/fixtures while preserving native assertion bodies. Closed
maintenance tasks13–15/24/27 are historical retired scope, not promised features.
Current family/case registration is not a substitute for reviewing those bodies
or required dual-backend results.

Original benchmark sources are preserved as noncompiled `.go.txt` under
`internal/canonicalsqltest/testdata/provenance` and
`internal/sqlitegate/testdata/provenance`. Live matched harnesses now use SQL;
old measurements are not a baseline for the changed authority. No new benchmark
campaign is required. Static synthetic populated-old-Badger evidence lives in
`internal/standaloneruntime/testdata`; startup tests hash its files before/after
SQL lifecycle and check the old operation was not imported. They never open a
Badger engine. Protected audit results and user drafts remain unchanged.

| Durable behavior / corruption category | Primary migration owners |
| --- | --- |
| Binary keys, global namespaces and unsigned cursors | TASK-002 |
| Sources/messages/items/revisions/payloads, immutable equality, CAS, replay and incremental mutation | TASK-007 |
| Active heads, exact scope, temporal reads, raw membership and recall/trace interleavings | TASK-008/009 |
| Source privacy/classification/membership/incomplete state and marker-last preparation | TASK-005 |
| Turn/boundary certificates, capabilities, copied/tampered proofs and grounding edges | TASK-006 |
| Ordered producer receipts/cursors, gaps and commit-before-receipt recovery | TASK-010 |
| Delivery claims/leases, projection manifests/activation/retention/checkpoints | TASK-011 |
| Permanent source/revision denials, erase journal/fence/guard, source/blob ownership locks, erased-blob tombstones and paged crash recovery | TASK-012 |
| Startup/readiness, canonical authority binding, search cancellation, lexical/vector denial retirement and fallback | TASK-017/018 |
| Remaining consumer integration, raw corruption fixtures and old-file preservation acceptance | TASK-019 |

The global payload namespace, `source-privacy-ready` marker and erase guard must
not become synthetic user scopes in SQL. Domain scope Version/ChangeSeq and CAS,
full-tuple physical identity, duplicate rejection and envelope validation remain
required independently of the removed archive clock and row stamps. Retained
tests cover corrupt real rows, rollback, failure before readiness and interrupted
pending-erase recovery on both engines. Task31 retires maintenance-only
TASK-013/014/015 coverage without replacing it with another archive subsystem.

The pgx v5.10.0 pin requires Go 1.25.0 and is compatible with the repository's
Go 1.26 toolchain; checksums are committed in `go.sum`. Pin provenance is the
[upstream tagged module](https://github.com/jackc/pgx/blob/v5.10.0/go.mod) and
[official PostgreSQL image catalog](https://github.com/docker-library/official-images/blob/master/library/postgres).

## Preserved runtime evidence and limits

At original revision `7f50d5cdac028408d626df0f252599d84371eca2`, before the
integration split, the full normal run passed 31 packages. The sqlitegate
package took 575.931 seconds; its DenseOracle PostgreSQL case took 524.56
seconds versus 49.01 seconds for SQLite. These native cases ran in the former
untagged selection; those commands no longer describe the current default suite.

The original full race attempt reported 600-second package timeouts in
`index/sqlite`, `internal/canonicalcore` and `internal/canonicalsql`, followed
by verified cancellation with exit 143. It did not complete or pass. An active
testcase in a timeout report does not establish the complete cause, and absence
of a `DATA RACE` warning in an incomplete run does not prove race safety.

The original `/tmp/boyan-task21-{full,race,build,arch,lint}.log` and matching
`*result.json` records are retained unchanged as local evidence. Subsequent
bounded profiles showed modernc race instrumentation, TSAN and checkptr costs;
native PostgreSQL setup/hydration/rebuild also showed repeated queries. These
are contributing observations, not a complete runtime-cost breakdown. The
DenseOracle duration includes native setup and rebuild, not pure vector-search
time. No measured speedup, new latency SLA or completed native race pass is
claimed. The current required full matrix must still pass independently before
integration and merged-state acceptance.

Test selection changes no storage contract: fresh SQLite/PostgreSQL selection,
stop/reopen, pending-erase recovery before readiness and permanent owner-loss
fencing remain supported. Existing Badger files are preserved without data
transfer. There is no application backup/restore/archive/export/import API.
Unsupported prior unshipped SQL schemas are refused without automatic
conversion or reset; see [Operations](OPERATIONS.md) for configuration and
lifecycle guidance.
