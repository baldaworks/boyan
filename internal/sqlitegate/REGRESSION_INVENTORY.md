# Search migration regression inventory

TASK015, before engine retirement (TASK016). Original baseline: accepted TASK014
`3f55241220a01e0d561629a651762d0c938f0da7`; resumed integration baseline after
accepted TASK025–028: `c1a509ea5591ce4c3f98f56ffb3c46518ae728af`.
This is an assertion inventory,
not a test-count, execution log, benchmark, TEI result or permission to close the
Story. Existing engines remain available until independent inventory review.
Requirements: SEARCH002,006–018; QUALITY002–006; CON001,006. The retained lexical
and exact-vector controls also exercise SEARCH003–005.

Paths below are repository-relative. Test identifiers are exact Go function
names; multiple identifiers in a cell share the assertion stated in that row.
`SQL` means real pinned SQLite/FTS5/sqlite-vec execution; Manager/Hybrid fixtures
use a fake canonical reader/provider around the real SQL adapter. `Runtime`
and `Host` additionally use actual canonical Badger. Transport-only tests retain
their service seams: they prove serialization/classification, not SQL execution.

## Old lexical adapter contracts

All old groups below are in `index/bleve`. Numerical Bleve scores, analyzer
implementation details and old directory layout are deliberately excluded;
retrieval identities, stable SQLite ties and public score validity are retained.

| Old group and meaningful assertions | Permanent SQL/runtime coverage |
| --- | --- |
| `bleve_projection_test.go`: Russian/English retrieval, generation switch, independent full scope kind+key activation | `index/sqlite/lexical_internal_test.go`: `TestLexicalPreauthoredRelevance`, `TestLexicalFiltersAndTemporalBoundaries`, `TestLexicalSnapshotPinsPhysicalBuildAndTime`; `publication_internal_test.go`: `TestPublicationRequiresCanonicalExactCompleteTuple` |
| `bleve_kind_filter_test.go`: native kind must exclude other kinds | `TestLexicalFiltersAndTemporalBoundaries`; `internal/semanticprojection/sqlite_hybrid_test.go`: `TestSQLiteHybridFiltersAndTemporalBoundaries` |
| `bleve_canonical_applier_test.go`: canonical hydration, resumable delta, missing-revision delete | `canonical_applier_internal_test.go`: `TestCanonicalApplyStagesAndCopies`, `TestCanonicalApplyReplacesMembershipAndDeletesMissing`, `TestCanonicalRejectsStaleAndForeignRecords` |
| `bleve_projection_consistency_test.go`: minimum watermark succeeds only with clean matching canonical/physical generation; durable activation ahead of physical fails; concurrent activation cannot mix a pinned search | `publication_internal_test.go`: `TestPublicationRequiresCanonicalExactCompleteTuple`, `TestPublicationPhysicalReaderRechecksScrubRevocation`; `lexical_internal_test.go`: `TestLexicalCloseCancellationAndUncertifiedMinimum`, `TestLexicalSnapshotPinsPhysicalBuildAndTime`; `store/badger/canonical_sqlite_coordinator_test.go`: `TestSQLiteCoordinatorPublicationInterruptionRecovery` |
| `bleve_projection_certified_test.go`: exact manifest, manifest change rejects certification, explicit channel depth and deterministic tie order | `TestLexicalHybridChannelBound`, `TestLexicalPreauthoredRelevance`, `TestPublicationConsistentPreservesOrdinaryCandidateDepth`; SQL Hybrid final-channel/postcert matrix below |
| `bleve_projection_lifecycle_test.go`: operations and reactivation after Close fail | `index/sqlite/open_internal_test.go`: `TestCloseCancelsSnapshotsDrainsAndRejectsNewWork`; `TestLexicalCloseCancellationAndUncertifiedMinimum`; `TestPublicationLegacyCommitPreservesLifecycleErrors` |

The authored relevance corpus, expected IDs, old engine observations, immutable
benchmark profile and audit hashes remain in [AUDIT_BASELINE.md](AUDIT_BASELINE.md).
`lexical_test.go` and `index/sqlite/TestLexicalPreauthoredRelevance` retain the
Russian, English, mixed-language, identifier, punctuation, stopword, literal
operator, NFC and binary tie controls. `legacy_baseline_test.go` is a measurement
harness/import to retire in016, not the sole keeper of these expectations.

## Old disposable semantic store contracts

Old paths in this table are under `index/semantic`; SQL replacements are under
`index/sqlite`. Grouping preserves distinct assertions within each old file.

| Old file/group and assertions | Permanent coverage or precise exclusion |
| --- | --- |
| `store_test.go`: Stage complete but unpublished; exact view/model; foreign/malformed/incomplete chunk rejection; exact degraded marker; active-pointer CAS; no mixed concurrent generation | `semantic_internal_test.go`: `TestSemanticStageIsCompleteButUnpublished`, `TestSemanticStageRejectsInvalidBeforeWriting`, `TestSemanticStagePinsExactLexicalSource`; `semantic_publish_internal_test.go`: `TestSemanticPublicationCASRetiresOnlyPrevious`, `TestSemanticCompetingPublishersAndOverflow`, `TestSemanticDegradationAndPreciseInvalidation`; `search_snapshot_internal_test.go`: `TestSearchSnapshotBothChannelsDuringPublication` |
| `store_test.go`: reopen orphan repair; reader survives GC; exact8192/one-over and projection bounds; private disposable directory leaves siblings alone | `TestSemanticGarbageOwnershipAndStartupRepair`, `TestSemantic8192ChunksAnd8193RejectedBeforeClaim`, `TestSemanticAdmissionBoundaries`; `TestFreshLexicalOnlyReopenAndPrivateFiles`; runtime legacy-sibling/receipt preservation below. Startup repair is now explicit and quiescent, not an automatic Open sweep |
| `control_bounds_internal_test.go`: full4096-byte escaped scope, ready/degraded control, over-limit rejection before allocation; bounded header encoders | `TestSemanticAdmissionBoundaries` (4096-byte NUL scope, escaped512x256 arrays, reopen/reuse; 4097 rejection); `TestSemanticDegradedControlFailsClosed`; `search_snapshot_integrity_internal_test.go`: `TestSearchSnapshotLargeCorruptControlMetadata`; `internal/semantic/projection_contract_test.go`: `TestPortableProjectionPreflightBoundsAndOwnership`. JSON control-size encoder details are excluded: SQL stores typed columns, with bounded metadata reads |
| `observed_time_test.go`, `portable_test.go`: observed timestamp immutable; returned vectors owned by caller; missing legacy observation never ready/reused | `TestSemanticReusableOwnsVectorsAndRequiresExactView`; `TestSearchSnapshotRejectsCorruptObservedTime`; `TestSearchSnapshotRejectsPortableMetadataCorruption`; `TestSemanticCorruptionRevokesOnlyBadBuildOnEnabledReopen`. SQL has no legacy observation codec; canonical rebuild is the migration |
| `iteration3_internal_test.go`: aggregate64MiB checked before unbounded copying; maximum escaped metadata; epoch ABA and overflow reject unchanged pointer; corrupt epoch fails closed | `TestSemanticExact64MiBAdmissionAndOneByteOver`, `TestSemanticAdmissionBoundaries`, `TestSemanticPublicationEpochGrowthPreservesExactAdmission`, `TestSemanticCompetingPublishersAndOverflow`, `TestSemanticReleasePublishedAndConflictingControl`, `TestSearchSnapshotRejectsStoredShapeCorruption`. Old legacy-build-claim key cleanup is replaced by relational orphan cleanup |
| `recovery_internal_test.go`: canceled/failed Stage preserves active; restart orphan removal; missing/corrupt active state/chunk is never usable; vector binary encoding and encoded budget | `TestSemanticStageCanceledLeavesNoCompleteState`, `TestSemanticCancellationInSecondSQLBatchRollsBackAndReopensIncomplete`, `TestSemanticCorruptionRevokesOnlyBadBuildOnEnabledReopen`, `TestSearchSnapshotRejectsCoverageAndOrdinalCorruption`, `TestSearchSnapshotVectorCorruptionMatrix`, `TestSemanticExact64MiBAdmissionAndOneByteOver`; `internal/sqlitegate/precision_test.go` and `vector_test.go` exercise real BLOB scalar operands. Opaque Badger key hashing is engine-specific, not encryption; SQLite private path/permissions and no stored semantic text replace it |
| `retirement_internal_test.go`: replacement/degrade delete only predecessor; staged loser/other scope/readers survive; cancellation rolls back pointer+retirement; corrupt predecessor fails;8192 retirement | `TestSemanticPublicationCASRetiresOnlyPrevious`, `TestSemanticDegradationAndPreciseInvalidation`, `TestSemanticPublicationCancellationRollsBackPointerAndRetirement`, `TestSemanticRetirementRejectsCorruptPredecessor`, `TestSemantic8192ChunksAnd8193RejectedBeforeClaim`, `TestSemanticReleasePreservesCompetingWinnerAndOtherScopes`, `TestSearchSnapshotBothChannelsDuringPublication` |
| `hardening_internal_test.go`: cancellation after lock and during sort/scan/GC; deep-copy reference fields; bounded orphan cleanup; sibling-chunk metadata must agree; exact64MiB; amplification/header/oversized corrupt values reject before copying; idempotent concurrent close/private files | `TestSemanticCoverageOrderingPreservesCancellationIdentity`, `TestSemanticScopeWaitCancellationAndCloseDrain`, `TestSemanticGarbageCancellationResumesAfterReopen`, `TestSemanticReusableOwnsVectorsAndRequiresExactView`, `TestSemanticStageRejectsInvalidBeforeWriting`, `TestSemanticStageRejectsSiblingRecordMetadataBeforeWriting`, `TestSemanticExact64MiBAdmissionAndOneByteOver`, `TestSearchSnapshotLargeCorruptMetadataPreflight`, `TestSearchSnapshotLargeCorruptControlMetadata`, `TestSearchSnapshotLargeCorruptLexicalFixedFields`, `TestCloseCancelsSnapshotsDrainsAndRejectsNewWork`, `TestFreshLexicalOnlyReopenAndPrivateFiles`; the sibling test independently mutates all ten old fields (scope sequence/kind/category/memory key/CreatedAt/ValidFrom/ValidUntil/ExpiresAt/source IDs/conversation IDs), checks each chunk remains individually valid, rejects before candidate persistence and stages a consistent two-chunk positive control; portable preflight remains in `internal/semantic`. Old callback cadence and JSON item-copy APIs are not runtime SQL APIs |
| `path_keys_internal_test.go`: reject symlinked root/parent/child and special files without touching target; malformed chunk-key index/GC identities | `TestUnsafePathsRejectedBeforeSQLite`, `TestStateDirectoryCannotInjectURIOptions`, `TestCorruptAndUnknownDatabaseBytesRemainUnchanged`; relational FK/unique ownership in `schema_internal_test.go`, ordinal/coverage corruption tests. Badger key/index mismatch fixture is excluded; SQL corruption must fail selected reads, not be quietly repaired into ready |
| `gc_iteration4_internal_test.go`: active ready/degraded ownership survives; canceled multi-page cleanup resumes; malformed/orphan state never becomes ready | `TestSemanticGarbageOwnershipAndStartupRepair`, `TestSemanticGarbageCancellationResumesAfterReopen`, `TestSemanticReleaseRevokedReopenRepair`, `TestSemanticCompleteBuildingStateIsNeverReady`, `TestSearchSnapshotRejectsStoredShapeCorruption`. GC no longer edits active surplus rows: selected-build integrity fails closed and semantic startup revokes bad builds. Old JSON/key scanners and their batch callbacks are engine-specific |
| `gate_internal_test.go`: queued writer progresses under readers; canceled wait returns capacity | `TestPhysicalPoolsEnforceSettingsAndReadOnly`, `TestSearchSnapshotBothChannelsDuringPublication`, `TestSemanticScopeWaitCancellationAndCloseDrain`, `TestSearchSnapshotCallCancellationAndAutomaticDrain`; Hybrid's two-reader cancellation/reentry tests below. Badger's FIFO read/write gate itself is excluded: SQL uses WAL, fixed1-writer/2-reader pools and context-aware admission |

SQL startup distinguishes recoverable semantic invalidity from required database
corruption: malformed semantic rows revoke/degrade that build; required structural
corruption rejects Open without changing original bytes. This deliberately does
not retain old semantic-Badger Open's all-or-nothing failure policy.

## Canonical publication, large records, retirement and owner lifecycle

| Contract / Design correction | Exact permanent decisive assertions |
| --- | --- |
| Physical seal, canonical checkpoint advance/activation and SQL activation failures; exact empty/concurrent anchor; lost physical base cannot certify delta | `store/badger/canonical_sqlite_coordinator_test.go`: `TestSQLiteCoordinatorPublicationInterruptionRecovery`, `TestSQLiteCoordinatorEmptySyncCannotCertifyLostPhysicalState`, `TestSQLiteCoordinatorDeltaCannotReplaceLostBase`, `TestSQLiteCoordinatorRebuildSealsInitialAnchorBeforeNewerPages`, `TestSQLiteCoordinatorPublishesProvenEmptyScope`; `internal/projectionsync/coordinator_commit_test.go`: `TestCoordinatorCommitsExactManifestAtEveryPath`, `TestCoordinatorManifestFailureNeverAdvancesOrActivates` |
| Dirty restart, activation gap, retention floor, safely pinned reader, owned incomplete abort; no accumulated equal-tuple copies | `index/sqlite/publication_internal_test.go`: `TestPublicationCrashReopenAndRetentionFloor`, `TestPublicationPinnedReaderSurvivesRetentionAndPendingOwner`, `TestPublicationAbortCleanupIsPagedAndRetryable`, `TestPublicationAbortPreservesCanonicalPreviousBeforeSQLSwitch`, `TestPublicationAbortAfterCheckpointAdvanceReleasesOwner`, `TestPublicationRetentionDoesNotAccumulateSameTupleCopies`; `publication_cancel_internal_test.go`: `TestPublicationCancellationRollsBackSealSwitchCopyAndAbort`; runtime resumed-delta test below |
| Real canonical2,097,153-byte conversation ingest/hydrate/apply/filter/copy/rebuild/seal/activate/reopen/forget; leading NUL exact identity | `store/badger/canonical_sqlite_budget_probe_test.go`: `TestCanonicalAcceptsConversationBeyondSQLitePageBudget`, `TestCanonicalLeadingNULConversationRetainsExactSQLiteMembership` (actual canonical path and logical row checks, not a convenient reduced fixture) |
|128-record and2MiB normal grouping; target-1/exact/+1; oversized scalar/aggregate memberships isolated; duplicates; normalized+stemmed tokens counted twice; checked overflow; no128-large-record hydration/copy | `canonical_budget_internal_test.go`: `TestCanonicalBudgetTargetEdgesHaveRealTransactionBoundaries`, `TestCanonicalAggregateMembershipOversize`, `TestCanonicalOversizedRecordIsIsolatedAndHydratedAlone`, `TestCanonicalObservedConnectionLimitsAndSingleRecordCopy`, `TestCanonicalAccountingIncludesNormalizedDuplicatedTokensAndOverflow`; `canonical_applier_internal_test.go`: `TestCanonicalApplyLargeSplitAndCanonicalOutsideWriter`; `TestLexicalScopeSizeAndDepth` exceeds8192 lexical rows |
| Effective SQL LENGTH/VARIABLE limits; controlled unrepresentable row is sanitized failure, current transaction rolls back and cannot seal; canonical/prior certificate preserved | `TestCanonicalObservedConnectionLimitsAndSingleRecordCopy`, `TestCanonicalUnrepresentableRecordRollsBackAndSanitizes`, `TestCanonicalBeginSanitizesSQLFailure`; canonical coordinator failure matrix preserves canonical receipts/history |
| Oversized insert/membership/copy/scrub cancellation; all-or-nothing scrub identity validation; finite source/revision parameter pages; exact scope; revoked certificate; zero-chunk coverage; pending/pinned peer preservation | `canonical_cancel_internal_test.go`: both `TestCanonicalCancelDuringOversizedMembershipAndCopy`, `TestCanonicalCancelDuringOversizedScrubPreservesPinnedReader`; `canonical_scrub_internal_test.go`: `TestCanonicalScrubValidatesWholeInputBeforeRemoval`, `TestCanonicalScrubPagesFilterIdentities`, `TestCanonicalScrubZeroChunkCoverageAndState`, `TestCanonicalScrubZeroCoverageUsesSemanticSourceProofWithoutLexicalRows`, `TestCanonicalScrubOversizedSourcePreservesPendingAndPinnedReader`, `TestCanonicalScrubCanceledSemanticPageRetainsSourceProofForRetry`; `semantic_scrub_race_internal_test.go` both original-source/row-reuse proofs |
| Semantic admission remains512-byte identities/256 metadata/4096 scope/2MiB chunk/8192 chunks/64MiB; grouping overhead cannot expand acceptance | `semantic_bounds_internal_test.go`: `TestSemanticAdmissionBoundaries`, `TestSemantic8192ChunksAnd8193RejectedBeforeClaim`, `TestSemanticExact64MiBAdmissionAndOneByteOver`, `TestSemanticSQLInputAccountingMaximumAndGrouping`; `internal/semantic/TestPortableProjectionPreflightBoundsAndOwnership` |
| Exact owner release: missing/repeat; wrong kind/key; partial/complete/empty/zero-coverage; published exact epoch; malformed/ineligible control; no peer/new winner deletion | `semantic_release_internal_test.go` four state-machine tests; `semantic_release_fault_internal_test.go`: `TestSemanticReleaseAfterActualStageFailure`, `TestSemanticReleaseCapturedRowIdentityRejectsABA`; `semantic_release_regression_internal_test.go`: `TestSemanticReleasePreservesCompetingWinnerAndOtherScopes` includes obsolete lexical source still classified Published |
| Every release fault (before revoke, chunk/source/conversation/coverage deletion, control removal), current-page cancel rollback, resumable revoke; publish/revoke both orders; no use-after-close | `TestSemanticReleaseSQLFailureResumesEveryFamily`, `TestSemanticReleaseCancellationRollsBackCurrentPage`, `TestSemanticReleaseCancellationAtRevocationAndControl`, `TestSemanticReleaseRevokedReopenRepair`; all six `semantic_release_race_internal_test.go` tests; `TestSemanticReleaseDeadlineWaitingScope` checks dropped reservation/retry; `TestSemanticReleaseDurablePartialStageRepeated` checks128 durable partial rows and no accumulation across three owners |
| Manager terminal guard on failed Stage/stale/cancel/ambiguous commit/CAS/postcheck; primary error wins; detached one-budget cleanup; Stop joins; peers/prior active survive | `internal/semanticprojection/sqlite_manager_test.go`: `TestSQLiteManagerReleasesEveryStageFailure`, `TestSQLiteManagerReleasesStaleAndCanceledCandidates`, `TestSQLiteManagerResolvesAmbiguousPublication`, `TestSQLiteManagerPreservesCompetingPublicationAndPendingPeer`, `TestSQLiteManagerPostcheckInvalidatesObsoleteSourcePrecisely`, `TestSQLiteManagerDefersCleanupWithPrimaryErrorAndSafeStatus`, `TestSQLiteManagerCleanupSharesDetachedBudget`, `TestSQLiteManagerStopWaitsForTerminalGuard`, `TestSQLiteManagerPreservesPreviousActiveOnAbandonedRebuild`, `TestSQLiteManagerStoreCloseCancelsInFlightGuard` |
| Start excludes admitted pre-Start Repair/provider/partial/complete/terminal owner; one initializer/producer, closed/cancel priority, failed attempt fanout/retry and deferred cleanup recovery | `startup_test.go`: `TestSQLiteStartupWaitsForAlreadyAdmittedProducer`, `TestSQLiteStartupWaitersShareAttemptAndDoNotCancelInitializer`, `TestSQLiteStartupCountsOnlySingleFlightProducer`, `TestSQLiteStartupFailureWakesWaitersAndAllowsRepairAndRetry`, `TestSQLiteStartupClosedPrecedesWaiterCancellation`, `TestSQLiteStartupStopCancelsAndJoinsInitializer`, `TestSQLiteStartupDeadlineCancelsActualSQLAndAllowsRetry`, `TestSQLiteStartupReopenRecoversDeferredAbandonedCandidate`; `startup_regression_test.go`: both waiter tests retain canceled-completed priority and original failure object after later success |
| Driver rollback finishes before write/snapshot return, Store/Manager drain and Close; failed Begin releases connection; original error identity survives | `index/sqlite/write_lifecycle_internal_test.go`: all four `TestWriteLifecycle*`; `search_snapshot_lifecycle_internal_test.go`: all three `TestSearchSnapshot*` actual-driver gate cases |

No physical-erasure or2MiB delete/WAL/heap/time quota is inferred. Successful
cleanup means actual logical rows gone; deferred cleanup remains reported and
requires a truly quiescent future startup. Tests never global-sweep a paused peer
to make a runtime fixture pass.

## Hybrid, exact SQL, audit timing and public privacy

| Old/new assertion group | Permanent decisive coverage |
| --- | --- |
| Old `hybrid_test.go`, `hybrid_bounds_test.go`: semantic-only and overlap survive caller limit; filter before cosine, max over every chunk/window before revision top-k; independent100+100/RRF60/union200; lexical fallback depth256 | `index/sqlite/search_snapshot_internal_test.go`: `TestSearchSnapshotExactRevisionOracleAndPlan`, `TestSearchSnapshotSemanticFiltersAndTime`, `TestSearchSnapshotSemanticChannelAndQueryBounds`, `TestSearchSnapshotRejectsOversizedSelectedBuildBeforeVectorScalars`; `internal/semanticprojection/sqlite_hybrid_test.go`: `TestSQLiteHybridBoundsIndependentChannelsAndRRF`, `TestSQLiteHybridFiltersAndTemporalBoundaries`; lexical depth tests above; pure `internal/semantic` vector/fusion tests remain unchanged |
| Old model/view incompatibility, outage/malformed vectors, invalid requests/cancellation/canonical failures, including empty strict minimum | `TestSQLiteHybridIncompatibleAndInvalidProviderFallback`, `TestSQLiteHybridRejectsInvalidAndCanceledRequests`, `TestSQLiteHybridErrorPolicy`, `TestSQLiteHybridOutagePropagatesSanitizedAuthorityFailures`, `TestSQLiteHybridSanitizesWrappedContextErrors`, `TestSQLiteHybridRejectsChangedCertificatesIncludingEmptyMinimum`; all errors return nil hits/report and preserve classified cause |
| Old report independent contributions, deterministic reopen, sanitized fallback and request-local empty report | `TestSQLiteHybridReadsMatchingCompletePair` asserts literal1/61+1/61; `internal/semanticeval/TestDeterministicHybridCorpus` compares full ranks/reports twice and after reopen; `internal/memoryread/TestRecallKeepsRequestLocalReportEvenForEmptyResults`; transport serialization below |
| Audit direct Hybrid captured-time before slow inference, explicit AsOf, caller immutability | `TestSQLiteHybridPinsEffectiveTimeAndExplicitAsOf` advances injected clock during actual provider phase; expiry equality returns zero; `TestSQLiteHybridPreflightPinsEntryAsOf` independently advances clock at pool wait/Close, both ready/fallback paths and explicit time |
| Audit RecallService captured-time before canonical dependency delay and inference, then canonical hydration | `sqlite_temporal_regression_test.go`: `TestSQLiteRecallPinsEligibilityBeforeSlowDependencies` uses actual SQL and fake time crossing expiry twice; response must contain canonical text and hybrid report. This is the SQL successor to `hybrid_temporal_test.go/TestRecallPinsHybridEligibilityBeforeSlowDependencies`; archived source stays unchanged |
| Canonical hydration rejects stale forgotten/denied/expired/foreign/superseded identities; projection never supplies text | `TestSQLiteHybridCanonicalHydrationBarrier`; `internal/memoryread/recall_hybrid_authority_test.go`: `TestSemanticOnlyCanonicalAuthorityDuringChanges`; new runtime denied/replay/restore/erase test below; retained canonical privacy/receipt suites are still authoritative |
| §15 negative metadata: missing/degraded/wrong model/dimensions/source/scope/physical/incomplete/corrupt; zero inference calls, ordinary lexical vs strict retryable conflict | `sqlite_hybrid_preflight_test.go`: `TestSQLiteHybridPreflightSkipsProviderForUnavailableMetadata` asserts exact calls/fallback/report/scanned counts and strict nil outputs |
| §15 open vs SemanticState vs Close classification; context wins; primary error vs Close; negative metadata cannot conceal authority or lexical errors | `TestSQLiteHybridPreflightErrorBoundaries`, `TestSQLiteHybridPreflightAuthorityPriority`, `TestSQLiteHybridPreflightFallbackHydrationFailure`, `TestSQLiteHybridPreflightActualSQLErrors`; real table-drop errors exercise actual SQLite failure, not message matching |
| §15 probe is metadata only, explicitly closed; provider reenters both real readers; vectors precede final shared channel view; independent postread recertification | `TestSQLiteHybridFinalChannelSnapshotFollowsVectors`, `TestSQLiteHybridProviderCanReenterBothReadersAfterPreflight`, `TestSQLiteHybridSharesTransactionAcrossConcurrentPublication`; `wrapHybridChannelRead` asserts provider return, one lexical+dense channel call, metadata-only pre/post views and exactly one Close per phase |
| §15 occupied reader pool can delay probe; caller deadline/cancel or Store close drains; no provider/identity/report; capacity reacquirable | `TestSQLiteHybridPreflightPoolWaitCancellation`, `TestSQLiteHybridPreflightStoreCloseDrainsWaiter`, `TestSQLiteHybridClosesTransactionsOnFailure` |
| §15 negative then ready may conservatively fallback; positive then changed provider-time source/model/physical/epoch checked fresh; final-channel Close mutations remain at final phase | `TestSQLiteHybridPreflightPublicationRaces` includes ordinary/minimum and empty/populated; `TestSQLiteHybridRejectsChangedCertificatesIncludingEmptyMinimum` independently mutates canonical/manifest/physical/same-tuple-physical/epoch/model/completeness after final channels |

TASK013's provider-before-every-SQL pool expectation was explicitly superseded by
Design§15/TASK024; its substantive resource invariant remains provider reentry
after the closed probe. No old post-channel test was moved to preflight. The
original all-Embed-blocking runtime regression remains
`standalone_semantic_test.go/TestSemanticQueueOverflowDoesNotFailLexicalSync`:
both workers blocked, queue64 full, lexical Sync and recall succeed before fake
release, overflow status retained. Whole-file SHA256 remains
`f435e9bceac3b817642e737d95dec34e406b1a9bc8bbcd4784cbf25b70c0cbfd`.

## Runtime, Host, HTTP, MCP and canonical compatibility

| Meaningful contract | Permanent test and assertions |
| --- | --- |
| Fake-provider full ingest/reopen/replacement/degrade/forget; deterministic rank/reports, semantic-only paraphrase, strict minimum, all public filters | `internal/semanticeval/TestDeterministicHybridCorpus`: unchanged corpus/revision IDs, response validation and ranks across repeated/reopened Host, replacement removes prior revision, provider outage preserves lexical readiness/conflicts minimum, scope forget returns empty. `fixture_test.go/seed` now builds actual SQLite checkpoint before the actual SQLite Host; `TestReferenceTEI` shares that seed, but its real-provider gate is separate and not claimed here |
| Source deny while SQL stale; reopen and replay retain receipt; restored canonical privacy cannot resurrect text; populated restored-runtime erase must remove all eight row families synchronously | `internal/standaloneruntime/standalone_sqlite_privacy_test.go`: `TestSQLiteRuntimeDeniedReplayRestoreAndErase` retains denied/replay/restore/receipt checks, creates an independent eligible turn in the same restored target scope, proves passage inference, positive publication epoch, healthy hybrid recall and positive counts in every row family before erase. Actual public ForgetScope and identical replay each require immediate eight-family zero across all builds and empty hydrated recall. Historical strengthened F1 was RED before TASK025–028; resumed results are recorded separately, without a new TASK015 product-fix/TDD claim. No post-return polling, component scrub, global repair or provider wait supplies removal |
| Provider outage/deadline/invalid vectors async repair; canonical change invalidates old readiness; source forget removes only target; all-blocking shutdown | `TestStandaloneSemanticOutageRepairAndSourceForget`, `TestStandaloneSemanticShutdownCancelsBlockedInference`; `TestCanceledLexicalSyncPreservesHealthySemanticReadiness`, `TestFailedLexicalSyncInvalidatesHealthySemanticReadiness`, `TestSemanticRefreshRequiresLexicalSuccess` |
| Required lexical repair before readiness, lost projection/reopen; additive sqlite checkpoint leaves old checkpoint/receipt and legacy siblings byte-identical | `TestSQLiteRuntimeReopenAndMissingProjectionRecovery`; `pkg/boyan/TestPublicHostRecoversProjectionBeforeReady` now removes actual SQL file/marks actual sqlite checkpoint dirty, checks immediate minimum recall and unchanged canonical scope version/history |
| Corrupt required file original bytes untouched and actionable backup/rebuild error; unrepaired physical failure cannot start | `TestSQLiteRuntimeCorruptRequiredFileIsPreserved`, `TestSQLiteRuntimeUnrepairableLexicalFailureHasRecoveryGuidance`; SQL security/integrity tests above |
| Semantic startup/cleanup failure keeps lexical ready, failed stage releases every owned row; Stop deadline retains storage through outstanding producer; fresh start repairs safely | `TestSQLiteRuntimeSemanticInitializerFailureKeepsLexicalReady`, `TestSQLiteRuntimeFailedStageReleasesOwnedCandidate`, `TestSQLiteRuntimeShutdownDeadlineRetainsSharedStorage`; Manager deferred/persistent-error and startup-reopen tests above |
| Required resumed delta aborts before replacement to avoid ownership conflict, preserves previous/active until switch | `TestSQLiteRuntimeResumedDeltaAbortsBeforeReplacement` uses real coordinator marker and asserts retained physical IDs/certified four-record replacement |
| Disabled constructor resolves no credential/provider, creates no semantic workers/tables; enabled uses same SQL store, private files, no credential persistence; disabling old state makes no provider calls | `pkg/boyan/config_embeddings_test.go`: `TestNewDisabledEmbeddingsIgnoreInvalidFieldsAndCreateNoSemanticState`, `TestEmbeddingsConfigNormalizeDisabledIsInert`, `TestEmbeddingsConfigCredentialIsInertWhenDisabledOrUnnamed`, `TestConfigValidateIncludesEmbeddingShapeWithoutResolvingCredential`, `TestNewEnabledConfigOpensPrivateSemanticStateWithoutPersistingCredential`; `TestSQLiteRuntimeOptionalSemanticSharesStorage` explicitly counts semantic tables and no calls after disabling corrupt old vectors |
| Host optional reports/status/header and ordinary/minimum errors stay sanitized; blocked passage build doesn't block readiness; credentials resolved at construction; restart/forget | `pkg/boyan/TestHostSemanticLifecycle`, `TestHostSemanticRepairFallbackRecoveryAndRestart` exercise loopback fake provider, malformed/private error prose, readyz optional headers, default absence, strict cold conflict and healthy/recovered/forgotten state |
| Host/MCP native replacement/history/temporal/forget/trace; exact scope and current canonical text | `pkg/boyan/TestPublicHostAndMCPNativeMemoryLifecycle`; shared `persistIntegrationTurn` now seeds SQLite, retaining unchanged public assertions |
| Pending erase completes before rebuilding lost SQL file; foreign same-key kind survives; text/trace/payload cannot return; restore loading never advances erase and validated restore resumes | `pkg/boyan/TestPublicHostFinishesEraseBeforeRebuildingLostIndex` now removes actual SQL file; `TestStandaloneStartupResumesPendingEraseBeforeProjection`, `TestStandaloneLoadingRestoreDoesNotAdvanceErase`, `TestStandaloneValidatedRestoreResumesBeforeErase` |
| HTTP ingestion validation/classification/status/retry contract, receipts and safe logs; SDK optional report/minimum | `internal/httpingest/TestHandlerValidationOriginReturnsBadRequest`, `TestHandlerExecutionErrorsPreserveStatuses`, `TestHandlerStrictJSONRejectsBeforeIngestion`, `TestWriteSafeErrorPreservesMarkedClassificationAndRetryMetadata`; `client/http` receipt binding/preservation tests; `internal/standaloneruntime` HTTP logging/limit tests; public search is Host/MCP, not a new REST search endpoint |
| MCP default/optional report, minima, empty/filter/foreign-scope rejection; forget binding/content-free acknowledgements | `mcp/TestSearchSDKPreservesPublicRetrievalReport`, `TestSearchMinimumWatermarkThroughSDK`, `TestSearchRejectsFilterMismatchedReferenceThroughSDK`, `TestSearchRejectsForeignReturnedScope`, `TestForgetRequestBindingThroughSDK`, `TestForgetAcknowledgementUsesDurableOutcomeTimestamp`, `TestForgetSourceUsesServerOwnedTimestampAndContentFreeOutcome`; public `pkg/memory` request/response/OpenAPI tests retained |
| Canonical replay/restore/erase/deny authority and operation receipt byte compatibility | Existing `store/badger` canonical operation/outcome, forget timestamp, privacy restore/native+logical roundtrip, erase-fence/journal/source-guard and source-denial tests remain unchanged. SQL migration adds new projection identity only; runtime receipt/replay and actual restore assertions above connect that authority to SQL recall |

## Reviewer overlay provenance and disposition

Inputs are immutable `/tmp/boyan-48w-task22-review-probe_test.go`,
`task13-review1-probe_test.go`, `task23-review1-probe_test.go` and
`task23-review1-write_probe_test.go` with their named review reports; prefix every
short path here with `/tmp/boyan-48w-`. No archived overlay file is changed.

| Probe | Permanent assertion / disposition |
| --- | --- |
|22 partial-cancel stage cleanup repeated | Promoted `TestSemanticReleaseDurablePartialStageRepeated`: exactly128 previously committed chunks survive second-page cancel; exact release removes control/coverage/chunks and all memberships; three owners leave zero accumulated rows |
|22 competing winner/peer/two other full scopes; obsolete-source Published | Promoted `TestSemanticReleasePreservesCompetingWinnerAndOtherScopes`: loser CAS conflicts; owner release preserves four protected builds; changed lexical watermark still returns active exact epoch1 Published |
|22 deadline waiting scope and reservation drain | Promoted `TestSemanticReleaseDeadlineWaitingScope`: deadline identity, empty result, no reservation, exact retry succeeds |
|13 four query windows plus omission counts | Already permanent `TestSQLiteHybridRetainsReviewerChannelContracts/four-windows`; valid hybrid report with nonzero omitted windows/runes |
|13 UpdatedAt-only canonical change | Same test `/timestamp-only`: strict conflict after final channel Close, even unchanged logical tuple |
|13 snapshot Close failure cause and privacy | Promoted `TestSQLiteHybridFinalChannelClosePreservesFailure`: final-channel Close returns no identities/report, preserves cause and StoreFailure classification, sanitizes private prose and releases both readers; `TestSQLiteHybridPreflightErrorBoundaries` separately distinguishes positive/negative preflight Close errors |
|13 ordinary changed pair discards dense | `TestSQLiteHybridRetainsReviewerChannelContracts/changed-pair`: final-channel Close removes semantic pointer; ordinary response degraded with zero dense contribution/count |
|23 canceled waiter beats completed success/failure, both Start/Repair | Promoted `TestSQLiteStartupWaiterContextPrecedesCompletedResult`: deterministic joiner paused until initializer result published, canceled caller still receives context.Canceled |
|23 waiter keeps first failed attempt identity after independent successful Start | Promoted `TestSQLiteStartupWaiterRetainsFailureAfterLaterSuccessfulAttempt`: exact error object retained for both waiting APIs and exactly two repair attempts |
|23 write driver rollback drain | Already permanent `TestWriteLifecycleWaitsForAlreadyStartedRollback`, `TestWriteLifecycleManagerStartupWaitsForRollback`; real pinned SQLite driver gates actual rollback. Original P1 RED and reviewed fix provenance remain in TASK023 reports; this task does not claim a new production fix |

## Canonical-denial integration — Design §16 / accepted TASK025–028

These are additional assertion mappings, not replacements for the original
publication, missing/dirty/lost-index, temporal, saturation, permission, safe-error,
portable-budget, release or startup controls above. The accepted component chain
is TASK025 `d90f937`, TASK026 `c612963`, TASK027 `a651aa4`, TASK028 `c1a509e`.
Results and complete TASK015 source hashes belong in the resumed implementation
report; names here identify the assertions and their primary owners.

Historical F1 attribution is corrected: canonical erase deletes payloads and
active heads, but structural revisions, immutable Evidence, denials and original
journals/outcomes survive. Missing required structure is an error, never proof
of denial. The observed gap was retained projection material after public success;
it was not observed text disclosure. No claim of physical WAL/MVCC/legacy-byte
erasure follows from logical row counts.

| Owner / invariant | Permanent decisive evidence and preserved limits |
| --- | --- |
|025 mandatory immutable authority: nil/typed nil/value-level noncomparability fail; same comparable instance rebinds; another fails permanently; failure doesn't close caller resources or adopt a peer stage | `index/sqlite/canonical_privacy_internal_test.go`: `TestCanonicalPrivacyBinding`; `canonical_privacy_lifecycle_internal_test.go`: `TestCanonicalPrivacyClosingStoreCannotBind`. `TestCanonicalPrivacyUnboundMaterialWrite` rejects the listed material paths before rows/owners are claimed. It tests material Stage; it is **not** evidence for a separate unbound-empty Stage case |
|025 all material entry points use fresh authority under the full scope lease, before SQL mutation, and close source reads before canonical callbacks | `TestCanonicalPrivacyGuardsEveryMaterialProducer` exercises apply/rebuild/incremental-copy/commit-lazy-copy and admissible chunk Stage against direct revision, immutable Evidence and concrete membership denial; `TestCanonicalPrivacyCallsOutsideSQLUnderScope` asserts scope ownership and all SQL connections free during callbacks. `TestCanonicalPrivacyAuthorityErrorsAndFreshAdmission` covers missing/wrong/failed authority, unknown source, cancellation and denial after scope reacquisition. Invalid zero-count Stage cases prove portable rejection, not privacy-branch sensitivity; coverage/chunk loops can supply the same rejection, so no per-branch mutation-completeness claim is made |
|025 guarded producer inventory and durable sealing fences | Lexical material INSERTs (`lexical_record`, FTS, source/conversation) are reachable through scoped `lexicalBuffer` → `writeInputs` → `lexicalInput.write`; apply/rebuild and both active-copy paths admit each revision/source first. Semantic coverage/chunk/member INSERTs are reachable through guarded Stage before claim, with scope held through final complete update. `TestCanonicalPrivacyRevokedCannotLazyCopy` covers source and destination revocation; `TestCanonicalPrivacyFailurePreservesLastCertifiedView` protects the previous certificate. Existing seal/resume/activate/publish/CAS tests above remain distinct. Empty degraded-control creation and delete-only GC/release/retention are not material producers; LoadReusable grants no later Stage authority |
|025 actual canonical authority versus explicit synthetic fixtures | `internal/standaloneruntime/TestStandaloneCanonicalPrivacyBindingUsesRealAuthority` rejects a late portable-valid Stage using a real durable denial and independently current source. Runtime/canonical coordinator/restore/public fixtures use the actual canonicalStore pointer; TASK015's evaluation seed and native Host seed pass that same actual store as constructor argument3. New SQL-only regression fixtures register their actual known revisions/sources with `registerSemanticPrivacy`; unknown identities remain errors, with no unbound/allow-all bypass |
|026 exact denial authority, all retained owners/eight families, zero chunks, safe readers and peer/newer-winner preservation | `canonical_denial_reconcile_internal_test.go`: `TestCanonicalDenialReconcileIdentityAuthority`, `TestCanonicalDenialReconcileAllFamiliesAndOwners`, `TestCanonicalDenialReconcileZeroCoverageAfterLostSQLProof`, `TestCanonicalDenialReconcileRealPublicationAndLatePublish`. Actual direct denial, mixed immutable Evidence and SQL membership select targets; zero coverage remains removable after its last SQL source proof is gone. Active positive epoch is degraded exactly, pending target revoked, unrelated complete/empty/kind peers and pinned MVCC reader survive; independent healthy CAS replacement survives repeat reconciliation |
|026 finite progress and exact captured ownership/proof predicates | `TestCanonicalDenialReconcileBoundedPagesAndMemberships` exceeds8192 lexical rows and128 preserved-prefix rows, preserves130 eligible rows, streams aggregate membership fanout above2MiB and checks callbacks outside SQL under the lease; `TestCanonicalDenialReconcileProofByteGroups` separately accounts for large Evidence witnesses; `TestCanonicalDenialReconcileCandidateRewrite` preserves rewritten rowid/owner/revision/item/scope/source/ordinal/epoch peers.128/normal2MiB are scheduling bounds with one oversized item isolated, not hard delete/WAL/heap/time quotas |
|026 required fault/error/Close/context/drain contract | `canonical_denial_fault_internal_test.go`: `TestCanonicalDenialReconcileFailureBoundaries`, `TestCanonicalDenialReconcilePageCancellation`, `TestCanonicalDenialReconcileSnapshotCloseError`, `TestCanonicalDenialReconcileReadCloseErrors`, `TestCanonicalDenialReconcileBusyIsRetryable`, `TestCanonicalDenialReconcileCloseDrains`. Actual SQL failures before/after revoke and each family, page rollback/prior revocation, unsafe orphan FTS, missing/wrong structure, primary-read versus secondary Close, candidate/membership/snapshot Close, retryable busy and scope/read/canonical/page Close drain remain backend evidence; no successful cleanup after failed Close or unknown authority |
|026 persisted scopes and disabled metadata-only cleanup | `canonical_denial_disabled_internal_test.go`: `TestCanonicalDenialReconcileAllPersistedScopes` crosses128 scope tuples including semantic-only/retained-only and same-key/different-kind; `TestCanonicalDenialReconcileDisabledMetadataOnly` preserves fresh schema absence and undenied invalid-vector bytes on old disabled reopen while removing proven denied targets at the correct epoch. No vector decoding/model readiness/provider/credential/worker is needed |
|027 journal-first current-runner migrated-source retry and preserved compatibility | `internal/forgetops/source_journal_retry_test.go`: `TestSourceJournalRetryWithoutPayload`, `TestSourceJournalRetryFinishesPendingOutcome` use a supported imported archived source with actual Badger. First erase removes payload but preserves structure/journal; same-process/reopen repeat returns exact original outcome; Outcome-before-Complete still runs to Complete. `source_journal_compat_test.go/TestSourceJournalCompatibility` preserves native/fresh resolution, bounded legacy adoption, truncated legacy rejection, full SourceRef/scope/kind/fingerprint corruption controls, context/error sanitization and no-mutation rejection. Unknown no-journal historical migrated mappings are not claimed repaired; no TurnSourceID guess |
|028 joined public scope erase/replay independent of blocked provider | `internal/standaloneruntime/standalone_denial_completion_test.go/TestStandaloneDenialPublicScopeAndReplay`: actual canonical processor, passage calls, positive published epoch, healthy two-item Hybrid, distinct retained lexical owners, eight positive families → actual public ForgetScope/replay each immediate eight-family zero while third passage build stays blocked. TASK015 strong F1 adds its separate denied→reopen→receipt→logical restore→independent eligible turn lifecycle; it is retained rather than replaced by this prerequisite |
|028 source-only/native+migrated, later eligible turns, historical/mixed/zero coverage and truncated large scope | `TestStandaloneDenialSourceAndLaterTurn` admits later canonical material after the original fence and establishes a healthy independent winner before cleanup; target becomes zero, peer/later remain positive in all eight families with healthy recall and equal replay outcome, including migrated same-SQL restart. `standalone_denial_interleaving_test.go/TestStandaloneDenialHistoricalMixedAndZeroChunks` keeps structural history/mixed Evidence and lost-source-proof zero coverage. `TestStandaloneDenialTruncatedLargeScope` uses actual8200 canonical/lexical revisions, empty truncated references and disabled semantic-schema absence; it checks four lexical families without applying the semantic cap |
|028 same-SQL restart distinct from fresh logical restore; required startup order | `standalone_denial_recovery_test.go`: `TestStandaloneDenialCompletedJournalSameSQLRestart`, `TestStandaloneDenialInterruptedJoinSameSQLRestart`, `TestStandaloneDenialPendingJournalRecovery` keep populated SQL across completed/pending journals, interrupted retirement/Sync and later turns. Required cleanup precedes Ready/lexical certification; outcome/receipt remain original. `TestStandaloneDenialLogicalRestoreFreshSQL` independently proves fresh archive reconstruction, not same-SQL retirement. `TestStandaloneDenialStartupOrder` uses real DELETE failure plus disabled invalid vectors; `TestStandaloneDenialInvalidOwnerStopsStartup` rejects unsafe ownership before Ready. Optional initializer failure remains independently covered by the accepted canonical-valid fixture |
|028 original outcome plus required error; public Host/HTTP MCP shape | `standalone_denial_errors_test.go/TestStandaloneDenialPublicErrors` covers actual SQL failure/busy plus context and classified read/Close/canonical/Sync boundary propagation after canonical completion, then real joined retry. `standalone_forget_projection_test.go/TestForgetProjectionSyncerOrderAndErrors` covers validation/order/first-error/context priority. `pkg/boyan/host_denial_completion_test.go/TestHostDenialCompletionErrors` uses actual public Go Host and its Streamable HTTP MCP handler: original outcome plus sanitized Go error, content-free MCP IsError, no success partial marker, exact retry/replay identity. Low-level Close faults are026 evidence consumed here, not duplicated |
|028 late producers/pending publications and tracked shutdown | `standalone_denial_interleaving_test.go`: `TestStandaloneDenialLateProducerOrderings` composes an already scoped paused hydration owner, actual public completion and late portable Stage rejection. Its immediate negative select is **not independent proof of reconciliation's scope wait**, because next Sync also waits for ownerDone; actual scope-exclusion proof belongs to026. `TestStandaloneDenialPendingPublicationAndPeers` rejects target Publish and revoked lexical resume/activate while preserving independent complete/empty pending owners. `TestStandaloneDenialShutdownDrains` joins a service-owned producer and tracked public forget before storage closes, then completes same-SQL startup retirement; primitive read/callback/page/Close coverage remains026 |

Accepted portable zero-count policy remains unchanged: identity-free bound empty
Stage is valid; a coverage record with ChunkCount0 is rejected before claim.
Persisted zero-coverage recovery fixtures deliberately use raw SQL, not a new
Stage admission promise. Accepted semantic8192/64MiB and all original channel,
queue, worker, deadline and public bounds remain in force. The pre-existing
variable-length custom ItemID scan-order issue `boyan-3bz` remains outside these
claims; normal runtime fixtures use generated StableID. No scanner fix is claimed.

TASK025's producer matrix, TASK026's primitive failures and TASK027's journal
permutations are reused because TASK028 consumes those exact capabilities;
copying them into runtime tests would add no independent boundary assertion.
Conversely strong TASK015 F1, F2's ten-field sibling admission matrix, original
all-Embed saturation and final-channel/postcertification probes retain their
distinct assertions. Existing `gomodguard` deprecation remains a tooling warning,
not a zero-warning verification claim. Component evidence does not replace
required TEI, comparable cost benchmarks, final delivery review or merged-state
verification.

## TASK016 deletion handoff

The adapters/tests have **not** been removed by this task. Remaining direct old
test imports are intentional checkpoint inputs, not hidden runtime dependencies:

| Remaining old fixture/harness dependency | Retirement action preserving meaning |
| --- | --- |
| `internal/sqlitegate/legacy_baseline_test.go` imports both old engines | Remove old measurement harness/import only; retain authored controls/profile/observations/AUDIT_BASELINE |
| `internal/semanticprojection/hybrid_test.go`, `hybrid_bounds_test.go`, `hybrid_report_test.go`, `hybrid_recovery_test.go`, `hybrid_temporal_test.go`, `ports_test.go` and their LegacyHybrid helpers | Remove duplicates only against the SQL/fusion/report/timing rows above; legacy observed-codec upgrade and LegacySearchReader typed-nil constructor are engine-only exclusions. Direct audited timing has explicit actual-SQL successors; retain audit source hash history |
| `manager_test.go/openSemanticStore`, fake manifests/default old identity, `repair_loop2/3/4_test.go`, `operational_test.go`, `retirement_test.go` | Preserve backend-neutral scheduler/single-flight/scan/pre-inference budget/temporal/precise-invalidation assertions when mechanically replacing old fixture plumbing. These tests are **not blanket-deletion candidates**: e.g. queue refilling, same-scope fairness, 8192 scanned-ineligible records and high-dimensional pre-inference budget are distinctive orchestration controls. SQL Manager/Runtime owner/startup tests above cover actual storage terminal behavior; do not substitute those for scheduler assertions |
| `internal/projection/projection_runtime_retention_test.go`, `projection_runtime_sync_test.go` old applier fixtures | Port shared fixture to SQL or delete only an assertion explicitly duplicated by SQL publication/coordinator/runtime matrix; preserve replay-only portable applier/startup/retention reconciliation order tests |
| `internal/memoryread/recall_interleaving_test.go` old consistent-projection fixture | Mechanically port to actual SQL Store/applier, retain first-call retryable conflict and second-call two-record successful control; canonical time/text interfaces unchanged |
| `pkg/boyan/host_projection_recovery_test.go/replaceStartupV1Index` old v1 mapping | Engine-specific old schema fixture is excluded from SQL schema compatibility. Retained here pending016; actual SQL dirty/missing/never-created repair controls and unchanged canonical assertions now execute. Legacy-file nonmutation is independently asserted by runtime SHA/receipt test; SQL unknown/newer versions must fail closed rather than pretend to be a supported v1 upgrade |

TASK016 must audit `rg 'index/(bleve|semantic)|blevesearch|NewLegacyHybrid'`
including tests and module/architecture files, port the retained fixture plumbing,
and rerun affected tests. This inventory authorizes no deletion itself. TASK017
owns operator prose; TASK019 comparable benchmarks and real pinned TEI/merged-state
Verify remain separate gates. Local test success cannot close the Story.

## TASK016 retirement follow-through

The TASK015 handoff above remains historical. TASK016 ports all38 neutral tests
in manager, repair_loop2/3/4, operational and retirement to the real SQL Store.
`manager_sqlite_fixture_test.go` supplies an actual sealed empty lexical tuple
and the independently known fake canonical population through a
`neutralFixturePrivacy` pointer. It explicitly registers each fixture write's
known population and preserves history using the accepted fixture representation;
per-ID checks do not repeatedly import all8192 records. The existing shared
mutable SQL/Hybrid fixture is unchanged. Unknown identities remain errors. Queue refill,
same-scope fairness,8192 scanned-ineligible records, high-dimensional admission
before inference, single-flight, temporal and precise invalidation assertions
remain. Refresh/forget now holds a real SQL SearchSnapshot across replacements.
`TestVerifyPublishGapRacePublishesThenInvalidatesStaleGeneration` captures the
actual ready owner and requires that same owner/positive epoch degraded with
zero chunks/coverage. SQL exact invalidation retains epoch1; old epoch2 was the
retired adapter's replacement-marker detail, not an extra publication requirement.

`projection_runtime_sync_test.go` and `projection_runtime_retention_test.go` now
use real SQL with the actual canonical Store authority. Replay-only applier,
resumed delta, dirty restart, exact scope, active+previous retention, concurrent
peer search and canonical-first activation before pruning remain. SQL row counts
replace old directory/marker checks. The old generation-directory symlink
pruning fixture is engine-specific; `TestUnsafePathsRejectedBeforeSQLite`
independently preserves SQLite's path protections. A crashed incomplete owner,
complete canonical future, live in-process owner and exact abort are checked as
distinct SQL rows. Missing physical state still rebuilds without canonical change.

`recall_interleaving_test.go` uses actual SQL and retains first-call retryable
conflict then two-record/sequence2 retry. The additional imports discovered in
canonicalcore were ported with actual canonical authority: current-head AsOf,
truncated large-forget/later-turn/replay and beyond-first-page kind filters.
`TestKindFilterSurvivesMissingSQLiteRebuild` replaces the old v1 mapping fixture;
the excluded mapping implementation is not a supported SQL upgrade. Host keeps
actual missing/dirty/never-created cases; SQL unknown/newer-version failure and
runtime legacy-file nonmutation remain unchanged.

The old lexical/store tables and Hybrid rows above map the71 retired package
source files and duplicate LegacyHybrid tests to exact permanent assertions.
`TestSQLiteHybridFallbackBoundsHydrationAtLexicalMaximum` separately retains
the old fallback hydration bound:300 real lexical rows produce256 ordinary SQL
candidates, but missing-semantic Hybrid fallback returns exactly200 in stable
order, reports100 lexical candidates/zero dense/scanned, and invokes no provider.
This is distinct from healthy-channel fusion's200 bound.
Only the old observed-codec upgrade and LegacySearchReader typed-nil constructor
are engine exclusions. The SQL typed-nil BuildStore and required lifecycle tests
remain, including both default and explicitly composed SQLite identity. Shared
`delayedRecallReader` and `queryProviderFunc` survive for actual SQL timing/error
successors. Authored lexical controls, profile, observations and AUDIT_BASELINE
remain after removing the old measurement harness. The old measurement archive
is retained for TASK019; no new benchmark or real-TEI result is claimed here.

The final Manager requires exact CandidateLifecycle; legacy nil-lifecycle/GC,
replacement-marker fallback and default legacy identity branches are retired.
Canonical Badger, all portable semantic values and candidate-release contracts,
the accepted SQL privacy/recovery components and original all-Embed saturation
regression remain. Import/module/runtime graph checks and native results are
recorded in the TASK016 implementation report/ledger; this inventory is not
independent acceptance or merged-state verification.
