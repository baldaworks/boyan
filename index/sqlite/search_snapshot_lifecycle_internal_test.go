package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sqlitedriver "modernc.org/sqlite"
)

// gateRollbackDriver delegates to the pinned real SQLite driver. Only rollback
// completion is gated, reproducing a transaction already marked done while its
// connection is still owned by a background rollback.
type gateRollbackDriver struct {
	started     chan struct{}
	release     chan struct{}
	begin       func(context.Context) error
	rollbackErr error
}

func (gate *gateRollbackDriver) Open(name string) (driver.Conn, error) {
	conn, err := (&sqlitedriver.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}

	return &gateRollbackConn{Conn: conn, gate: gate}, nil
}

type gateRollbackConn struct {
	driver.Conn

	gate *gateRollbackDriver
}

func (conn *gateRollbackConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if conn.gate.begin != nil {
		if err := conn.gate.begin(ctx); err != nil {
			return nil, err
		}
	}

	tx, err := conn.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &gateRollbackTx{Tx: tx, gate: conn.gate}, nil
}

func (conn *gateRollbackConn) ResetSession(ctx context.Context) error {
	return conn.Conn.(driver.SessionResetter).ResetSession(ctx)
}

func (conn *gateRollbackConn) IsValid() bool { return conn.Conn.(driver.Validator).IsValid() }

type gateRollbackTx struct {
	driver.Tx

	gate *gateRollbackDriver
}

func (tx *gateRollbackTx) Rollback() error {
	close(tx.gate.started)
	<-tx.gate.release

	return errors.Join(tx.Tx.Rollback(), tx.gate.rollbackErr)
}

func openGatedReadStore(t *testing.T, gate *gateRollbackDriver) *Store {
	t.Helper()
	s, dir := openTestStore(t, false)
	name := fmt.Sprintf("gated_search_rollback_%d", semanticCancelSequence.Add(1))
	sql.Register(name, gate)

	reader, err := sql.Open(name, poolURI(filepath.Join(dir, "search", "search.sqlite"), true, false))
	if err != nil {
		t.Fatal(err)
	}

	reader.SetMaxOpenConns(readerConnections)
	reader.SetMaxIdleConns(readerConnections)

	if err = s.reader.Close(); err != nil {
		t.Fatal(err)
	}

	s.reader = reader

	return s
}

// Close must retain its work lease and connection until the real rollback
// finishes, even if database/sql already returns ErrTxDone to a second caller.
func TestSearchSnapshotCloseWaitsForAlreadyStartedRollback(t *testing.T) {
	gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{})}
	s := openGatedReadStore(t, gate)

	var releaseOnce sync.Once

	release := func() { releaseOnce.Do(func() { close(gate.release) }) }
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	snapshot, err := s.read(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rollback := make(chan error, 1)
	go func() { rollback <- snapshot.tx.Rollback() }()

	<-gate.started // The transaction's done CAS has won, but the actual driver rollback is blocked.
	cancel()

	closed := make(chan error, 1)
	go func() { closed <- snapshot.Close() }()

	deadline, done := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer done()

	if err = s.CloseContext(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("storage closed before driver rollback completed: %v", err)
	}

	select {
	case err := <-closed:
		t.Fatalf("snapshot Close returned before driver rollback completed: %v", err)
	default:
	}

	release()

	if err = <-rollback; err != nil {
		t.Fatal(err)
	}

	if err = <-closed; err != nil {
		t.Fatal(err)
	}

	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	if got := s.reader.Stats().InUse; got != 0 {
		t.Fatalf("leaked read connections=%d", got)
	}
}

func TestSearchSnapshotBeginFailureReleasesConnectionAndIdentity(t *testing.T) {
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
			s := openGatedReadStore(t, gate)
			_, err := s.read(ctx)

			want := sentinel
			if canceled {
				want = context.Canceled
			}

			if !errors.Is(err, want) {
				t.Fatalf("begin error=%v want=%v", err, want)
			}

			if got := s.reader.Stats().InUse; got != 0 {
				t.Fatalf("begin failure leaked connection=%d", got)
			}

			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSearchSnapshotClosePreservesDriverRollbackFailure(t *testing.T) {
	sentinel := errors.New("controlled driver rollback failure")
	gate := &gateRollbackDriver{started: make(chan struct{}), release: make(chan struct{}), rollbackErr: sentinel}
	close(gate.release)
	s := openGatedReadStore(t, gate)

	snapshot, err := s.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err = snapshot.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("rollback identity=%v", err)
	}

	if got := s.reader.Stats().InUse; got != 0 {
		t.Fatalf("rollback failure leaked connection=%d", got)
	}
}
