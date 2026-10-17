package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

func (store *Store) acquire(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("sqlite search context is nil")
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if store.closing {
		return nil, nil, ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	store.work.Add(1)

	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(store.lifetime, cancel)

	var once sync.Once

	return operation, func() { once.Do(func() { stop(); cancel(); store.work.Done() }) }, nil
}

func (store *Store) write(ctx context.Context, apply func(context.Context, *sql.Tx) error) (result error) {
	operation, done, err := store.acquire(ctx)
	if err != nil {
		return err
	}
	defer done()
	// Drivers may report interruption or ErrTxDone after cancellation. Preserve
	// the operation's identity only on failure, before done cancels its lease.
	defer func() {
		if result != nil && operation.Err() != nil {
			result = operation.Err()
		}
	}()

	conn, err := store.writer.Conn(operation)
	if err != nil {
		return err
	}
	defer func() {
		// A second Rollback can return ErrTxDone while database/sql's
		// cancellation goroutine is still inside the driver's rollback. Closing
		// our owned connection joins that work before the operation lease ends.
		closeErr := conn.Close()
		if result == nil && !errors.Is(closeErr, sql.ErrConnDone) {
			result = closeErr
		}
	}()

	tx, err := conn.BeginTx(operation, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	if err := apply(operation, tx); err != nil {
		return err
	}

	return tx.Commit()
}

type readSnapshot struct {
	tx   *sql.Tx
	conn *sql.Conn
	ctx  context.Context
	once sync.Once
	done func()
	err  error
}

func (store *Store) read(ctx context.Context) (*readSnapshot, error) {
	operation, done, err := store.acquire(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := store.reader.Conn(operation)
	if err != nil {
		if operation.Err() != nil {
			err = operation.Err()
		}

		done()

		return nil, err
	}

	tx, err := conn.BeginTx(operation, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		_ = conn.Close()

		if operation.Err() != nil {
			err = operation.Err()
		}

		done()

		return nil, err
	}

	snapshot := &readSnapshot{tx: tx, conn: conn, ctx: operation, done: done}
	// The driver rolls back canceled transactions. Close also releases our
	// lifecycle lease, including when a canceled caller forgot to close a view.
	context.AfterFunc(operation, func() { _ = snapshot.Close() })

	return snapshot, nil
}

func (snapshot *readSnapshot) Close() error {
	snapshot.once.Do(func() {
		snapshot.err = snapshot.tx.Rollback()
		if errors.Is(snapshot.err, sql.ErrTxDone) {
			snapshot.err = nil
		}

		// ErrTxDone can precede the driver's actual rollback completion. The
		// owned connection waits for that rollback before releasing this lease.
		if snapshot.conn != nil {
			err := snapshot.conn.Close()
			if !errors.Is(err, sql.ErrConnDone) {
				snapshot.err = errors.Join(snapshot.err, err)
			}
		}

		snapshot.done()
	})

	return snapshot.err
}

// CloseContext rejects new operations and initiates cancellation once. A caller
// deadline bounds its wait; storage remains owned until outstanding work drains.
func (store *Store) CloseContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite search close context is nil")
	}

	store.closeOnce.Do(func() {
		store.mu.Lock()
		store.closing = true
		store.cancel()

		store.mu.Unlock()
		go func() {
			store.work.Wait()
			store.closeErr = errors.Join(store.reader.Close(), store.writer.Close(), store.root.Close())
			close(store.closed)
		}()
	})

	select {
	case <-store.closed:
		return store.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (store *Store) Close() error { return store.CloseContext(context.Background()) }
