# Canonical SQL preservation tests

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
go test -count=1 -v ./internal/canonicalsqltest/...
```

`BOYAN_REQUIRE_POSTGRES=1` makes absent configuration fail before the matrix can
skip PostgreSQL. A configured but unreachable server always fails. Optional
local runs without a DSN skip only PostgreSQL subtests and provide no parity
evidence. The CI test job uses required mode, a healthy dedicated PostgreSQL
18.6 service and the exact official image index digest pinned in its workflow.
Its password and `sslmode=disable` apply to that isolated test service.

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
