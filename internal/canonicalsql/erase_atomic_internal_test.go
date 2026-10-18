package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"testing"
	"time"
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
func eraseStampSnapshot(t *testing.T, o *Owner) []Stamp {
	t.Helper()

	var all []Stamp

	err := o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		// Compare all structural stamps without repeatedly hydrating every live
		// envelope. Page effects are separately checked through the public APIs.
		rows, err := tx.q.query(ctx,
			"SELECT family,binary_key,last_seq,deleted FROM "+o.connection.table("row_stamp")+" ORDER BY family,binary_key")
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				stamp            Stamp
				sequence, family []byte
			)
			if err := rows.Scan(&family, &stamp.Cursor.BinaryKey, &sequence, &stamp.Deleted); err != nil {
				return err
			}

			stamp.Cursor.Family = Family(family)

			stamp.Cursor.Sequence, err = DecodeUint64(sequence)
			if err != nil {
				return err
			}

			all = append(all, stamp)
		}

		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	return all
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
		before := eraseStampSnapshot(t, o)
		release := failEraseJournalWrite(t, o)
		j, err := o.AdvanceScopeErase(t.Context(), scope, "erase")

		release()

		if err == nil || j.Stage != "" || !reflect.DeepEqual(before, eraseStampSnapshot(t, o)) {
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

func TestEraseLoadingFenceSurvivesReopen(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		scope := memory.Scope{Key: "erase:loading", Kind: memory.ScopeKindPersonal}
		r := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		_, err := o.BeginScopeErase(t.Context(), r)
		requireEraseTestNoError(t, err)
		rawExec(t, o.connection,
			"INSERT INTO "+o.connection.table("restore_control")+"(singleton,control) VALUES(1,$1)", []byte("loading"))
		requireEraseTestNoError(t, o.Close())
		o = reopen()
		_, err = o.BeginScopeErase(t.Context(), r)
		assertCode(t, err, memory.CodeUnavailable)
		_, err = o.AdvanceScopeErase(t.Context(), scope, "erase")
		assertCode(t, err, memory.CodeUnavailable)
		err = o.CompleteScopeErase(t.Context(), scope, "erase")
		assertCode(t, err, memory.CodeUnavailable)
		err = o.PrepareSourcePrivacy(t.Context())
		assertCode(t, err, memory.CodeUnavailable)
		_, _, err = o.ListPendingScopeErases(t.Context(), nil, 1)
		assertCode(t, err, memory.CodeUnavailable)

		var encoded []byte

		err = scanOne(t.Context(), o.connection.q, "SELECT envelope FROM "+o.connection.table("erase_journal"), nil, &encoded)
		requireEraseTestNoError(t, err)

		var j memory.ScopeEraseJournal
		requireEraseTestNoError(t, Decode(EraseJournal, encoded, &j))

		if j.Stage != memory.ScopeErasePrepared {
			t.Fatalf("loading advanced journal %+v", j)
		}
	})
}

func assertErasePageRollback(t *testing.T, o *Owner, journal memory.ScopeEraseJournal, refs []memory.PayloadRef) {
	t.Helper()
	before := eraseStampSnapshot(t, o)
	clock := eraseTestClock(t, o)
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

	if !reflect.DeepEqual(before, eraseStampSnapshot(t, o)) || !reflect.DeepEqual(clock, eraseTestClock(t, o)) {
		t.Fatal("page failure changed stamps/clock")
	}

	if !reflect.DeepEqual(payloads, captureErasePayloads(t, o, refs)) {
		t.Fatal("page failure changed payloads")
	}
}
func eraseTestClock(t *testing.T, o *Owner) []byte {
	t.Helper()

	var clock []byte

	err := scanOne(t.Context(), o.connection.q, "SELECT mutation_clock FROM "+o.connection.table("store_meta"),
		nil, &clock)
	requireEraseTestNoError(t, err)

	return clock
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

	var erased, deleted int

	for _, stamp := range eraseStampSnapshot(t, o) {
		if stamp.Cursor.Family == ErasedBlob && !stamp.Deleted {
			erased++
		}

		if stamp.Cursor.Family == Payload && stamp.Deleted {
			deleted++
		}
	}

	if erased != len(refs) || deleted != len(refs) {
		t.Fatalf("erased/deletion stamps=%d/%d want=%d", erased, deleted, len(refs))
	}
}
