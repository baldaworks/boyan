//go:build integration

package forgetops_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type sourceJournalOwner interface {
	sqlForgetStore
	CommitCanonicalForget(ctx context.Context, request m.CanonicalForgetCommitRequest) (m.ForgetOutcome, error)
	DenySource(ctx context.Context, scope m.Scope, id string, at time.Time) error
	Close() error
}

type sourceJournalStore struct {
	sourceJournalOwner

	database    *canonicalsqltest.Database
	resolutions int
	erases      int
}

func (s *sourceJournalStore) CanonicalSourceID(ctx context.Context, scope m.Scope, ref m.SourceRef) (string, error) {
	s.resolutions++

	return s.sourceJournalOwner.CanonicalSourceID(ctx, scope, ref)
}

func (s *sourceJournalStore) RunScopeErase(ctx context.Context,
	request m.ScopeEraseRequest) (m.ScopeEraseJournal, error) {
	s.erases++

	return s.sourceJournalOwner.RunScopeErase(ctx, request)
}

func runSourceJournalAuthorities(t *testing.T, run func(*testing.T, canonicalsqltest.Backend)) {
	t.Helper()

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) { run(t, backend) })
	}
}

func openSourceJournalStore(t *testing.T, backend canonicalsqltest.Backend) (
	*sourceJournalStore, func() (*sourceJournalStore, error),
) {
	t.Helper()

	fixture := storefixture.New(t, backend)
	open := func() (*sourceJournalStore, error) {
		owner, err := fixture.Open(t.Context())
		if err != nil {
			return nil, err
		}

		return &sourceJournalStore{sourceJournalOwner: owner.(sourceJournalOwner), database: fixture.Database}, nil
	}
	store := &sourceJournalStore{sourceJournalOwner: fixture.Store.(sourceJournalOwner), database: fixture.Database}
	sourceJournalNoError(t, store.PrepareSourcePrivacy(t.Context()))

	return store, open
}

func sourceJournalRawDB(t *testing.T, database *canonicalsqltest.Database) (*sql.DB, string) {
	t.Helper()

	conn, err := sql.Open("sqlite", database.DSN)
	sourceJournalNoError(t, err)
	t.Cleanup(func() { sourceJournalNoError(t, conn.Close()) })

	return conn, ""
}

// Snapshot every user-table column, not just envelopes, so rejected operations
// cannot silently alter counters, indexes, journals or schema history.
func sourceJournalSQLBytes(t *testing.T, database *canonicalsqltest.Database) map[string]string {
	t.Helper()

	if database.Postgres != nil {
		return sourceJournalPostgresBytes(t, database)
	}

	conn, prefix := sourceJournalRawDB(t, database)
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
	rows, err := conn.QueryContext(t.Context(), query)
	sourceJournalNoError(t, err)

	var tables []string

	for rows.Next() {
		var name string
		sourceJournalNoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}

	sourceJournalNoError(t, rows.Err())
	sourceJournalNoError(t, rows.Close())

	result := make(map[string]string)
	for _, table := range tables {
		result[table] = sourceJournalTableBytes(t, conn, prefix, table)
	}

	return result
}

func sourceJournalPostgresBytes(t *testing.T, database *canonicalsqltest.Database) map[string]string {
	t.Helper()
	rows, err := database.Postgres.Query(t.Context(), `SELECT table_name FROM information_schema.tables
 WHERE table_schema='boyan_canonical' AND table_type='BASE TABLE'`)
	sourceJournalNoError(t, err)

	var tables []string

	for rows.Next() {
		var name string
		sourceJournalNoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}

	sourceJournalNoError(t, rows.Err())
	rows.Close()

	result := make(map[string]string)

	for _, table := range tables {
		var records string

		query := `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,'[]')
 FROM boyan_canonical."` + table + `" t`
		sourceJournalNoError(t, database.Postgres.QueryRow(t.Context(), query).Scan(&records))
		result[table] = records
	}

	return result
}

func sourceJournalTableBytes(t *testing.T, conn *sql.DB, prefix, table string) string {
	t.Helper()
	//nolint:unqueryvet // Rejection snapshots must include every persisted column, including future additions.
	rows, err := conn.QueryContext(t.Context(), "SELECT * FROM "+prefix+`"`+table+`"`)
	sourceJournalNoError(t, err)

	defer rows.Close()

	columns, err := rows.Columns()
	sourceJournalNoError(t, err)

	var records []string

	for rows.Next() {
		values := make([]any, len(columns))

		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}

		sourceJournalNoError(t, rows.Scan(pointers...))

		encoded, err := json.Marshal(values)
		sourceJournalNoError(t, err)

		records = append(records, string(encoded))
	}

	sourceJournalNoError(t, rows.Err())
	sort.Strings(records)
	encoded, err := json.Marshal(records)
	sourceJournalNoError(t, err)

	return string(encoded)
}

func sourceJournalSQLRewrite(t *testing.T, database *canonicalsqltest.Database,
	kind string, change func([]byte) []byte) {
	t.Helper()

	table := map[string]string{"erase-journal": "erase_journal", "forget-outcome": "forget_outcome"}[kind]
	if table == "" {
		t.Fatalf("unexpected source journal fault target %q", kind)
	}

	var encoded []byte
	if database.Postgres != nil {
		sourceJournalNoError(t,
			database.Postgres.QueryRow(t.Context(), "SELECT envelope FROM boyan_canonical."+table).Scan(&encoded))
	} else {
		conn, _ := sourceJournalRawDB(t, database)
		sourceJournalNoError(t, conn.QueryRowContext(t.Context(), "SELECT envelope FROM "+table).Scan(&encoded))
	}

	envelope, err := m.UnmarshalRecordEnvelope(encoded)
	sourceJournalNoError(t, err)
	encoded, err = m.MarshalRecordEnvelope(kind, change(envelope.Payload))
	sourceJournalNoError(t, err)

	var count int64

	if database.Postgres != nil {
		result, err := database.Postgres.Exec(t.Context(), "UPDATE boyan_canonical."+table+" SET envelope=$1", encoded)
		sourceJournalNoError(t, err)

		count = result.RowsAffected()
	} else {
		conn, _ := sourceJournalRawDB(t, database)
		result, err := conn.ExecContext(t.Context(), "UPDATE "+table+" SET envelope=$1", encoded)
		sourceJournalNoError(t, err)
		count, err = result.RowsAffected()
		sourceJournalNoError(t, err)
	}

	if count != 1 {
		t.Fatalf("fault fixture changed %d rows; want one", count)
	}
}

func sourceJournalEnvelopes(t *testing.T, store *sourceJournalStore) []string {
	t.Helper()

	if store.database.Postgres != nil {
		rows, err := store.database.Postgres.Query(t.Context(), "SELECT envelope FROM boyan_canonical.source")
		sourceJournalNoError(t, err)

		defer rows.Close()

		var result []string

		for rows.Next() {
			var encoded []byte
			sourceJournalNoError(t, rows.Scan(&encoded))
			result = append(result, string(encoded))
		}

		sourceJournalNoError(t, rows.Err())

		return result
	}

	conn, prefix := sourceJournalRawDB(t, store.database)
	rows, err := conn.QueryContext(t.Context(), "SELECT envelope FROM "+prefix+"source")
	sourceJournalNoError(t, err)

	defer rows.Close()

	var result []string

	for rows.Next() {
		var encoded []byte
		sourceJournalNoError(t, rows.Scan(&encoded))
		result = append(result, string(encoded))
	}

	sourceJournalNoError(t, rows.Err())

	return result
}
