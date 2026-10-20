//go:build integration

package canonicalsql

import (
	"context"
	"reflect"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func failEraseJournalWrite(t *testing.T, o *Owner) func() {
	t.Helper()

	c := o.connection
	if c.pg == nil {
		rawExec(t, c, "CREATE TRIGGER erase_test_fail BEFORE UPDATE ON erase_journal "+
			"BEGIN SELECT RAISE(ABORT,'erase injected'); END")

		return func() { rawExec(t, c, "DROP TRIGGER erase_test_fail") }
	}

	rawExec(t, c, "CREATE FUNCTION boyan_canonical.erase_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ "+
		"BEGIN RAISE EXCEPTION 'erase injected'; END $$")
	rawExec(t, c, "CREATE TRIGGER erase_test_fail BEFORE UPDATE ON boyan_canonical.erase_journal "+
		"FOR EACH ROW EXECUTE FUNCTION boyan_canonical.erase_test_fail()")

	return func() {
		rawExec(t, c, "DROP TRIGGER erase_test_fail ON boyan_canonical.erase_journal")
		rawExec(t, c, "DROP FUNCTION boyan_canonical.erase_test_fail()")
	}
}

// A failure after page effects but before the journal write must roll back
// payload deletes, tombstones, denials, locks, state, stamps and progress together.
func TestEraseEveryPageAtomicAndReopen(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		scope := memory.Scope{Key: "erase:atomic", Kind: memory.ScopeKindPersonal}
		refs := seedEraseHistory(t, o, scope, 130)
		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		journal, err := o.BeginScopeErase(t.Context(), request)
		requireEraseTestNoError(t, err)

		stages := map[memory.ScopeEraseStage]bool{}
		pages := 0

		for journal.Stage != memory.ScopeEraseComplete {
			stages[journal.Stage] = true
			assertErasePageRollback(t, o, journal, refs)
			requireEraseTestNoError(t, o.Close())
			o = reopen()
			journal, err = o.AdvanceScopeErase(t.Context(), scope, "erase")
			requireEraseTestNoError(t, err)

			pages++
			if pages > 100 {
				t.Fatal("erase failed to terminate")
			}
		}

		if pages < 20 || len(stages) < 15 {
			t.Fatalf("page/phase coverage %d/%d", pages, len(stages))
		}

		assertEraseFinalPayloadStamps(t, o, scope, refs)
	})
}

func TestEraseRejectedPageFailureRetainsOwnedLocks(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		scope := memory.Scope{Key: "erase:reject", Kind: memory.ScopeKindPersonal}
		seedSharedEraseSource(t, o, scope)
		r := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		_, err := o.BeginScopeErase(t.Context(), r)
		requireEraseTestNoError(t, err)
		advanceEraseTestToStage(t, o, scope, "erase", memory.ScopeEraseRejectLocks)
		before := eraseDomainSnapshot(t, o)
		release := failEraseJournalWrite(t, o)
		j, err := o.AdvanceScopeErase(t.Context(), scope, "erase")

		release()

		if err == nil || j.Stage != "" || !reflect.DeepEqual(before, eraseDomainSnapshot(t, o)) {
			t.Fatalf("failed rejection=%+v %v", j, err)
		}

		requireEraseTestNoError(t, o.Close())
		o = reopen()

		j, err = o.RunScopeErase(t.Context(), r)
		if err == nil || j.Stage != memory.ScopeEraseRejected {
			t.Fatalf("rejection retry=%+v %v", j, err)
		}

		pending, _, err := o.ListPendingScopeErases(t.Context(), nil, 1)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending=%+v %v", pending, err)
		}
	})
}

func assertErasePageRollback(t *testing.T, o *Owner, journal memory.ScopeEraseJournal, refs []memory.PayloadRef) {
	t.Helper()
	before := eraseDomainSnapshot(t, o)

	payloads := captureErasePayloads(t, o, refs)
	release := failEraseJournalWrite(t, o)
	failed, err := o.AdvanceScopeErase(t.Context(), journal.Scope, journal.OperationID)

	release()

	if err == nil || failed.Stage != "" {
		t.Fatalf("failed page exposed provisional state %+v %v", failed, err)
	}

	after, found, err := o.LoadScopeErase(t.Context(), journal.Scope, journal.OperationID)
	if err != nil || !found || !reflect.DeepEqual(after, journal) {
		t.Fatalf("page progress changed: %+v %v", after, err)
	}

	if !reflect.DeepEqual(before, eraseDomainSnapshot(t, o)) {
		t.Fatal("page failure changed stamps/clock")
	}

	if !reflect.DeepEqual(payloads, captureErasePayloads(t, o, refs)) {
		t.Fatal("page failure changed payloads")
	}
}
func captureErasePayloads(t *testing.T, o *Owner, refs []memory.PayloadRef) [][]byte {
	t.Helper()

	var result [][]byte

	for _, ref := range append(append([]memory.PayloadRef(nil), refs[:3]...), refs[len(refs)-3:]...) {
		data, err := o.LoadPayload(t.Context(), ref)
		if err != nil && !privacyMissing(err) {
			t.Fatal(err)
		}

		result = append(result, data)
	}

	return result
}

func assertEraseFinalPayloadStamps(t *testing.T, o *Owner, scope memory.Scope, refs []memory.PayloadRef) {
	t.Helper()
	requireErasePayloadsAbsent(t, o, refs)

	state, err := o.LoadScopeState(t.Context(), scope)
	requireEraseTestNoError(t, err)

	if state.Version != 1 || state.ChangeSeq != 1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}

	if err := o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		for _, ref := range refs {
			if _, err := tx.Get(ctx, Key{Family: ErasedBlob, Parts: [][]byte{[]byte(ref.ID)}}); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func eraseDomainSnapshot(t *testing.T, o *Owner) []Row {
	t.Helper()

	var all []Row

	err := o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		for _, family := range durableFamilies {
			var after *Key

			for {
				page, err := tx.Scan(ctx, Range{Family: family, After: after, Limit: memory.MaxCanonicalReadRecords})
				if err != nil {
					return err
				}

				if len(page) == 0 {
					break
				}

				all = append(all, page...)
				key := page[len(page)-1].Key
				after = &key
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return all
}
