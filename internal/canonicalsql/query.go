package canonicalsql

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type query interface {
	exec(ctx context.Context, statement string, args ...any) (int64, error)
	query(ctx context.Context, statement string, args ...any) (rows, error)
}

type sqliteQuery struct{ conn *sql.Conn }

func (q sqliteQuery) exec(ctx context.Context, sql string, args ...any) (int64, error) {
	result, err := q.conn.ExecContext(ctx, sql, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
func (q sqliteQuery) query(ctx context.Context, sql string, args ...any) (rows, error) {
	return q.conn.QueryContext(ctx, sql, args...)
}

type postgresQuery interface {
	Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error)
}
type pgQuery struct{ conn postgresQuery }

func (q pgQuery) exec(ctx context.Context, sql string, args ...any) (int64, error) {
	result, err := q.conn.Exec(ctx, sql, args...)

	return result.RowsAffected(), err
}
func (q pgQuery) query(ctx context.Context, sql string, args ...any) (rows, error) {
	result, err := q.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	return pgRows{result}, nil
}

type pgRows struct{ pgx.Rows }

func (r pgRows) Close() error {
	r.Rows.Close()

	return r.Rows.Err()
}

func scanOne(ctx context.Context, q query, statement string, args []any, dest ...any) error {
	r, err := q.query(ctx, statement, args...)
	if err != nil {
		return err
	}
	defer r.Close()

	if !r.Next() {
		if err := r.Err(); err != nil {
			return err
		}

		return sql.ErrNoRows
	}

	if err := r.Scan(dest...); err != nil {
		return err
	}

	if err := r.Err(); err != nil {
		return err
	}

	return r.Close()
}
