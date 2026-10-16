# Accepted audit and old-engine baseline

TASK-002 trace: REQ-SEARCH-003,012,016,018; REQ-QUALITY-005; CON-004,006.
The runtime at this gate still uses Bleve and vector Badger. No SQLite adapter
or runtime switch is part of this task.

## Audit provenance

Read-only source: `/tmp/boyan-docs-product-contract`, branch
`fix/docs-product-contract`; base and HEAD both
`224cda43066f358276f130ae98e948eed8a10b93`. Captured before integration:
six modified tracked files and exactly two untracked regression tests.
Binary-safe `git diff --binary --full-index`, branch/base/head, porcelain
status, complete copies of both untracked tests and SHA256 manifests are in
`/tmp/boyan-48w-task2-evidence.KkQvRi`.

Tracked patch SHA256:
`a6f1bd72cbdebe98f06a284a521981b199eadd2d072d49ad0494a04d7a2f2c94`.
The compact manifest below is durable; the full patch stays in the temporary
evidence artifact rather than duplicating all integrated changes here.

| Source path | SHA256 of complete file |
| --- | --- |
| ARCHITECTURE.md | 7cbaaed318feae7ca8880c65d9ae53815844be48cef9e0a8d6ed78078658d2cf |
| README.md | 245e7b2cf0735ce86a70910fed4dede3c6eed12f85a8d9390ed388560a16dfe6 |
| docs/OPERATIONS.md | 2bb3a4edee81618c908572f42f98abfd82f030400ed945420ca09189da95dce9 |
| docs/openapi.yaml | 2cc4f5f609aa732344014ae99b572b31503bc0ee0cd359fe5cb221c2a5c8cc61 |
| internal/memoryread/recall_search.go | ad73faa29b0f01df98d2157b7facaf10a538435c45a884e3d3b2e3ca3c7ee9bc |
| internal/semanticprojection/hybrid.go | daf0c8ceea1efd50930134a60c6ec1d84616c7d74c35a4aeec407b6cd9c1907d |
| internal/semanticprojection/hybrid_temporal_test.go (untracked) | 23043e2fe52e6f1f135d398422323335e79b3554c73765a8a98791cfcd4fd30c |
| pkg/memory/openapi_contract_test.go (untracked) | 833bb51354b4bd3b1b1dc43d7b1c285bf1ffd363acf12793d9c5bf6d531f1206 |

All eight files were applied byte-for-byte through `apply_patch`, with no
adaptation. This preserves the single captured eligibility time at Recall and
direct Hybrid entry, corrected stable OpenAPI example IDs/hash validation,
terminal message sequence and Unicode TrimSpace rules, and synchronous fatal
semantic-startup documentation. These descriptions remain accurate for this
old runtime; TASK-017 must update backend-specific layout/lifecycle prose.
Earlier accepted audit RED evidence is preserved as history; this integration
does not claim a newly rerun RED experiment.

## Relevance and comparable benchmark input

`lexicalControls` and `lexicalRelevanceQueries` in `lexical_test.go` are the
single test-only corpus and expected-ID source. Expectations were authored in
TASK-001 at `a06906ee7c65de977224aeb091acc76edd1ff9ef` before this task observed
old-engine rank. `legacy_baseline_test.go` reuses them on real Bleve and logs
numeric scores/ranks; it checks relevant ID sets, with the previously documented
old-engine NFC miss. The SQLite gate retains its stronger binary ID tie order.
Do not revise relevance expectations to fit measured rank.

`testdata/benchmark-profile.json` fixes the corpus, metadata, request instant,
filters, synthetic float32 query/chunk vectors and scale recipes for TASK-019.
The 9-document correctness control is distinct from the 16384-document lexical
and 8192-chunk semantic throughput populations. It defines comparable inputs,
not a performance result or a new quality threshold. Real TEI relevance/cost
remains a separate gate.

Old graph reproduction uses a fresh archive of old main plus the complete
accepted audit, with a copy of the same baseline harness and only its two shared
control functions at `/tmp/boyan-48w-oldbaseline.wVAl2a`. It retains old
`go.mod`/`go.sum`, with no SQLite dependency additions. The feature worktree
also runs the harness under its already accepted TASK-001 graph. Raw output is
kept in the temporary evidence artifact. On engine retirement, remove only the
legacy harness/import; preserve corpus, profile, observations and provenance.

Machine: Go 1.26.5, linux/amd64, CGO_ENABLED=1, Linux
6.18.26.1-microsoft-standard-WSL2, AMD Ryzen 7 PRO 7840U with 4 visible logical
CPUs. No latency, allocation, footprint or speedup comparison is claimed here.

Observed old-main ranks/scores from `oldmain-baseline.log` (three searches per
query). Rank is recorded, not prescribed for the replacement engine:

| Query | Old rank: revision (score) |
| --- | --- |
| кошка; дом; quoted кошка | 1: ru (0.49721947675649181) |
| кошка дом | 1: ru (0.49754191827703587) |
| engineer; run; test | 1: en (0.52760192174273357) |
| engineer run test | 1: en (0.57745767432169293) |
| настройка; connection | 1: mixed (0.52760192174273357) |
| настройка connection | 1: mixed (0.54112963243133183) |
| user_id; v1.2 | 1: identifiers (2.0541135299754281) |
| cache-key; HTTP/2 | 1: identifiers (2.7080439465298403) |
| the и | 1: stopwords (4.3064765609635618); 2: en (0.5241628514182689) |
| punctuation | 1: tie-b; 2: tie-a (both 2.8433318013467241) |
| `" OR banana --` | 1: decoy (2.2919219097346821) |
| café; !!! | zero hits |

The feature-graph run returned tie-a before tie-b at the same numeric score;
the initial feature run also returned tie-b before tie-a. This is evidence
against assuming old binary-ID ties. Other observed IDs/scores agree across
the two dependency graphs. The NFC control is intentionally empty on old Bleve
and requires nfc on the SQLite analyzer gate. Required Russian, English,
mixed-language, identifier, punctuation and stopword ID sets pass in both.

Raw old-main baseline log SHA256:
`49affc55a806c1be2c9192f41b33796aa011c3112ad65610c8db99a2b1c0a2a1`.
Benchmark profile SHA256:
`fe0d84bf0f06cb50cec4598976efb47ebfda7aaba0f251ec609b519c6b4c2b51`.

## Integration verification

Source equality was checked after integration with all eight file hashes,
byte comparison of the binary-safe tracked patch and porcelain status, and
branch/HEAD comparison against the captured artifacts. Every comparison
passed. The feature copies also match all eight source hashes. No code or
Beads write targets the main workspace, lock or untracked specification draft.

Focused captured-time/direct-AsOf and OpenAPI domain/schema tests pass, as do
normal relevance controls and race checks for the feature gate, semantic
projection and public memory package. The exact old-main-plus-audit snapshot
also passes its baseline, semantic projection and public-memory race checks.
`go test -count=1 ./...`, `go vet ./...`, pinned `go tool golangci-lint run
./...` (zero issues), `go tool go-arch-lint check --project-path .`, and
`git diff --check` pass. The pinned linter emits its existing gomodguard
deprecation warning. The original pinned `go.mod`/`go.sum` remain unchanged.
Local socket tests
require execution outside the network-restricted sandbox; an EPERM-only first
attempt is retained as environment evidence rather than counted as a pass.

| Change | Requirement/task trace | Invariant impact |
| --- | --- | --- |
| CHG-001 capture and source equality | SEARCH-018, CON-006 / TASK-002 | Source audit bytes/status preserved |
| CHG-002 exact audit integration and two regressions | SEARCH-012,016,018 / TASK-002 | One request instant; public contracts; old startup semantics preserved |
| CHG-003 shared authored controls and old-engine harness/results | SEARCH-003, QUALITY-005, CON-004 / TASK-002 | No claimed score/rank parity; no production adapter |
| CHG-004 deterministic benchmark profile and compact evidence | QUALITY-005 / TASK-002 | Same-input comparison defined; measurements deferred to TASK-019 |
