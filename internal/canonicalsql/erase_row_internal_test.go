//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func canonicalEraseRows(t *testing.T, owner *Owner) []Row {
	t.Helper()

	var result []Row

	for _, row := range eraseDomainSnapshot(t, owner) {
		if row.Key.Family == Source || row.Key.Family == Message || row.Key.Family == Revision {
			result = append(result, row)
		}
	}

	return result
}

func TestEraseCanonicalVisitorsPreserveRows(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := memory.Scope{Key: "erase:rows", Kind: memory.ScopeKindPersonal}
		refs := seedEraseHistory(t, owner, scope, 2)
		before := canonicalEraseRows(t, owner)
		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		journal, err := owner.BeginScopeErase(t.Context(), request)
		requireEraseTestNoError(t, err)

		for !eraseTerminal(journal.Stage) {
			journal, err = owner.AdvanceScopeErase(t.Context(), scope, request.OperationID)
			requireEraseTestNoError(t, err)

			if !reflect.DeepEqual(before, canonicalEraseRows(t, owner)) {
				t.Fatalf("canonical rows changed at stage=%s", journal.Stage)
			}
		}

		if journal.SourceCount != 2 || journal.RevisionCount != 2 {
			t.Fatalf("missing deny effects: %+v", journal)
		}

		requireErasePayloadsAbsent(t, owner, refs)
	})
}

func TestErasePageCancellationBetweenVisitorsRollsBack(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := memory.Scope{Key: "erase:cancel-page", Kind: memory.ScopeKindPersonal}
		seedEraseHistory(t, owner, scope, 2)
		before := eraseDomainSnapshot(t, owner)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		visits := 0

		err := owner.Update(t.Context(), func(_ context.Context, tx *Tx) error {
			_, _, err := eraseWalkPage(ctx, tx, Range{Family: Source, Scope: &scope, Limit: 128}, nil, func(row Row) error {
				visits++
				_, err := eraseLockRecord(ctx, tx, &memory.ScopeEraseJournal{Scope: scope, OperationID: "cancel"}, row, Source)

				if visits == 1 {
					cancel()
				}

				return err
			})

			return err
		})
		if !errors.Is(err, context.Canceled) || visits != 2 {
			t.Fatalf("visits=%d error=%v", visits, err)
		}

		if !reflect.DeepEqual(before, eraseDomainSnapshot(t, owner)) {
			t.Fatal("canceled page committed effects")
		}
	})
}

func TestEraseRowRetainsTransactionAndDomainChecks(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := memory.Scope{Key: "erase:row-checks", Kind: memory.ScopeKindPersonal}
		seedEraseHistory(t, owner, scope, 1)

		for _, family := range []Family{Source, Message, Revision} {
			t.Run(string(family), func(t *testing.T) {
				err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					page, err := tx.Scan(ctx, Range{Family: family, Scope: &scope, Limit: 1})
					if err != nil {
						return err
					}

					row := page[0]
					assertEraseRowTransactionChecks(t, ctx, tx, row, family)
					assertEraseRowDomainChecks(t, ctx, tx, row, family)

					return nil
				})
				requireEraseTestNoError(t, err)
			})
		}
	})
}

func assertEraseRowTransactionChecks(t *testing.T, ctx context.Context, tx *Tx, row Row, family Family) {
	t.Helper()

	for _, mode := range []string{"closed", "poisoned", "readonly"} {
		copyTx := *tx

		switch mode {
		case "closed":
			copyTx.active = false
		case "poisoned":
			copyTx.failed = invalid("injected primitive failure")
		case "readonly":
			copyTx.writable = false
		}

		_, err := erasePageRecord(ctx, &copyTx, row, family)
		assertCode(t, err, memory.CodeInvalidDerived)

		if mode == "poisoned" && err != copyTx.failed {
			t.Fatal("poison error replaced")
		}
	}
}

func assertEraseRowDomainChecks(t *testing.T, ctx context.Context, tx *Tx, row Row, family Family) {
	t.Helper()

	for _, mode := range []string{"family", "identity", "scope"} {
		bad := row

		switch mode {
		case "family":
			bad.Key.Family = Payload
		case "identity":
			bad.Key.Parts = [][]byte{[]byte("wrong-id")}
		case "scope":
			bad.Key.Scope = memory.Scope{}
		}

		_, err := erasePageRecord(ctx, tx, bad, family)
		assertCode(t, err, memory.CodeStoreFailure)
	}

	var invalidDomain any

	switch family {
	case Source:
		invalidDomain = memory.CanonicalSourceRecord{SourceID: string(row.Key.Parts[0]), Scope: row.Key.Scope}
	case Message:
		invalidDomain = memory.MessageRecord{MessageID: string(row.Key.Parts[0])}
	case Revision:
		invalidDomain = memory.MemoryRevision{RevisionID: string(row.Key.Parts[0])}
	}

	encoded, err := Encode(family, invalidDomain)
	requireEraseTestNoError(t, err)

	row.Envelope = encoded
	_, err = erasePageRecord(ctx, tx, row, family)
	assertCode(t, err, memory.CodeStoreFailure)
}
