//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestReadSnapshotPinsStateAcrossWriterAndRollback(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	execTest(t, store.writer, `INSERT INTO lexical_build
	 (physical_build_id,scope_kind,scope_key,generation_id,watermark) VALUES('b','user','u','g',?)`, encodeUint64(1))

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	assertRecordCount(t, snapshot.tx, 0)

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE lexical_build SET record_count=2")

		return err
	}); err != nil {
		t.Fatal(err)
	}

	assertRecordCount(t, snapshot.tx, 0)
	assertRecordCount(t, store.reader, 2)

	sentinel := errors.New("abort")

	err = store.write(t.Context(), func(_ context.Context, tx *sql.Tx) error {
		execTest(t, tx, "UPDATE lexical_build SET record_count=3")

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error: %v", err)
	}

	assertRecordCount(t, store.reader, 2)
}

func assertRecordCount(t *testing.T, query interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, want int) {
	t.Helper()

	var got int
	if err := query.QueryRowContext(t.Context(), "SELECT record_count FROM lexical_build").Scan(&got); err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("record_count=%d want=%d", got, want)
	}
}

func TestCloseDeadlineRetainsPoolsUntilWriterReturns(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	started, release, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		result <- store.write(t.Context(), func(context.Context, *sql.Tx) error {
			close(started)
			<-release

			return nil
		})
	}()

	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	if err := store.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close deadline=%v", err)
	}

	select {
	case <-store.closed:
		t.Fatal("storage closed under writer")
	default:
	}

	close(release)

	if err := <-result; err == nil {
		t.Fatal("shutdown writer committed")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

//nolint:cyclop // Exercise cancellation at both SQL pool wait and active scan boundaries.
func TestQueuedWriterAndRunningScanCancel(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	conn, err := store.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err = store.write(ctx, func(context.Context, *sql.Tx) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer cancel=%v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel = context.WithCancel(t.Context())

	snapshot, err := store.read(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := snapshot.tx.QueryContext(ctx, `WITH RECURSIVE n(x) AS
	 (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT x FROM n`)
	if err != nil {
		t.Fatal(err)
	}

	if !rows.Next() {
		t.Fatal("scan did not start")
	}

	cancel()

	for rows.Next() {
	}

	if !errors.Is(rows.Err(), context.Canceled) {
		t.Fatalf("scan cancel=%v", rows.Err())
	}

	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLifetimeCancelsRunningSnapshotScan(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, `WITH RECURSIVE n(x) AS
	 (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT x FROM n`)
	if err != nil {
		t.Fatal(err)
	}

	if !rows.Next() {
		t.Fatal("scan did not start")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	for rows.Next() {
	}

	if !errors.Is(rows.Err(), context.Canceled) {
		t.Fatalf("lifetime scan=%v", rows.Err())
	}

	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}
