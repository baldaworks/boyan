package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestNativeReconciliationAdvancesLateStateAndEvent(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, _, turn, _, store := nativeFlow(t, factory)
		ingestKindFixture(t, store, turn)
		first, err := store.ScanActiveMemory(context.Background(), m.ActiveMemoryScanRequest{Scope: turn.Scope, Limit: 256})
		requireNoError(t, err, "page1: %v")

		if len(first) != 256 {
			t.Fatalf("first page size: %d", len(first))
		}

		late,
			err := store.ScanActiveMemory(context.Background(),
			m.ActiveMemoryScanRequest{Scope: turn.Scope,
				AfterItemID: first[255].Item.ItemID,
				Limit:       256})
		requireNoError(t, err, "page2: %v")

		if len(late) != 2 {
			t.Fatalf("late fixtures: %+v", late)
		}

		for _, active := range late {
			assertLateReconciliation(t, reader, store, turn, active)
		}
	})
}

func assertLateReconciliation(t *testing.T,
	reader coreReader,
	store coreOwner,
	turn m.Turn,
	active m.ActiveCanonicalMemory) {
	t.Helper()

	records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, []string{active.RevisionID})
	requireNoError(t, err, "record: %v")

	if len(records) != 1 {
		t.Fatal("late record missing")
	}

	original := records[0]
	predicate := original.SourceRefs[0].SourceTurnID
	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
		error) {
		candidates, extractErr := extractTestStateCandidate(ctx, request)
		if extractErr != nil {
			return nil, extractErr
		}

		candidates[0].Predicate = predicate
		candidates[0].Kind = active.Item.Kind
		candidates[0].Memory.Kind = active.Item.Kind
		candidates[0].Memory.Statement = "updated memory"

		return candidates, nil
	})
	processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")

	update, derivation := lateEditTurn(t, turn, predicate, active.Item.Kind)
	outcome, err := processor.ProcessTurn(context.Background(), update, derivation)
	requireNoError(t, err, "update late item: %v")
	revisions,
		err := store.LoadCanonicalRevisions(context.Background(),
		m.CanonicalRevisionReadRequest{Scope: turn.Scope,
			RevisionIDs: outcome.RevisionIDs})
	requireNoError(t, err, "revisions: %v")

	if len(revisions) != 1 ||
		revisions[0].ItemID != active.Item.ItemID ||
		revisions[0].Revision != 2 ||
		len(revisions[0].Parents) != 1 ||
		revisions[0].Parents[0] != active.RevisionID {
		t.Fatalf("late %s was not superseded: %+v",
			active.Item.Kind,
			revisions)
	}
}

type failingReconcileStore struct {
	m.CanonicalStore

	calls     int
	applied   int
	page      []m.ActiveCanonicalMemory
	duplicate m.ActiveCanonicalMemory
	mode      string
	cancel    context.CancelFunc
}

func (s *failingReconcileStore) ScanActiveMemory(ctx context.Context,
	request m.ActiveMemoryScanRequest) ([]m.ActiveCanonicalMemory,
	error) {
	s.calls++
	if s.mode == "nonadvancing" && s.calls == 2 {
		return s.page, nil
	}

	if s.mode == "ambiguous" && s.calls == 3 {
		return []m.ActiveCanonicalMemory{s.duplicate}, nil
	}

	page, err := s.CanonicalStore.ScanActiveMemory(ctx, request)
	if err != nil {
		return nil, err
	}

	s.transformPage(page)

	return page, nil
}

func (s *failingReconcileStore) ApplyCanonicalMutation(ctx context.Context,
	mutation m.CanonicalMutation) (m.CanonicalMutationOutcome,
	error) {
	s.applied++
	if s.mode == "cas" {
		store := s.CanonicalStore.(coreOwner)

		_,
			err := store.CommitCanonicalOperation(ctx,
			m.CanonicalOperationCommitRequest{Scope: mutation.Scope,
				ExpectedScopeVersion: mutation.ExpectedScopeVersion,
				OperationID:          "competing",
				Fingerprint:          "competing",
				CommittedAt:          time.Now().UTC()})
		if err != nil {
			return m.CanonicalMutationOutcome{}, err
		}
	}

	return s.CanonicalStore.ApplyCanonicalMutation(ctx, mutation)
}

func TestPagedReconciliationFailuresDoNotPartiallyWrite(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, _, turn, _, store := nativeFlow(t, factory)
		ingestKindFixture(t, store, turn)
		page, err := store.ScanActiveMemory(context.Background(), m.ActiveMemoryScanRequest{Scope: turn.Scope, Limit: 256})
		requireNoError(t, err, "page: %v")
		late,
			err := store.ScanActiveMemory(context.Background(),
			m.ActiveMemoryScanRequest{Scope: turn.Scope,
				AfterItemID: page[255].Item.ItemID,
				Limit:       256})
		requireNoError(t, err, "late: %v")
		records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, []string{late[0].RevisionID})
		requireNoError(t, err, "late record: %v")

		for _, mode := range []string{"ambiguous", "nonadvancing", "foreign", "cancel", "cas"} {
			t.Run(mode, func(t *testing.T) {
				assertPagedFailure(t, store, turn, late[0], records[0].SourceRefs[0].SourceTurnID, mode)
			})
		}
	})
}

func assertPagedFailure(t *testing.T,
	store coreOwner,
	turn m.Turn,
	target m.ActiveCanonicalMemory,
	predicate,
	mode string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	duplicate := target
	duplicate.Item.ItemID = target.Item.ItemID + "-duplicate-long"
	duplicate.RevisionID = "duplicate-revision"
	adapter := &failingReconcileStore{CanonicalStore: store, mode: mode, cancel: cancel, duplicate: duplicate}
	extractor := canonicalExtractorFunc(func(extractCtx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
		error) {
		candidates, extractErr := extractTestStateCandidate(extractCtx, request)
		if extractErr == nil {
			candidates[0].Predicate = predicate
		}

		return candidates, extractErr
	})
	processor, err := core.NewCanonicalTurnProcessor(adapter, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	before, err := store.LoadScopeState(context.Background(), turn.Scope)
	requireNoError(t, err, "before: %v")
	update,
		err := m.NewTurn(turn.Scope,
		turn.Conversation,
		"failure-"+mode,
		turn.CompletedAt.Add(time.Hour),
		"updated memory",
		"OK")
	requireNoError(t, err, "turn: %v")

	_, err = processor.ProcessTurn(ctx, update, testProcessorDerivation())
	if err == nil {
		t.Fatal("unsafe scan committed")
	}

	if mode != "cas" && adapter.applied != 0 {
		t.Fatalf("failed scan attempted mutation: %d", adapter.applied)
	}

	if mode == "cas" {
		code, _, ok := m.ClassifyError(err)
		if !ok || code != m.CodeConflict {
			t.Fatalf("CAS error: %v", err)
		}
	}

	assertNoPartialReconciliation(t, store, turn.Scope, update, before.Version, mode)
}

func (s *failingReconcileStore) transformPage(page []m.ActiveCanonicalMemory) {
	if s.calls == 1 {
		s.page = page
	}

	if s.calls == 2 && s.mode == "cancel" {
		s.cancel()
	}

	if s.calls == 2 && s.mode == "foreign" && len(page) > 0 {
		page[0].Item.Scope.Key = "test:foreign"
	}
}

func assertNoPartialReconciliation(t *testing.T,
	store coreOwner,
	scope m.Scope,
	update m.Turn,
	beforeVersion uint64,
	mode string) {
	t.Helper()

	after, err := store.LoadScopeState(context.Background(), scope)
	requireNoError(t, err, "after: %v")

	wantVersion := beforeVersion
	if mode == "cas" {
		wantVersion++
	}

	if after.Version != wantVersion {
		t.Fatalf("failed request changed scope: %d -> %d", beforeVersion, after.Version)
	}

	operationID, err := m.CanonicalSemanticOperationID(update.ExportID, testProcessorDerivation())
	requireNoError(t, err, "operation: %v")
	_, found, err := store.LoadCanonicalOperation(context.Background(), scope, operationID)
	requireNoError(t, err, "operation read: %v")

	if found {
		t.Fatal("failed request left replay/partial write")
	}
}

func lateEditTurn(t *testing.T, turn m.Turn, predicate string, kind m.MemoryKind) (m.Turn, m.DerivationRef) {
	t.Helper()

	id := "state-edit"
	completed := turn.CompletedAt.Add(time.Hour)
	text := "updated memory"
	derivation := testProcessorDerivation()

	if kind == m.MemoryKindEvent {
		id = predicate
		completed = turn.CompletedAt
		text = "needle memory"
		derivation.Prompt = "prompt-v2"
	}

	update, err := m.NewTurn(turn.Scope, turn.Conversation, id, completed, text, "OK")
	requireNoError(t, err, "turn: %v")

	return update, derivation
}
