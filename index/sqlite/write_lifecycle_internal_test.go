//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/internal/semanticprojection"
	"github.com/baldaworks/boyan/pkg/memory"
)

func openGatedWriteStore(t *testing.T, gate *gateRollbackDriver) (*Store, string) {
	t.Helper()

	store, dir := openTestStore(t, false)
	name := fmt.Sprintf("gated_write_rollback_%d", semanticCancelSequence.Add(1))
	sql.Register(name, gate)

	writer, err := sql.Open(name, poolURI(filepath.Join(dir, "search", "search.sqlite"), false, false))
	if err != nil {
		t.Fatal(err)
	}

	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	if err := store.writer.Close(); err != nil {
		t.Fatal(err)
	}

	store.writer = writer

	return store, dir
}

// Losing the Tx done CAS to cancellation must not release the operation lease
// while the actual driver Rollback still owns the SQL writer connection.
func TestWriteLifecycleWaitsForAlreadyStartedRollback(t *testing.T) {
	gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{})}
	store, dir := openGatedWriteStore(t, gate)

	release := sync.OnceFunc(func() { close(gate.release) })
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	returned := make(chan error, 1)
	go func() {
		returned <- store.write(ctx, func(operation context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(operation, "CREATE TABLE pending_write_rollback(value INTEGER)"); err != nil {
				return err
			}

			cancel()
			<-gate.started

			return operation.Err()
		})
	}()

	<-gate.started

	deadline, cancelClose := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancelClose()

	if err := store.CloseContext(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Store closed before real writer rollback completed: %v (writer InUse=%d)", err, store.writer.Stats().InUse)
	}

	select {
	case err := <-returned:
		t.Fatalf("write returned before real writer rollback completed: %v", err)
	default:
	}

	release()

	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("write cancellation identity=%v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if got := store.writer.Stats().InUse; got != 0 {
		t.Fatalf("writer connection leaked=%d", got)
	}

	assertPendingWriteRolledBack(t, dir)
}

// Start/Stop must consume actual SQL quiescence, including background rollback,
// before the caller is allowed to close storage or retry semantic initialization.
//
//nolint:funlen // Actual SQL rollback and both lifecycle drains form one boundary.
func TestWriteLifecycleManagerStartupWaitsForRollback(t *testing.T) {
	gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{})}
	store, dir := openGatedWriteStore(t, gate)

	release := sync.OnceFunc(func() { close(gate.release) })
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	lifecycle := &writeStartupLifecycle{Store: store, cancel: cancel, rollbackStarted: gate.started}
	// Startup performs no canonical/provider work. Empty embedded ports fail if
	// this metadata-only maintenance unexpectedly starts a producer.
	manager, err := semanticprojection.New(semanticprojection.Dependencies{
		Canonical: struct{ memory.CanonicalStore }{}, Records: struct{ memory.RecallCanonicalReader }{},
		Manifests: struct {
			memory.RecallProjectionCheckpointReader
		}{}, Physical: store,
		Provider: struct{ embedding.Provider }{}, Store: store, Lifecycle: lifecycle, LexicalProjectionID: "sqlite",
		ModelSpace: semanticcore.ModelSpace{Model: "model", Revision: "revision", Dimensions: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	returned := make(chan error, 1)
	go func() { returned <- manager.Start(ctx) }()

	<-gate.started

	deadline, cancelClose := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancelClose()

	if err := manager.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Manager drained before real writer rollback completed: %v", err)
	}

	select {
	case err := <-returned:
		t.Fatalf("initializer returned before real writer rollback completed: %v", err)
	default:
	}

	release()

	if err := <-returned; !errors.Is(err, semanticprojection.ErrClosed) {
		t.Fatalf("stopped initializer identity=%v", err)
	}

	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if got := store.writer.Stats().InUse; got != 0 {
		t.Fatalf("writer connection leaked=%d", got)
	}

	assertPendingWriteRolledBack(t, dir)
}

func TestWriteLifecycleBeginFailureReleasesConnectionAndErrorIdentity(t *testing.T) {
	sentinel := errors.New("controlled driver begin failure")

	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{})}
			gate.begin = func(context.Context) error {
				if canceled {
					cancel()
				}

				return sentinel
			}
			store, _ := openGatedWriteStore(t, gate)
			called := false
			err := store.write(ctx, func(context.Context, *sql.Tx) error {
				called = true

				return nil
			})

			want := sentinel
			if canceled {
				want = context.Canceled
			}

			if !errors.Is(err, want) || called {
				t.Fatalf("begin failure=%v called=%v", err, called)
			}

			if got := store.writer.Stats().InUse; got != 0 {
				t.Fatalf("begin failure leaked connection=%d", got)
			}

			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWriteLifecycleRollbackFailurePreservesPrimaryError(t *testing.T) {
	primary := errors.New("controlled apply failure")
	rollback := errors.New("controlled rollback failure")
	gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{}), rollbackErr: rollback}
	close(gate.release)
	store, dir := openGatedWriteStore(t, gate)

	err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE pending_write_rollback(value INTEGER)"); err != nil {
			return err
		}

		return primary
	})
	if !errors.Is(err, primary) {
		t.Fatalf("primary error lost=%v", err)
	}

	if got := store.writer.Stats().InUse; got != 0 {
		t.Fatalf("rollback failure leaked connection=%d", got)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	assertPendingWriteRolledBack(t, dir)
}

type writeStartupLifecycle struct {
	*Store

	cancel          context.CancelFunc
	rollbackStarted <-chan struct{}
}

func (l *writeStartupLifecycle) RepairSemanticOrphans(ctx context.Context) error {
	return l.Store.write(ctx, func(operation context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(operation, "CREATE TABLE pending_write_rollback(value INTEGER)"); err != nil {
			return err
		}

		l.cancel()
		<-l.rollbackStarted

		return operation.Err()
	})
}

func assertPendingWriteRolledBack(t *testing.T, dir string) {
	t.Helper()

	db, err := sql.Open("sqlite", poolURI(filepath.Join(dir, "search", "search.sqlite"), true, true))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = db.Close() }()

	var count int

	const query = "SELECT count(*) FROM sqlite_schema WHERE name='pending_write_rollback'"

	if err := db.QueryRowContext(t.Context(), query).Scan(&count); err != nil || count != 0 {
		t.Fatalf("aborted SQL survived rollback=%d err=%v", count, err)
	}
}
