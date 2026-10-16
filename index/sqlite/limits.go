package sqlite

import (
	"context"
	"database/sql"

	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// These are observed engine limits, never a new lexical admission policy.
// Open eagerly inspects every physical pool connection without changing it.
type sqlConnectionLimits struct{ length, variables int }

func inspectConnectionLimits(ctx context.Context, pool *sql.DB, count int) ([]sqlConnectionLimits, error) {
	conns := make([]*sql.Conn, 0, count)
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	limits := make([]sqlConnectionLimits, 0, count)
	for range count {
		conn, err := pool.Conn(ctx)
		if err != nil {
			return nil, err
		}

		conns = append(conns, conn)

		length, err := driver.Limit(conn, sqlite3.SQLITE_LIMIT_LENGTH, -1)
		if err != nil {
			return nil, err
		}

		variables, err := driver.Limit(conn, sqlite3.SQLITE_LIMIT_VARIABLE_NUMBER, -1)
		if err != nil {
			return nil, err
		}

		limits = append(limits, sqlConnectionLimits{length: length, variables: variables})
	}

	return limits, nil
}
