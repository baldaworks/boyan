package storefixture_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type mutationProbe struct {
	connection *canonicalsql.Connection
	exec       func(string, ...any) error
	table      func(string) string
	clock      func() ([]byte, error)
}

func newMutationProbe(t *testing.T, fixture *storefixture.Fixture) mutationProbe {
	t.Helper()

	if fixture.Database.Postgres != nil {
		conn := fixture.Database.Postgres

		return mutationProbe{connection: canonicalsql.NewPostgres(conn), exec: func(statement string, args ...any) error {
			_, err := conn.Exec(t.Context(), statement, args...)

			return err
		}, table: func(name string) string { return "boyan_canonical." + name }, clock: func() ([]byte, error) {
			var clock []byte

			err := conn.QueryRow(t.Context(), "SELECT mutation_clock FROM boyan_canonical.store_meta").Scan(&clock)

			return clock, err
		}}
	}

	db, err := sql.Open("sqlite", fixture.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return mutationProbe{connection: canonicalsql.NewSQLite(conn), exec: func(statement string, args ...any) error {
		_, err := conn.ExecContext(t.Context(), statement, args...)

		return err
	}, table: func(name string) string { return name }, clock: func() ([]byte, error) {
		var clock []byte

		err := conn.QueryRowContext(t.Context(), "SELECT mutation_clock FROM store_meta").Scan(&clock)

		return clock, err
	}}
}

func (p mutationProbe) read(t *testing.T, scope memory.Scope, family canonicalsql.Family, id string, target any) {
	t.Helper()

	key := canonicalsql.Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(id)}}

	err := p.connection.View(t.Context(), func(tx *canonicalsql.Tx) error {
		data, err := tx.Get(t.Context(), key)
		if err != nil {
			return err
		}

		return canonicalsql.Decode(family, data, target)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (p mutationProbe) write(t *testing.T, key canonicalsql.Key, value any) {
	t.Helper()

	data, err := canonicalsql.Encode(key.Family, value)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		return tx.Put(t.Context(), key,
			data)
	}); err != nil {
		t.Fatal(err)
	}
}

func (p mutationProbe) remove(t *testing.T, key canonicalsql.Key) {
	t.Helper()

	if err := p.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		return tx.Delete(t.Context(),
			key)
	}); err != nil {
		t.Fatal(err)
	}
}

func forEachMutation(t *testing.T, run func(*testing.T, *storefixture.Fixture, mutationStore, mutationProbe)) {
	t.Helper()
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(mutationStore)
		if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		run(t, f, s, newMutationProbe(t, f))
	})
}

func privacyState(t *testing.T, probe mutationProbe, scope memory.Scope, id string) string {
	t.Helper()

	var record struct {
		State string `json:"state"`
	}
	probe.read(t, scope, canonicalsql.SourcePrivacy, id, &record)

	return record.State
}

func (p mutationProbe) failInserts(t *testing.T, table string) func() {
	t.Helper()

	var create, drop string
	if p.table(table) == table {
		create = "CREATE TRIGGER task7_fail BEFORE INSERT ON " + table +
			" BEGIN SELECT RAISE(ABORT,'task7 injected failure'); END"
		drop = "DROP TRIGGER task7_fail"
	} else {
		if err := p.exec("CREATE FUNCTION boyan_canonical.task7_fail() RETURNS trigger LANGUAGE plpgsql AS $$ " +
			"BEGIN RAISE EXCEPTION 'task7 injected failure'; END $$"); err != nil {
			t.Fatal(err)
		}

		create = "CREATE TRIGGER task7_fail BEFORE INSERT ON " + p.table(table) +
			" FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task7_fail()"
		drop = "DROP TRIGGER task7_fail ON " + p.table(table)
	}

	if err := p.exec(create); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := p.exec(drop); err != nil {
			t.Fatal(err)
		}

		if p.table(table) != table {
			if err := p.exec("DROP FUNCTION boyan_canonical.task7_fail()"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (p mutationProbe) stamps(t *testing.T) []canonicalsql.Stamp {
	t.Helper()

	var result []canonicalsql.Stamp

	if err := p.connection.View(t.Context(), func(tx *canonicalsql.Tx) error {
		var err error

		result, err = tx.ScanStamps(t.Context(), nil, memory.MaxCanonicalReadRecords)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return result
}

func assertMutationCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != want {
		t.Fatalf("code=%s want=%s error=%v", code, want, err)
	}
}

func assertMutationState(t *testing.T, store mutationStore, scope memory.Scope, want uint64) {
	t.Helper()

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != want || state.ChangeSeq != want {
		t.Fatalf("scope=%+v error=%v want=%d", state, err, want)
	}
}
