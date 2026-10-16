# SQLite dependency feasibility fixture

This test-only package is the first approved `boyan-48w.1` gate. It does not
implement the production adapter, create application state, or switch runtime
composition. Its temporary tables and preprocessing helpers are experiments;
later tasks must implement and test the actual adapter separately.

The following relevance controls were authored before replacing either engine.
Expected IDs express required relevance, not numerical Bleve score parity.

| Revision ID | Document | Queries requiring this revision |
| --- | --- | --- |
| ru | Кошки гуляют возле домов | кошка; дом; кошка дом |
| en | The engineers are running tests | engineer; run; test; engineer run test |
| mixed | Настройки database connections | настройка; connection; настройка connection |
| identifiers | user_id HTTP/2 cache-key v1.2 | user_id; cache-key; HTTP/2; v1.2 |
| stopwords | the и | the и |
| nfc | cafe + U+0301 | café (Latin precomposed U+00E9) |
| tie-a, tie-b | identical punctuation!!! | punctuation (binary ID order tie-a, tie-b) |
| decoy | banana volcano | no required matches for the preceding queries |

Punctuation-only input must produce no matches. Quoted `"кошка"` remains
literal word retrieval. `" OR banana --` is analyzed as the three literal
tokens `or`, `banana` and has only the decoy match; there is no unrestricted
FTS/SQL execution. The OR relevance policy intentionally permits any analyzed
term. Empty-token queries must never reach unrestricted SQL.

The nfc row is a normalization control, not a claim of accent insensitivity.
Tests use explicit Unicode escapes for its independently chosen equivalent
forms. Old-engine baseline observations are recorded separately; stopword-only
behavior may differ under the approved new raw-token policy.

Old Bleve baseline (main 224cda4, real temporary projection, same documents):
`кошка дом` → ru; `engineer run test` → en; `настройка connection` → mixed;
`user_id`, `cache-key` → identifiers; `the и` → stopwords,en;
`punctuation` → tie-a,tie-b; quoted кошка → ru; `" OR banana --` → decoy;
`!!!` and precomposed `café` → empty. New NFC handling returns nfc for café.
The temporary baseline harness was removed after recording observations so the
gate does not retain a dependency on the retired engine.

Gate evidence (Go 1.26.5, linux/amd64):

- Downloaded `modernc.org/sqlite v1.60.1` (origin b122d0417c01508beb55158faedeb64a0c5bfd8a),
  `modernc.org/libc v1.77.1` (9176651b0b6d3fb661a848553e5d608e47e2285e),
  `github.com/kljensen/snowball v0.10.0` (2569472eb930a0c3c7435cd265362823c7bdaf9e).
  Module checksums are in go.sum and `go mod verify` passed.
- Inspected downloaded sqlite go.mod: Go 1.26.0 and exact libc v1.77.1.
  libc requires x/tools v0.50.0, x/mod v0.41.0, x/sync v0.23.0 and
  x/sys v0.48.0. Minimal version selection also advances x/text, x/net,
  x/crypto and x/term; these are transitive consequences of the exact pin.
- sqlite `vec/patches.go` registers sqlite3_auto_extension in init; its
  build tags include Linux and generated vec_linux_amd64.go has
  `linux && amd64`. Real connections report vec_version() = v0.1.9.
  No external extension path or custom page cache is used.
- sqlite driver and libc LICENSE files use BSD-3-Clause; sqlite itself is
  public domain. Bundled sqlite-vec is used under MIT with its notice in
  sqlite's LICENSE-SQLITE_VEC (upstream offers Apache-2.0 OR MIT).
  Snowball LICENSE is MIT. Direct english/russian Stem(token,false) retains
  stopwords; raw tokens are preserved independently. NFC uses x/text v0.42.0.
- Repository evidence: Ubuntu CI, local build/install in RELEASING.md,
  linux/amd64 sidecar deployment. No additional release target matrix exists.
  This gate establishes linux/amd64 only.
- Actual SQL proves FTS5/MATCH, BLOB cosine, MATERIALIZE eligible before
  distance, maximum across two query windows and all revision chunks before
  top-k, binary ties, empty/fewer-than-k, and excluded malformed vectors.
  NaN norm becomes NULL; ±Inf norm becomes +Inf. Both are rejected. Text JSON,
  malformed lengths, wrong dimensions, zero/nonunit vectors are rejected.
  Dimensions 1, 768, 4096 run. Near-tie SQL cosine 1 versus independent double
  oracle 0.99999999500000003 differs by 5e-9; exact SQL ties use revision ID.
- Real pools verify initialization on all three simultaneous physical
  connections, query-only reader rejection, private 0600 database/WAL/SHM,
  escaped URI filename, reopen, pinned reads during writer publication,
  rollback after failed SQL, transaction/active scan/queued writer cancellation.
- RED: missing vec import fails with `no such function: vec_version`; raw-only
  analyzer fails кошка/engineer controls. GREEN uses static vec import and
  direct pinned stemmers. All code here remains test-only.
- Passed: full `go test ./...`; final focused `go test -race -count=1
  ./internal/sqlitegate`; `CGO_ENABLED=0 go test -count=1 ./internal/sqlitegate`;
  `go vet ./...`; pinned full golangci-lint (0 issues; existing gomodguard
  deprecation warning); architecture; mod verify; tidy -diff; diff --check.
  Tests/lint use task-owned /tmp caches. Final focused checks cover the frozen
  fixture after formatting and removing the temporary baseline harness.
- `CGO_ENABLED=0 go build -buildvcs=false ./...` passed. Plain build fails VCS
  stamping because Go selects an unrelated existing read-only `/tmp/.git`
  directory (`go build -x`: cd /tmp; git status --porcelain). The worktree's
  own git status/log succeed. No unrelated metadata was altered.

These are feasibility results, not evidence for runtime migration, production
secure opening, certification, every application filter, or lifecycle shutdown.
Those contracts remain with their approved later tasks.
