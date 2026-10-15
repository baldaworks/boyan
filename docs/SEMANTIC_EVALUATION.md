# Semantic retrieval evaluation

The public synthetic corpus in `internal/semanticeval/corpus_test.go` contains
nine hand-authored memories, a superseding residence assertion, and nine fixed
queries. It contains no private or production content. Text, relevance labels,
and thresholds were selected before querying the reference model. The
`revisions` table freezes the exact canonical revision identities produced by
deterministic ingestion, independently of vector inference. Changing corpus
content requires reviewing both the judgments and those identities; do not tune
the fixture to a model's observed ranking.

Every query must retrieve its declared revision within its own top-k. All nine
must pass; there is no average that can hide a failed control.

| Fixture | Expected document | Required top-k |
|---|---|---:|
| `en-exact` | `tea` | 1 |
| `en-paraphrase` | `tea` | 3 |
| `ru-exact` | `bike` | 1 |
| `ru-paraphrase` | `bike` | 3 |
| `mixed-exact` | `backup` | 1 |
| `mixed-paraphrase` | `backup` | 3 |
| `mixed-allergy` | `allergy` | 3 |
| `residence-exact` | `residence` | 1 |
| `semantic-only` | `bike` | 3 |

The coffee, car, log-rotation, and recipe memories are hard distractors sharing
entities and vocabulary with relevant memories. Top-3 inclusion measures
retrievability, not contradiction understanding or an assurance that every
other returned result is relevant. The English `bicycle commuting` control
must have zero lexical candidates and retrieve the Russian commute assertion.

Both suites persist canonical records through the real turn processor, build
the real Bleve projection, construct `boyan.Host`, and use `Host.Search`.
They validate hybrid mode, every healthy query's minimum watermark, exact-scope,
conversation/source/kind/as-of exclusions, update supersession, scope deletion,
and outage fallback with a conflicting uncertifiable minimum. Repeated calls
and a reopened Host compare ordered revision IDs, lexical/semantic ranking
components, and the complete Go retrieval report exactly. Freshness/total
scores depend on Host wall time and are deliberately excluded from this
comparison; fixed-clock total-score determinism remains covered in the lower
level ranking/recall contract tests.

The offline HTTP fake computes vectors from individual bilingual concept words.
It cannot access document IDs, expected results, whole-query lookup tables, or
production ranking helpers. Its vectors test integration contracts only and
provide no evidence about learned multilingual model quality. Run it with:

```bash
go test ./internal/semanticeval -count=1 -v
```

The real evaluation uses the immutable reference
`intfloat/multilingual-e5-base` revision
`d128750597153bb5987e10b1c3493a34e5a4502a`, 768 dimensions, `query: ` and
`passage: ` prefixes. It probes TEI `/info` on the endpoint origin and fails on
an unavailable or mismatched model/revision. A bounded local proxy forwards
the public fixture to TEI and injects a 503 for the outage control without
stopping or altering TEI. Evaluation is actual Boyan retrieval, not raw cosine.

```bash
BOYAN_TEI_EVAL_ENDPOINT=http://127.0.0.1:8080/v1/embeddings \
  go test ./internal/semanticeval -run TestReferenceTEI -count=1 -v
```

The endpoint must expose `/info` with `model_id` and immutable `model_sha`.
Invalid configured endpoints, provider errors, incompatible dimensions,
identity mismatch, failure to become hybrid-ready within 90 seconds, any missed
top-k expectation, or a lifecycle assertion failure make this command fail.
Unset `BOYAN_TEI_EVAL_ENDPOINT` explicitly skips model evaluation; an exit-zero
test invocation containing that skip is not quality evidence.
`TestConfiguredReferenceFailsClosed` checks the standalone test process fails
for malformed, unavailable, and mismatched configured providers and does not
print upstream diagnostic prose.

Each query logs only its public fixture ID, top-k, hit outcome, and elapsed
milliseconds for one `Host.Search` call. Provider deadlines, corpus size and
three evaluation rounds bound work and output. Timings include query inference,
retrieval, and canonical hydration, excluding initial projection creation.
They are observations for this corpus and machine, not service-level objectives
or general quality/throughput guarantees. The immutable image/resources/private
network reference remains documented in `OPERATIONS.md`.

Integrated branch validation and real-provider measurements are recorded with
the task handoff. Prism story closure additionally requires merging all changes
and verifying the merged target branch; local evaluation alone cannot close it.

## Recorded local observation (2026-10-15)

The configured real-provider invocation passed all 27 query outcomes (nine
queries in three rounds), exact repeated/reopened channel rankings/reports,
filters, healthy minimum certificates, update/delete, and injected outage.
The initial eight cases were authored before their first run; the semantic-only
ninth case was added before its first run. No labels or thresholds were adjusted
after observing model rankings. Single-search observed times were 27–38 ms,
and the complete test took 3.06 seconds with an already-loaded model.

The existing local amd64 TEI 1.9.0 container matched image digest
`sha256:bc7ad262695df5b7875b0c9c702deb8e9df3953bdf22c3d6068d9c0429b7b3f3`
and `/info` matched the model/revision above, float32 and mean pooling. Its
container limits were 2 CPUs and 4 GiB RAM; its server admitted 16 concurrent
requests, whereas the shipped Boyan reference profile configures 4. Client
batch 16, server batch 8 requests/8,192 tokens, two tokenizer workers, and
disabled truncation matched. This existing service was reused without starting
or downloading a model. The concurrency difference prevents treating this
timing observation as a measurement of the exact shipped deployment profile;
the fixture is sequential and model-quality identity matched.

A deliberate temporary wrong expected revision for `en-exact` produced
`hit=false`, an absent-from-top-1 diagnostic, and a nonzero test exit in all
three rounds. Restoring the original authored judgment restored the passing
corpus. Configured malformed/unavailable/identity-mismatched reference process
checks fail closed; the unconfigured reference invocation explicitly skips.

A final run concurrent with normal/race/CGO-disabled repository verification
also passed all 27 outcomes and lifecycle assertions, taking 14.94 seconds;
single-search times were 45–642 ms under that competing load. This difference
is why neither observation is a latency guarantee. Integrated normal tests,
race tests, CGO-disabled tests, vet, strict lint, architecture, module verification,
clean `go mod tidy -diff`, Compose rendering, and shipped configuration checks
passed. These are feature-branch observations; merged-target verification is
still required before story closure.
