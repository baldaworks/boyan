package canonicalsql

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type scopeClockSnapshot struct {
	clock  []byte
	stamps []Stamp
}

func readScopeClockSnapshot(t *testing.T, owner *Owner) scopeClockSnapshot {
	t.Helper()

	var result scopeClockSnapshot

	requireProofSuccess(t, owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		if err := scanOne(ctx, tx.q, "SELECT mutation_clock FROM "+tx.connection.table("store_meta"),
			nil, &result.clock); err != nil {
			return err
		}

		var err error

		result.stamps, err = tx.ScanStamps(ctx, nil, memory.MaxCanonicalReadRecords)

		return err
	}))

	return result
}

func TestFutureScopeStampPoisonsTransaction(t *testing.T) {
	for _, delta := range []uint64{1, 100} {
		for _, certificate := range []bool{false, true} {
			t.Run(fmt.Sprintf("plus-%d/certificate-%t", delta, certificate), func(t *testing.T) {
				forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
					assertFutureScopeStampPoison(t, owner, delta, certificate)
				})
			})
		}
	}
}

func assertFutureScopeStampPoison(t *testing.T, owner *Owner, delta uint64, certificate bool) {
	t.Helper()
	requireProofSuccess(t, owner.PrepareSourcePrivacy(t.Context()))
	h := newPrivacyHistory(t)
	_, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h))
	requireProofSuccess(t, err)
	initial := readScopeClockSnapshot(t, owner)
	clock, err := DecodeUint64(initial.clock)
	requireProofSuccess(t, err)
	_, err = owner.connection.q.exec(t.Context(), "UPDATE "+owner.connection.table("row_stamp")+
		" SET last_seq=$1 WHERE family=$2", Uint64(clock+delta), []byte(ScopeState))
	requireProofSuccess(t, err)
	before := readScopeClockSnapshot(t, owner)
	err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if certificate {
			if err := persistTurnCertificate(ctx, tx, proofTurnMutation(t, h)); err != nil {
				return err
			}

			if len(tx.sequence) == 0 {
				t.Error("certificate did not allocate lazy sequence")
			}
		}

		key, _ := proofScopeKey(h.scope)
		_, readErr := loadProofScopeState(ctx, tx, h.scope, key)

		code, _, _ := memory.ClassifyError(readErr)
		if code != memory.CodeStoreFailure {
			t.Errorf("future scope read accepted: %v", readErr)
		}

		return nil // A caller swallowing the error cannot commit the certificate/clock.
	})
	assertCode(t, err, memory.CodeStoreFailure)

	if !reflect.DeepEqual(before, readScopeClockSnapshot(t, owner)) {
		t.Fatal("poisoned transaction changed persisted state")
	}
}

func TestScopeStampWrittenInSameTransactionIsValid(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := testScope()
		key, _ := proofScopeKey(scope)
		state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope,
			Version: 1 << 63, ChangeSeq: 1 << 63}

		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if err := putPrivacyRecord(ctx, tx, key, ScopeState, state); err != nil {
				return err
			}

			if _, err := loadProofScopeState(ctx, tx, scope, key); err != nil {
				return err
			}

			prior := state.Version
			state.Version++

			state.ChangeSeq++
			if err := tx.CompareAndSwapScope(ctx, &prior, state); err != nil {
				return err
			}

			if _, err := loadProofScopeState(ctx, tx, scope, key); err != nil {
				return err
			}

			if err := tx.Delete(ctx, key); err != nil {
				return err
			}

			_, err := loadProofScopeState(ctx, tx, scope, key)

			return err
		}))
		assertClock(t, owner.connection, 1, 1)
	})
}
