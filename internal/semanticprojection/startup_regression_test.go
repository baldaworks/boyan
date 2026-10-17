//nolint:funlen,lll,testpackage // Deterministic waiter gates retain the distinct TASK023 reviewer assertions.
package semanticprojection

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

func TestSQLiteStartupWaiterContextPrecedesCompletedResult(t *testing.T) {
	for _, repair := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(map[bool]string{false: "start", true: "repair"}[repair]+map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
				f := newSQLiteManagerFixture(t)
				seedStartupOrphan(t, f)

				entered, resume := make(chan struct{}), make(chan struct{})
				joined, resumeJoin := make(chan struct{}), make(chan struct{})

				unblock, unblockJoin := sync.OnceFunc(func() { close(resume) }), sync.OnceFunc(func() { close(resumeJoin) })
				defer unblock()
				defer unblockJoin()

				failure := memory.RetryableError(memory.CodeConflict, "private probe prose", errors.New("private cause"))
				f.manager.lifecycle = &startupLifecycle{CandidateLifecycle: f.store, repair: func(ctx context.Context) error {
					close(entered)
					<-resume

					if err := f.store.RepairSemanticOrphans(ctx); err != nil {
						return err
					}

					if fail {
						return failure
					}

					return nil
				}}
				f.manager.joinedInitialization = func() { close(joined); <-resumeJoin }

				started, waiting := make(chan error, 1), make(chan error, 1)
				go func() { started <- f.manager.Start(t.Context()) }()

				<-entered

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				go func() {
					if repair {
						waiting <- f.manager.Repair(ctx, f.scope)
					} else {
						waiting <- f.manager.Start(ctx)
					}
				}()

				<-joined
				cancel()
				unblock()

				if err := <-started; (err != nil) != fail {
					t.Fatalf("initializer err=%v fail=%v", err, fail)
				}

				unblockJoin()

				if err := <-waiting; !errors.Is(err, context.Canceled) {
					t.Fatalf("waiter result=%v", err)
				}
			})
		}
	}
}

func TestSQLiteStartupWaiterRetainsFailureAfterLaterSuccessfulAttempt(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "repair"}[repair], func(t *testing.T) {
			f := newSQLiteManagerFixture(t)
			seedStartupOrphan(t, f)

			entered, resume := make(chan struct{}), make(chan struct{})
			joined, resumeJoin := make(chan struct{}), make(chan struct{})

			unblock, unblockJoin := sync.OnceFunc(func() { close(resume) }), sync.OnceFunc(func() { close(resumeJoin) })
			defer unblock()
			defer unblockJoin()

			failure := errors.New("private probe cause")

			var attempts atomic.Int32

			f.manager.lifecycle = &startupLifecycle{CandidateLifecycle: f.store, repair: func(ctx context.Context) error {
				if attempts.Add(1) == 1 {
					close(entered)
					<-resume

					return failure
				}

				return f.store.RepairSemanticOrphans(ctx)
			}}
			f.manager.joinedInitialization = func() { close(joined); <-resumeJoin }

			started, waiting := make(chan error, 1), make(chan error, 1)
			go func() { started <- f.manager.Start(t.Context()) }()

			<-entered

			go func() {
				if repair {
					waiting <- f.manager.Repair(t.Context(), f.scope)
				} else {
					waiting <- f.manager.Start(t.Context())
				}
			}()

			<-joined
			unblock()

			first := <-started
			if !errors.Is(first, failure) {
				t.Fatalf("first=%v", first)
			}

			if err := f.manager.Start(t.Context()); err != nil {
				t.Fatalf("retry=%v", err)
			}

			unblockJoin()

			if err := <-waiting; err != first {
				t.Fatalf("old waiter observed later initialization: first=%v waiter=%v", first, err)
			}

			if attempts.Load() != 2 {
				t.Fatalf("attempts=%d", attempts.Load())
			}
		})
	}
}
