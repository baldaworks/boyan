//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sqlitedriver "modernc.org/sqlite"
)

// Observe the real driver boundary, keeping SQL execution and transactions intact.
type observedQuery struct {
	query string
	args  []any
}

type queryObserver struct {
	mu      sync.Mutex
	queries []observedQuery
}

func (o *queryObserver) record(query string, args []driver.NamedValue) {
	o.mu.Lock()
	defer o.mu.Unlock()

	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}

	o.queries = append(o.queries, observedQuery{query, values})
}

func (o *queryObserver) take() []observedQuery {
	o.mu.Lock()
	defer o.mu.Unlock()

	queries := o.queries
	o.queries = nil

	return queries
}

type observedConnector struct {
	dsn      string
	observer *queryObserver
}

func (c observedConnector) Driver() driver.Driver { return &sqlitedriver.Driver{} }

func (c observedConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}

	return observedConn{Conn: conn, observer: c.observer}, nil
}

type observedConn struct {
	driver.Conn

	observer *queryObserver
}

func (c observedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c observedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.observer.record(query, args)

	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func (c observedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func observeReader(t *testing.T, store *Store, dir string) *queryObserver {
	t.Helper()

	observer := &queryObserver{}
	original := store.reader
	pool := sql.OpenDB(observedConnector{poolURI(filepath.Join(dir, "search", "search.sqlite"), true, false), observer})
	pool.SetMaxOpenConns(readerConnections)
	store.reader = pool

	t.Cleanup(func() { _ = pool.Close(); store.reader = original })

	return observer
}

func observedQueryPlan(t *testing.T, store *Store, query observedQuery) string {
	t.Helper()

	rows, err := store.reader.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query.query, query.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var plan []string

	for rows.Next() {
		var (
			id, parent, unused int
			detail             string
		)
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}

		plan = append(plan, detail)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return strings.Join(plan, "\n")
}
