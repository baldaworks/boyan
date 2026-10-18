package canonicalsql

import (
	"context"
	"errors"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Catches blocked admissions ignoring cancellation, recursive connection use,
// and a rejected callback running after its caller has stopped waiting.
func TestOwnerAdmissionCancellationAndRecursion(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		entered, release := make(chan struct{}), make(chan struct{})

		done := make(chan error, 1)
		go func() {
			done <- owner.View(t.Context(), func(ctx context.Context, _ *Tx) error {
				close(entered)

				code, _, _ := memory.ClassifyError(owner.RestorePreflight(ctx))
				if code != memory.CodeInvalidDerived {
					t.Error("recursive admission did not fail closed")
				}

				<-release

				return nil
			})
		}()

		<-entered

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()

		err := owner.Update(ctx, func(context.Context, *Tx) error {
			t.Error("canceled waiter entered transaction")

			return nil
		})

		close(release)

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("admission lost cancellation: %v", err)
		}

		if err := <-done; err != nil {
			t.Fatal(err)
		}

		if err := owner.RestorePreflight(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

// Close must cancel admitted work, wait for rollback, reject all waiting work,
// and keep ownership until that rollback has completed.
func TestOwnerCloseDrainsRollbackBeforeUnlock(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		key := Key{Family: Payload, Parts: [][]byte{[]byte("aborted")}}
		entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})

		result, closed := make(chan error, 1), make(chan error, 1)
		go func() {
			result <- owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				if err := tx.Put(ctx, key, envelope(t, Payload, "uncommitted")); err != nil {
					return err
				}

				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release

				return nil // Even an ignored cancellation must prevent COMMIT.
			})
		}()

		<-entered

		go func() { closed <- owner.Close() }()

		<-canceled

		assertCode(t, owner.RestorePreflight(t.Context()), memory.CodeUnavailable)

		// Inspect lock lifetime without admitting another transaction on the owner.
		assertOwnerStillLocked(t, owner)

		select {
		case err := <-closed:
			t.Fatalf("close returned before drain: %v", err)
		default:
		}

		close(release)

		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("active update was not canceled: %v", err)
		}

		if err := <-closed; err != nil {
			t.Fatal(err)
		}

		reopened := reopen()
		if err := reopened.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			_, err := tx.Get(ctx, key)
			assertCode(t, err, memory.CodeNotFound)

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		assertClock(t, reopened.connection, 0, 0)
	})
}

// Two actual owner admissions cannot race the expected-version CAS; rollback
// is complete before the next request reads the resulting durable state.
func TestOwnerSerializesScopeCAS(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		start := make(chan struct{})
		results := make(chan error, 2)

		for range 2 {
			go func() {
				<-start

				results <- owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					return tx.CompareAndSwapScope(ctx, nil, memory.ScopeState{
						SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: testScope(), Version: 1, ChangeSeq: 1,
					})
				})
			}()
		}

		close(start)

		first, second := <-results, <-results
		if first != nil {
			first, second = second, first
		}

		if first != nil {
			t.Fatalf("no admitted winner: %v", first)
		}

		assertCode(t, second, memory.CodeConflict)

		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}

		reopened := reopen()
		assertClock(t, reopened.connection, 1, 1)
	})
}
