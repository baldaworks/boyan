//go:build integration

package standaloneruntime //nolint:testpackage // Checks original outcomes through the actual application service.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryops"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,funlen,gocognit // Each injected boundary shares real canonical-completion and retry assertions.
func TestStandaloneDenialPublicErrors(t *testing.T) {
	for _, fault := range []string{
		"storage", "busy", "read-close", "canonical", "cancel-before", "cancel-retire", "cancel-sync", "sync",
	} {
		t.Run(fault, func(t *testing.T) {
			r, err := New(testStandaloneConfig(t))
			requireNoError(t, err, "new: %v")
			t.Cleanup(func() { _ = r.Stop(context.Background()) })
			turn := seedSemanticTurn(t, r, "public-error")
			startSQLComponents(t, r)

			db := openRuntimeSQL(t, r)
			defer func() { _ = db.Close() }()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			failure := memory.RetryableError(memory.CodeConflict, "required lexical sync failed", nil)
			if fault == "read-close" {
				failure = memory.RetryableError(memory.CodeConflict, "close failure", nil)
			}

			if fault == "canonical" {
				failure = memory.PermanentError(memory.CodeStoreFailure, "authority failure", nil)
			}

			cleanup := func() {}

			switch fault {
			case "storage":
				_, err = db.ExecContext(t.Context(), `CREATE TRIGGER denial_delete_failure BEFORE DELETE ON lexical_record
 BEGIN SELECT RAISE(ABORT,'private denial fault'); END`)
				requireNoError(t, err, "fault: %v")

				cleanup = func() {
					_, dropErr := db.ExecContext(t.Context(), "DROP TRIGGER denial_delete_failure")
					requireNoError(t, dropErr, "drop fault: %v")
				}
			case "busy":
				tx, txErr := db.BeginTx(t.Context(), nil)
				requireNoError(t, txErr, "busy writer: %v")
				_, txErr = tx.ExecContext(t.Context(), "UPDATE lexical_build SET state=state")
				requireNoError(t, txErr, "hold writer: %v")
				t.Cleanup(func() { _ = tx.Rollback() })

				cleanup = func() { requireNoError(t, tx.Rollback(), "release writer: %v") }
			case "cancel-before":
				cancel()
			default:
				adapter, adapterErr := forgetops.NewCanonicalForgetAdapter(r.service.store)
				requireNoError(t, adapterErr, "adapter: %v")

				joined := &forgetProjectionSyncer{retire: r.service.index, next: r.service.projection}
				if fault == "read-close" || fault == "canonical" {
					// TASK026 owns the actual reader/Close and canonical fault
					// mechanisms. Consume their classified result at the public
					// post-Complete boundary without replacing its state machine.
					joined.retire = denialRetireFunc(func(context.Context, memory.Scope) error { return failure })
				} else if fault == "cancel-retire" {
					joined.retire = denialRetireFunc(func(ctx context.Context, scope memory.Scope) error {
						cancel()

						return r.service.index.ReconcileCanonicalDenials(ctx, scope)
					})
				} else {
					joined.next = denialSyncFunc(func(ctx context.Context, scope memory.Scope) error {
						if fault == "cancel-sync" {
							cancel()

							return r.service.projection.Sync(ctx, scope)
						}

						return failure
					})
				}

				r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter, joined)
				requireNoError(t, err, "joined failure boundary: %v")

				cleanup = func() {
					r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter,
						&forgetProjectionSyncer{retire: r.service.index, next: r.service.projection})
					requireNoError(t, err, "restore normal join: %v")
				}
			}

			command := denialCommand(turn.Scope)

			outcome, err := r.ForgetScope(ctx, command)
			denialAssertPublicFailure(t, fault, err, failure)

			if fault == "cancel-before" {
				if outcome.OperationID != "" {
					t.Fatal("pre-canonical cancellation supplied outcome")
				}

				return
			}

			if outcome.OperationID == "" || outcome.RevisionCount != 1 {
				t.Fatalf("canonical outcome lost after completion: %+v", outcome)
			}

			journal, found, journalErr := r.service.store.LoadScopeErase(t.Context(), turn.Scope, outcome.OperationID)
			requireNoError(t, journalErr, "canonical completion: %v")

			if !found || journal.Stage != memory.ScopeEraseComplete || !reflect.DeepEqual(*journal.Outcome, outcome) {
				t.Fatal("projection error rolled back or changed canonical completion")
			}

			cleanup()

			replay, err := r.ForgetScope(t.Context(), command)
			requireNoError(t, err, "failure retry joins original outcome: %v")

			if !reflect.DeepEqual(outcome, replay) {
				t.Fatal("retry changed outcome")
			}

			var count int
			requireNoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&count),
				"retry retirement: %v")

			if count != 0 {
				t.Fatal("retry returned before retirement")
			}
		})
	}
}

//nolint:cyclop // Real SQL, propagated classified failures and caller cancellation retain distinct contracts.
func denialAssertPublicFailure(t *testing.T, fault string, err, failure error) {
	t.Helper()

	if err == nil || strings.Contains(err.Error(), "private denial fault") {
		t.Fatalf("partial success/unsafe failure: %v", err)
	}

	if strings.HasPrefix(fault, "cancel") && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation changed: %v", err)
	}

	if fault == "read-close" || fault == "canonical" || fault == "sync" {
		if !errors.Is(err, failure) {
			t.Fatalf("classified required error identity lost: %v", err)
		}

		code, class, _ := memory.ClassifyError(err)

		wantCode, wantClass, _ := memory.ClassifyError(failure)
		if code != wantCode || class != wantClass {
			t.Fatalf("classified required error changed: %s/%s", code, class)
		}
	}

	if fault == "storage" || fault == "busy" {
		code, _, _ := memory.ClassifyError(err)

		want := memory.CodeStoreFailure
		if fault == "busy" {
			want = memory.CodeConflict
		}

		if code != want {
			t.Fatalf("required failure code=%s want=%s error=%v", code, want, err)
		}
	}
}
