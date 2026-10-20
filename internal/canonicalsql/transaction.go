package canonicalsql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const rollbackTimeout = 5 * time.Second

// Connection borrows one dedicated native connection. Its owner must serialize
// calls, fence ownership, and close it; this adapter never reconnects.
type Connection struct {
	q  query
	pg *pgx.Conn
}

// NewSQLite adapts an already configured, dedicated SQLite connection.
func NewSQLite(conn *sql.Conn) *Connection { return &Connection{q: sqliteQuery{conn}} }

// NewPostgres adapts a pinned direct PostgreSQL connection.
func NewPostgres(conn *pgx.Conn) *Connection { return &Connection{q: pgQuery{conn}, pg: conn} }

// Tx is a bounded query handle valid only during its transaction callback.
// Every write goes through Put/Delete/CAS.
type Tx struct {
	connection *Connection
	q          query
	writable   bool
	active     bool
	failed     error
	readMemo   map[string]readMemoEntry
	readBytes  int
}

// Update commits a callback atomically. Any failed primitive
// aborts the transaction even if the callback accidentally discards its error.
func (c *Connection) Update(ctx context.Context, fn func(*Tx) error) error {
	return c.run(ctx, true, fn)
}

// View runs a consistent snapshot; write primitives fail closed.
func (c *Connection) View(ctx context.Context, fn func(*Tx) error) error {
	return c.run(ctx, false, fn)
}

func (c *Connection) table(name string) string {
	if c.pg != nil {
		return "boyan_canonical." + name
	}

	return name
}

func (c *Connection) run(ctx context.Context, write bool, fn func(*Tx) error) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	q, commit, rollback, err := c.begin(ctx, write)
	if err != nil {
		return databaseError(ctx, "begin", err)
	}

	tx := &Tx{connection: c, q: q, writable: write, active: true}
	defer func() {
		tx.active = false

		cleanup, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()

		if err := rollback(cleanup); err != nil {
			result = errors.Join(result, databaseError(cleanup, "rollback", err))
		}
	}()

	callbackErr := fn(tx)

	if err := ctx.Err(); err != nil {
		return err
	}

	if callbackErr != nil {
		return callbackErr
	}

	if tx.failed != nil {
		return tx.failed
	}

	if err := commit(ctx); err != nil {
		return databaseError(ctx, "commit", err)
	}

	return nil
}

type transactionEnd func(context.Context) error

func (c *Connection) begin(ctx context.Context, write bool) (query, transactionEnd, transactionEnd, error) {
	if c.pg != nil {
		options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead}
		if !write {
			options.AccessMode = pgx.ReadOnly
		}

		native, err := c.pg.BeginTx(ctx, options)
		if err != nil {
			return nil, nil, nil, err
		}

		rollback := func(ctx context.Context) error {
			err := native.Rollback(ctx)
			if errors.Is(err, pgx.ErrTxClosed) {
				return nil
			}

			return err
		}

		return pgQuery{native}, native.Commit, rollback, nil
	}

	statement := "BEGIN"
	if write {
		statement = "BEGIN IMMEDIATE"
	}

	if _, err := c.q.exec(ctx, statement); err != nil {
		return nil, nil, nil, err
	}

	committed := false
	commit := func(ctx context.Context) error {
		_, err := c.q.exec(ctx, "COMMIT")
		committed = err == nil

		return err
	}
	rollback := func(ctx context.Context) error {
		if committed {
			return nil
		}

		_, err := c.q.exec(ctx, "ROLLBACK")

		return err
	}

	return c.q, commit, rollback, nil
}

func (tx *Tx) fail(err error) error {
	if err != nil && tx.failed == nil {
		tx.failed = err
	}

	return err
}

func (tx *Tx) check(ctx context.Context, write bool) error {
	if err := ctx.Err(); err != nil {
		return tx.fail(err)
	}

	if !tx.active {
		return tx.fail(invalid("canonical SQL transaction is closed"))
	}

	if tx.failed != nil {
		return tx.failed
	}

	if write && !tx.writable {
		return tx.fail(invalid("canonical SQL transaction is read only"))
	}

	return nil
}
