//go:build integration

package storefixture_test

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type activeReaderStore interface {
	ScanActiveHeads(ctx context.Context, request memory.ActiveHeadScanRequest) ([]memory.ItemHead, error)
	ScanActiveMemory(ctx context.Context, request memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error)
	SourceRevisionBatch(ctx context.Context, scope memory.Scope, source, after string, limit uint32) ([]string,
		string, error)
	memory.CanonicalScopeStatePager
}

type sqlReader interface {
	memory.RecallCanonicalReader
	SearchDerived(ctx context.Context, request memory.DerivedSearchRequest) (memory.DerivedSearchResponse, error)
}

func publicReader(t *testing.T, store storefixture.Store) sqlReader {
	t.Helper()

	var reader any

	switch store := store.(type) {
	case *sqlite.Store:
		reader = sqlite.NewCanonicalReader(store)
	case *postgres.Store:
		reader = postgres.NewCanonicalReader(store)
	default:
		t.Fatal("unknown public owner")
	}

	result, ok := reader.(sqlReader)
	if !ok {
		t.Fatal("public reader lacks bounded recall/derived contract")
	}

	return result
}

func TestPublicReaderActiveRecallDerived(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		active, ok := f.Store.(activeReaderStore)
		if !ok {
			t.Fatal("public owner lacks active and source-local reader contract")
		}

		assertPublicReaderHeads(t, active, mutation.Scope)
		assertPublicReaderRecall(t, publicReader(t, f.Store), mutation.Scope, mutation.Sources[0].SourceID)
		assertPublicReaderEnumeration(t, active, mutation)
	})
}

func assertPublicReaderHeads(t *testing.T, active activeReaderStore, scope memory.Scope) {
	t.Helper()

	heads, err := active.ScanActiveHeads(t.Context(), memory.ActiveHeadScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(heads) != 1 || heads[0].RevisionID != "revision" {
		t.Fatalf("heads=%+v err=%v", heads, err)
	}

	records, err := active.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(records) != 1 || len(records[0].Evidence) != 1 {
		t.Fatalf("active=%+v err=%v", records, err)
	}
}

func assertPublicReaderRecall(t *testing.T, reader sqlReader, scope memory.Scope, sourceID string) {
	t.Helper()

	recalled, err := reader.LoadRecallRecords(t.Context(), scope, []string{"missing", "revision"})
	if err != nil || len(recalled) != 1 || recalled[0].Text != "protected memory" || recalled[0].ScopeChangeSeq != 1 {
		t.Fatalf("recall=%+v err=%v", recalled, err)
	}

	if len(recalled[0].SourceIDs) != 1 || recalled[0].SourceIDs[0] != sourceID {
		t.Fatalf("recall source IDs=%v want=%q", recalled[0].SourceIDs, sourceID)
	}

	derived, err := reader.SearchDerived(t.Context(), memory.DerivedSearchRequest{Scope: scope,
		Query: "protected", Limit: 10})
	if err != nil || len(derived.Results) != 1 || derived.Results[0].RevisionID != "revision" {
		t.Fatalf("derived=%+v err=%v", derived, err)
	}
}

func assertPublicReaderEnumeration(t *testing.T, active activeReaderStore, mutation memory.CanonicalMutation) {
	t.Helper()

	ids, next, err := active.SourceRevisionBatch(t.Context(), mutation.Scope, mutation.Sources[0].SourceID, "", 1)
	if err != nil || len(ids) != 1 || ids[0] != "revision" || next != "revision" {
		t.Fatalf("source batch=%v next=%q err=%v", ids, next, err)
	}

	states, nextScope, err := active.ListCanonicalScopeStates(t.Context(), nil, 1)
	if err != nil || len(states) != 1 || states[0].Scope != mutation.Scope || len(nextScope) != 0 {
		t.Fatalf("scopes=%+v next=%x err=%v", states, nextScope, err)
	}
}
