package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	sqlitedriver "modernc.org/sqlite"
)

var semanticCancelSequence atomic.Uint64

type semanticCoverageCancelContext struct {
	context.Context

	cancel context.CancelFunc
}

// Err injects cancellation at the pure ordering boundary after SQLite rows
// were read, distinguishing canceled validation from malformed coverage.
func (ctx semanticCoverageCancelContext) Err() error {
	var pcs [32]uintptr

	n := runtime.Callers(1, pcs[:])

	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, "semantic.OrderCoverage") {
			ctx.cancel()

			break
		}

		if !more {
			break
		}
	}

	return ctx.Context.Err()
}

func TestSemanticCoverageOrderingPreservesCancellationIdentity(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	tx, err := s.reader.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback() }()

	base, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, _, err = readSemanticCoverage(semanticCoverageCancelContext{Context: base, cancel: cancel}, tx, g.BuildID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("coverage cancellation=%v", err)
	}
}

func TestSQLiteWritePreservesCancellationAndGenuineErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		failure  error
		canceled bool
	}{
		{"transaction already canceled", sql.ErrTxDone, true},
		{"driver interrupted", errors.New("driver interrupted"), true},
		{"genuine storage error", errors.New("genuine storage failure"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			err := s.write(ctx, func(_ context.Context, _ *sql.Tx) error {
				if test.canceled {
					cancel()
				}

				return test.failure
			})

			want := test.failure
			if test.canceled {
				want = context.Canceled
			}

			if !errors.Is(err, want) {
				t.Fatalf("write returned %v want %v", err, want)
			}
		})
	}

	s, _ := openTestStore(t, true)
	if err := s.write(
		t.Context(),
		func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "SELECT 1")

			return err
		},
	); err != nil {
		t.Fatalf("successful write became cancellation: %v", err)
	}
}

//nolint:cyclop,funlen // Verify real in-flight rollback, ownership release and incomplete reopen together.
func TestSemanticCancellationInSecondSQLBatchRollsBackAndReopensIncomplete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	name := fmt.Sprintf("boyan_test_semantic_cancel_%d", semanticCancelSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(
		name,
		0,
		func(_ *sqlitedriver.FunctionContext, _ []driver.Value) (driver.Value, error) {
			cancel()

			return int64(0), nil
		},
	); err != nil {
		t.Fatal(err)
	}

	s, dir := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	r[0].ChunkCount = 130

	chunks := make([]semantic.ProjectionChunk, 130)
	for index := range chunks {
		chunks[index] = c[0]
		chunks[index].Ordinal = uint32(index)
	}
	// Cancellation happens inside a real SQLite statement, after the first batch
	// committed and after the second batch already inserted an earlier chunk.
	execTest(t, s.writer, fmt.Sprintf(`CREATE TRIGGER cancel_semantic_batch AFTER INSERT ON semantic_chunk
  WHEN new.ordinal=129 BEGIN SELECT %s(); END`, name))

	if _, err := s.Stage(ctx, g, r, chunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancel: %v", err)
	}

	var n, complete int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk").
		Scan(&n); err != nil ||
		n != 128 {
		t.Fatalf("current batch rollback: rows=%d err=%v", n, err)
	}

	if err := s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build").
		Scan(&complete); err != nil ||
		complete != 0 {
		t.Fatalf("partial complete=%d err=%v", complete, err)
	}

	if _, live := s.semanticStaging.Load(g.BuildID); live {
		t.Fatal("canceled stage retained live owner")
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()

	if _, found, err := reopened.ActiveState(t.Context(), g.Scope); err != nil || found {
		t.Fatalf("incomplete active=%v %v", found, err)
	}

	if err = reopened.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk").
		Scan(&n); err != nil ||
		n != 128 {
		t.Fatalf("pending preserved: %d %v", n, err)
	}
}
