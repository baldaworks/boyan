//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func execTest(t *testing.T, tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, statement string, args ...any) {
	t.Helper()

	if _, err := tx.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaOwnsFTSAndMembershipRows(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	err := store.write(t.Context(), func(_ context.Context, tx *sql.Tx) error {
		execTest(t, tx, `INSERT INTO lexical_build
		 (physical_build_id,scope_kind,scope_key,generation_id,watermark) VALUES('b','user','u','g',?)`, encodeUint64(0))
		execTest(t, tx, `INSERT INTO lexical_record
		 (physical_build_id,item_id,revision_id,revision,change_seq,kind,created_at,observed_at)
		 VALUES('b','i','r',?,?,'fact',?,?)`,
			encodeUint64(1), encodeUint64(1), encodeTime(time.Time{}), encodeTime(time.Time{}))
		execTest(t, tx, `INSERT INTO lexical_source VALUES(1,'source')`)
		execTest(t, tx, `INSERT INTO lexical_conversation VALUES(1,'conversation')`)

		if err := insertLexicalFTS(t.Context(), tx, 1, "secret", "", ""); err != nil {
			return err
		}

		if _, err := tx.ExecContext(t.Context(), `INSERT INTO lexical_active VALUES('user','other','b')`); err == nil {
			t.Fatal("cross-scope active pointer accepted")
		}

		if err := insertLexicalFTS(t.Context(), tx, 999, "orphan", "", ""); err == nil {
			t.Fatal("orphan FTS accepted")
		}

		if err := deleteLexicalBuild(t.Context(), tx, "b"); err != nil {
			return err
		}

		for _, table := range []string{"lexical_record", "lexical_source", "lexical_conversation", "lexical_fts"} {
			var n int
			if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
				return err
			}

			if n != 0 {
				t.Fatalf("orphan %s", table)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSemanticSchemaIdentityDimensionsAndCascades(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, true)
	execTest(t, store.writer, `INSERT INTO semantic_build
	 (build_id,scope_kind,scope_key,generation_id,physical_build_id,watermark,model_space,dimensions,epoch,status)
	 VALUES('b','user','u','g','physical',?,'model',2,?,'building')`, encodeUint64(1), encodeUint64(0))
	execTest(t, store.writer, `INSERT INTO semantic_coverage VALUES('b','i','r',?,'hash',1)`, encodeUint64(1))

	const insert = `INSERT INTO semantic_chunk
	 (build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,
	 dimensions,embedding,kind,created_at,observed_at)
	 VALUES('b',?,'r',?,0,'hash','input',?,?,?,'fact',?,?)`
	for _, invalid := range []struct {
		item       string
		dimensions int
		vector     []byte
	}{
		{"wrong-item", 2, make([]byte, 8)}, {"i", 3, make([]byte, 12)}, {"i", 2, make([]byte, 4)},
	} {
		if _, err := store.writer.ExecContext(t.Context(), insert, invalid.item, encodeUint64(1), encodeUint64(1),
			invalid.dimensions, invalid.vector, encodeTime(time.Time{}), encodeTime(time.Time{})); err == nil {
			t.Fatal("invalid chunk accepted")
		}
	}

	execTest(t, store.writer, insert, "i", encodeUint64(1), encodeUint64(1), 2, make([]byte, 8),
		encodeTime(time.Time{}), encodeTime(time.Time{}))
	execTest(t, store.writer, `INSERT INTO semantic_source VALUES(1,'s')`)
	execTest(t, store.writer, `INSERT INTO semantic_conversation VALUES(1,'c')`)

	const wrongScope = `INSERT INTO semantic_active VALUES('user','other','b',?)`
	if _, err := store.writer.ExecContext(t.Context(), wrongScope, encodeUint64(0)); err == nil {
		t.Fatal("cross-scope semantic pointer accepted")
	}

	execTest(t, store.writer, `INSERT INTO semantic_active VALUES('user','u','b',?)`, encodeUint64(0))
	execTest(t, store.writer, `DELETE FROM semantic_build WHERE build_id='b'`)

	for _, table := range []string{
		"semantic_chunk", "semantic_coverage", "semantic_source", "semantic_conversation", "semantic_active",
	} {
		var count int
		if err := store.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatalf("orphan %s", table)
		}
	}
}

func TestEnableThenDisableRetainsSemanticState(t *testing.T) {
	t.Parallel()

	store, dir := openTestStore(t, false)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	enabled, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := enabled.Close(); err != nil {
		t.Fatal(err)
	}

	disabled, err := Open(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disabled.Close() }()

	var count int

	const semanticCount = "SELECT count(*) FROM sqlite_schema WHERE name='semantic_build'"
	if err := disabled.reader.QueryRowContext(t.Context(), semanticCount).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatal("existing semantic schema erased")
	}
}
