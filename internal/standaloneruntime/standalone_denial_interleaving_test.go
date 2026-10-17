package standaloneruntime //nolint:testpackage // Real producer admission and service drain interleavings.

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryops"
	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:funlen // Historical and mixed authority, persisted zero coverage and public retirement form one fixture.
func TestStandaloneDenialHistoricalMixedAndZeroChunks(t *testing.T) {
	r, err := New(enabledLifecycleConfig(t, &denialProvider{}))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = r.Stop(context.Background()) })

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "denial:history"}
	command, targets := denialSeedPopulation(t, r, scope, "history-target", 1, false)
	_, peers := denialSeedPopulation(t, r, scope, "history-peer", 1, false)
	startSQLComponents(t, r)
	waitSemanticState(t, r, scope, "hybrid-ready")
	r.service.semantic.startup.Wait()

	old := targets[0]
	history := old
	history.Revision = 2
	digest := sha256.Sum256([]byte("historical-replacement"))
	history.RevisionID = memory.StableID("revision", digest[:])
	history.Parents = []string{old.RevisionID}
	mixed := old
	digest = sha256.Sum256([]byte("mixed-evidence"))
	mixed.RevisionID = memory.StableID("revision", digest[:])
	mixed.ItemID = memory.StableID("item", digest[:])

	mixed.Evidence = append(append([]memory.EvidenceRef{}, old.Evidence...), peers[0].Evidence...)
	state, err := r.service.store.LoadScopeState(t.Context(), scope)
	requireNoError(t, err, "history state: %v")
	_, err = r.service.store.ApplyCanonicalMutation(t.Context(), memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope, ExpectedScopeVersion: state.Version,
		Operation: memory.OperationRecord{OperationID: "history-and-mixed",
			Fingerprint: "history-and-mixed", CommittedAt: time.Now().UTC()},
		Items: []memory.MemoryItem{{ItemID: mixed.ItemID, Scope: scope,
			Kind: memory.MemoryKindState, MemoryKey: "mixed"}},
		Revisions: []memory.MemoryRevision{history, mixed},
		Heads: []memory.ItemHead{{ItemID: old.ItemID, RevisionID: history.RevisionID},
			{ItemID: mixed.ItemID, RevisionID: mixed.RevisionID}},
	})
	requireNoError(t, err, "history mutation: %v")
	requireNoError(t, r.service.projection.Sync(t.Context(), scope), "history lexical: %v")
	requireNoError(t, r.service.semantic.manager.Repair(t.Context(), scope), "history semantics: %v")
	manifest, _, err := r.service.store.LoadActiveProjectionManifest(t.Context(), scope, "sqlite")
	requireNoError(t, err, "history source: %v")
	records, err := r.service.reader.LoadRecallRecords(t.Context(), scope, []string{history.RevisionID})
	requireNoError(t, err, "history record: %v")

	if len(records) != 1 {
		t.Fatal("missing history record")
	}

	g, coverage, chunks := lateDeniedPrivacyFixture(records[0], manifest)
	g.BuildID = "denial-zero-coverage"
	chunks[0].BuildID = g.BuildID
	_, err = r.service.index.Stage(t.Context(), g, coverage, chunks)
	requireNoError(t, err, "pending before zero-coverage persisted fixture: %v")

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()
	// Persisted zero coverage is a recovery contract, not a claim that portable
	// Stage admits a zero-count CoverageRecord. Remove all SQL source proof for
	// this owner so the real surviving structural Evidence is sufficient.
	_, err = db.ExecContext(t.Context(), `PRAGMA foreign_keys=ON;
DELETE FROM semantic_chunk WHERE build_id='denial-zero-coverage';
UPDATE semantic_coverage SET chunk_count=0 WHERE build_id='denial-zero-coverage'`)
	requireNoError(t, err, "persisted zero coverage: %v")

	for _, revision := range []string{history.RevisionID, mixed.RevisionID} {
		denialAssertCounts(t, db, scope, revision, true)
	}

	var oldCount, zeroCount int
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM lexical_record WHERE revision_id=?", old.RevisionID).Scan(&oldCount), "retained history: %v")
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM semantic_coverage WHERE build_id=?", g.BuildID).Scan(&zeroCount), "zero coverage: %v")

	if oldCount == 0 || zeroCount != 1 {
		t.Fatalf("history/zero controls=%d/%d", oldCount, zeroCount)
	}

	_, err = r.ForgetSource(t.Context(), command)
	requireNoError(t, err, "public historical source erase: %v")

	for _, revision := range []string{old.RevisionID, history.RevisionID, mixed.RevisionID} {
		denialAssertCounts(t, db, scope, revision, false)
	}

	var peerCount int
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM lexical_record WHERE revision_id=?", peers[0].RevisionID).Scan(&peerCount), "eligible peer: %v")

	if peerCount == 0 {
		t.Fatal("mixed Evidence enrolled independent peer")
	}
}

type denialPausedReader struct {
	memory.RecallCanonicalReader

	entered chan struct{}
	release chan struct{}
}

func (p *denialPausedReader) LoadRecallRecords(
	ctx context.Context, scope memory.Scope, ids []string,
) ([]memory.RecallRecord, error) {
	records, err := p.RecallCanonicalReader.LoadRecallRecords(ctx, scope, ids)
	if err != nil {
		return nil, err
	}

	close(p.entered)

	select {
	case <-p.release:
		return records, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// The producer already owns the actual Store scope lease during hydration.
// Public retirement cannot complete until that admission finishes; fresh
// canonical guards reject the cached identity before its SQL insertion.
//
//nolint:funlen // Explicit channels show the complete producer-to-public-retirement ownership sequence.
func TestStandaloneDenialLateProducerOrderings(t *testing.T) {
	r, _, _, turn := denialFixture(t)
	record := privacyRuntimeRecord(t, r, turn.Scope)
	reader := &denialPausedReader{RecallCanonicalReader: r.service.reader,
		entered: make(chan struct{}), release: make(chan struct{})}
	applier, err := sqlite.NewSQLiteCanonicalApplier(r.service.index, reader, r.service.store)
	requireNoError(t, err, "independent producer: %v")
	requireNoError(t, applier.BeginProjectionRebuild(t.Context(), turn.Scope, "denial-inflight"), "producer begin: %v")
	revisions, err := r.service.store.LoadCanonicalRevisions(t.Context(),
		memory.CanonicalRevisionReadRequest{Scope: turn.Scope, RevisionIDs: []string{record.RevisionID}})
	requireNoError(t, err, "producer structural identity: %v")

	produced := make(chan error, 1)

	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)

		applyErr := applier.Apply(t.Context(), turn.Scope, "denial-inflight", nil, revisions)

		abortErr := applier.AbortProjectionSync(t.Context(), "denial-inflight")
		produced <- errors.Join(applyErr, abortErr)
	}()

	<-reader.entered

	adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
	requireNoError(t, err, "adapter: %v")

	retiring := make(chan struct{})
	r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter, &forgetProjectionSyncer{
		retire: denialRetireFunc(func(ctx context.Context, scope memory.Scope) error {
			close(retiring)

			return r.service.index.ReconcileCanonicalDenials(ctx, scope)
		}),
		next: denialSyncFunc(func(ctx context.Context, scope memory.Scope) error {
			// The independent applier owns its terminal action. The fixture joins
			// that owner before required Sync may claim a replacement stage.
			select {
			case <-ownerDone:
				return r.service.projection.Sync(ctx, scope)
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	})
	requireNoError(t, err, "joined request: %v")

	forgotten := make(chan error, 1)

	go func() { _, forgetErr := r.ForgetScope(t.Context(), denialCommand(turn.Scope)); forgotten <- forgetErr }()

	<-retiring

	select {
	case err := <-forgotten:
		t.Fatalf("retirement crossed owned scope: %v", err)
	default:
	}

	close(reader.release)
	requireNoError(t, <-produced, "cached hydration guard: %v")
	requireNoError(t, <-forgotten, "joined cleanup: %v")

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	denialAssertCounts(t, db, turn.Scope, "", false)
	manifest, _, err := r.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "fresh source after erase: %v")
	// Refreshed hydrated sequence is not authority. This otherwise portable
	// candidate uses the CURRENT source and must fail solely on permanent denial.
	record.ScopeChangeSeq = manifest.Watermark
	g, coverage, chunks := lateDeniedPrivacyFixture(record, manifest)

	_, err = r.service.index.Stage(t.Context(), g, coverage, chunks)
	if !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("late prepared identity admitted: %v", err)
	}
}

//nolint:funlen // Service cancellation, joined producer termination and same-SQL recovery stay adjacent.
func TestStandaloneDenialShutdownDrains(t *testing.T) {
	r, cfg, _, turn := denialFixture(t)
	record := privacyRuntimeRecord(t, r, turn.Scope)
	reader := &denialPausedReader{RecallCanonicalReader: r.service.reader,
		entered: make(chan struct{}), release: make(chan struct{})}
	applier, err := sqlite.NewSQLiteCanonicalApplier(r.service.index, reader, r.service.store)
	requireNoError(t, err, "producer: %v")
	requireNoError(t, applier.BeginProjectionRebuild(t.Context(), turn.Scope, "denial-shutdown"), "begin: %v")
	revisions, err := r.service.store.LoadCanonicalRevisions(t.Context(),
		memory.CanonicalRevisionReadRequest{Scope: turn.Scope, RevisionIDs: []string{record.RevisionID}})
	requireNoError(t, err, "revision: %v")
	ctx, done, err := r.service.beginOperation(t.Context())
	requireNoError(t, err, "tracked producer admission: %v")

	produced := make(chan error, 1)

	go func() { defer done(); produced <- applier.Apply(ctx, turn.Scope, "denial-shutdown", nil, revisions) }()

	<-reader.entered

	adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
	requireNoError(t, err, "adapter: %v")

	retiring := make(chan struct{})
	r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter, &forgetProjectionSyncer{
		retire: denialRetireFunc(func(ctx context.Context, scope memory.Scope) error {
			close(retiring)

			return r.service.index.ReconcileCanonicalDenials(ctx, scope)
		}), next: r.service.projection})
	requireNoError(t, err, "service join: %v")

	forgotten := make(chan error, 1)

	go func() { _, forgetErr := r.ForgetScope(t.Context(), denialCommand(turn.Scope)); forgotten <- forgetErr }()

	<-retiring

	stopCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	requireNoError(t, r.Stop(stopCtx), "service cancellation and drain: %v")

	if err := <-produced; !errors.Is(err, context.Canceled) {
		t.Fatalf("producer cancellation=%v", err)
	}

	if err := <-forgotten; !errors.Is(err, context.Canceled) {
		t.Fatalf("joined request cancellation=%v", err)
	}

	cfg.Embeddings = EmbeddingsConfig{}
	reopened, err := New(cfg)
	requireNoError(t, err, "closed owners permit reopen: %v")
	t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
	startSQLComponents(t, reopened)

	db := openRuntimeSQL(t, reopened)
	defer func() { _ = db.Close() }()

	denialAssertCounts(t, db, turn.Scope, "", false)
}

//nolint:cyclop,funlen // Independent target, peer and empty owner assertions cover exact publication ownership.
func TestStandaloneDenialPendingPublicationAndPeers(t *testing.T) {
	r, _, _, first := denialFixture(t)
	r.service.semantic.startup.Wait()

	scope := first.Scope
	page, err := r.service.store.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope, Limit: 10})
	requireNoError(t, err, "pending identities: %v")

	if len(page) != 2 {
		t.Fatal("pending target and peer missing")
	}

	ids := []string{page[0].RevisionID, page[1].RevisionID}
	records, err := r.service.reader.LoadRecallRecords(t.Context(), scope, ids)
	requireNoError(t, err, "pending records: %v")
	manifest, _, err := r.service.store.LoadActiveProjectionManifest(t.Context(), scope, "sqlite")
	requireNoError(t, err, "pending source: %v")

	var target, peer memory.RecallRecord

	for _, record := range records {
		if record.SourceRefs[0].ExportID == first.ExportID {
			target = record
		} else {
			peer = record
		}
	}

	if target.RevisionID == "" || peer.RevisionID == "" {
		t.Fatal("pending source selection invalid")
	}

	g, coverage, chunks := lateDeniedPrivacyFixture(target, manifest)
	g.BuildID = "denial-target-pending"
	chunks[0].BuildID = g.BuildID
	_, err = r.service.index.Stage(t.Context(), g, coverage, chunks)
	requireNoError(t, err, "complete target Stage: %v")

	other, peerCoverage, peerChunks := lateDeniedPrivacyFixture(peer, manifest)
	other.BuildID = "denial-peer-pending"
	peerChunks[0].BuildID = other.BuildID
	_, err = r.service.index.Stage(t.Context(), other, peerCoverage, peerChunks)
	requireNoError(t, err, "complete peer Stage: %v")

	empty := other
	empty.BuildID = "denial-empty-pending"
	_, err = r.service.index.Stage(t.Context(), empty, nil, nil)
	requireNoError(t, err, "empty pending Stage: %v")
	active, _, err := r.service.index.ActiveState(t.Context(), scope)
	requireNoError(t, err, "prior active owner: %v")

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	var targetBefore, peerBefore int
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM semantic_chunk WHERE build_id=?", g.BuildID).Scan(&targetBefore), "pending target positive: %v")
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM semantic_chunk WHERE build_id=?", other.BuildID).Scan(&peerBefore), "pending peer positive: %v")

	if targetBefore != 1 || peerBefore != 1 {
		t.Fatal("pending positive controls absent")
	}

	_, err = r.ForgetSource(t.Context(), memory.ForgetSourceCommand{SchemaVersion: memory.DerivedSchemaVersionV1,
		Source: target.SourceRefs[0], ForgottenAt: time.Now().UTC()})
	requireNoError(t, err, "public erase with pending owners: %v")
	denialAssertCounts(t, db, scope, target.RevisionID, false)

	err = r.service.index.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID,
		ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: active.PublicationEpoch})
	if !errors.Is(err, semantic.ErrConflict) && !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("revoked candidate was not rejected as incomplete: %v", err)
	}

	for _, owner := range []string{other.BuildID, empty.BuildID} {
		var complete bool
		requireNoError(t, db.QueryRowContext(t.Context(),
			"SELECT complete FROM semantic_build WHERE build_id=?", owner).Scan(&complete), "preserved pending owner: %v")

		if !complete {
			t.Fatalf("unrelated owner %s revoked", owner)
		}
	}

	var peerAfter int
	requireNoError(t, db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM semantic_chunk WHERE build_id=?", other.BuildID).Scan(&peerAfter), "peer after cleanup: %v")

	if peerAfter != peerBefore {
		t.Fatal("cleanup released independent peer")
	}

	resumed, err := r.service.applier.ResumeProjection(t.Context(), scope, manifest.GenerationID)
	requireNoError(t, err, "old lexical source resume: %v")

	if resumed {
		t.Fatal("privacy-revoked lexical source resumed")
	}

	if err := r.service.applier.Activate(t.Context(), scope, manifest.GenerationID); err == nil {
		t.Fatal("late lexical activation certified erased source")
	}
}
