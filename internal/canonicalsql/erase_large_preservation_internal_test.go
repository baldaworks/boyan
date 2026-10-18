package canonicalsql

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeEraseBoundsSummaryAcross1025HistoricalSourcesAndRevisions(t *testing.T) {
	forEachOwner(t, func(t *testing.T, store *Owner, _ func() *Owner) {
		var err error

		scope := memory.Scope{Key: "telegram:large", Kind: memory.ScopeKindPersonal}
		refs := seedEraseHistory(t, store, scope, 1025)
		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}

		journal, err := store.RunScopeErase(context.Background(), request)
		requireEraseTestNoError(t, err)

		if journal.SourceCount != 1025 || journal.RevisionCount != 1025 || !journal.ReferencesTruncated ||
			len(journal.Sources)+len(journal.Revisions) != 0 {
			t.Fatalf("journal=%+v", journal)
		}

		if journal.Outcome.SourceCount != 1025 || journal.Outcome.RevisionCount != 1025 ||
			!journal.Outcome.ReferencesTruncated {
			t.Fatalf("outcome=%+v", journal.Outcome)
		}

		assertLargeEraseRebuildMarker(t, store, scope)

		request.OperationID = "erase-again"

		request.ExpectedScopeVersion = journal.Outcome.ScopeVersion
		if _, err := store.RunScopeErase(context.Background(), request); err != nil {
			t.Fatalf("new request after truncated result: %v", err)
		}

		requireErasePayloadsAbsent(t, store, refs)

		if _, _, err := store.LoadCanonicalForgetOutcome(
			context.Background(), scope, "erase", memory.ForgetKindScope,
		); err != nil {
			t.Fatal(err)
		}
	})
}

func assertLargeEraseRebuildMarker(t *testing.T, store *Owner, scope memory.Scope) {
	t.Helper()

	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 1)
	requireEraseTestNoError(t, err)

	if len(changes) != 1 || !changes[0].RequiresRebuild || len(changes[0].RevisionIDs) != 0 {
		t.Fatalf("large erase missing bounded rebuild marker: %+v", changes)
	}
}
