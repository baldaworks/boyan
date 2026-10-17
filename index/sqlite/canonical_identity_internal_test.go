//nolint:cyclop,lll // Exact identity and rollback fixtures keep explicit real SQL assertions.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalByteNonemptyIdentitiesAndMemberships(t *testing.T) {
	store, _ := openTestStore(t, false)
	record := applyRecord("\x00revision")
	record.ItemID = "\x00item"
	record.Scope = memory.Scope{Kind: lexicalTestScope.Kind, Key: "\x00channel:address"}
	record.SourceIDs = []string{"\x00source", "plain\x00tail", "\x00source", "\x00"}
	record.ConversationIDs = []string{"\x00conversation", "plain\x00tail", "\x00conversation", "\x00"}
	reader := &applyReader{records: map[string]memory.RecallRecord{record.RevisionID: record}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), record.Scope, "\x00generation", nil, []memory.MemoryRevision{{RevisionID: record.RevisionID, ItemID: record.ItemID, Revision: 1}}); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"lexical_source", "lexical_conversation"} {
		var count int
		if err = store.reader.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 3 {
			t.Fatalf("%s lost byte identities or duplicate handling: %d %v", table, count, err)
		}

		if _, err = store.writer.ExecContext(t.Context(), `INSERT INTO `+table+` SELECT rowid,'' FROM lexical_record`); err == nil {
			t.Fatalf("%s accepted an actually empty identity", table)
		}
	}
	// Controlled fixture sealing makes real FTS/exact filters observable.
	if err = store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE lexical_build SET state='committed'`); err != nil {
			return err
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO lexical_active SELECT scope_kind,scope_key,physical_build_id FROM lexical_build`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.SearchRecall(t.Context(), memory.RecallRequest{Scope: record.Scope, Query: "needle", SourceID: "\x00source", ConversationID: "\x00conversation", Limit: 1, AsOf: &lexicalTestTime})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != record.RevisionID || hits[0].ItemID != record.ItemID {
		t.Fatalf("exact byte identity/filter changed: %+v %v", hits, err)
	}
}

func TestCanonicalMembershipUnexpectedUniqueConstraintPropagates(t *testing.T) {
	for _, table := range []string{"lexical_source", "lexical_conversation"} {
		t.Run(table, func(t *testing.T) {
			store, _ := openTestStore(t, false)
			if _, err := store.writer.ExecContext(t.Context(), `CREATE UNIQUE INDEX unexpected_identity_unique ON `+table+`(id)`); err != nil {
				t.Fatal(err)
			}

			reader := &applyReader{records: map[string]memory.RecallRecord{"first": applyRecord("first"), "second": applyRecord("second")}}

			applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
			if err != nil {
				t.Fatal(err)
			}

			err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "first"}, {RevisionID: "second"}})

			var classified *memory.Error
			if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure {
				t.Fatalf("unexpected unique conflict was suppressed: %v", err)
			}

			var count int
			if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("unique conflict left partial page: %d %v", count, err)
			}
		})
	}
}

func TestCanonicalMembershipOnlySuppressesExactDuplicateConflicts(t *testing.T) {
	for _, table := range []string{"lexical_source", "lexical_conversation"} {
		t.Run(table, func(t *testing.T) {
			store, _ := openTestStore(t, false)
			if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, `CREATE TABLE reject_membership(value INTEGER CHECK(value>0))`); err != nil {
					return err
				}

				_, err := tx.ExecContext(ctx, `CREATE TRIGGER membership_check BEFORE INSERT ON `+table+` BEGIN INSERT INTO reject_membership VALUES(0); END`)

				return err
			}); err != nil {
				t.Fatal(err)
			}

			applier, err := NewSQLiteCanonicalApplier(store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}, fixtureCanonicalPrivacy(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}))
			if err != nil {
				t.Fatal(err)
			}

			err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "r"}})

			var classified *memory.Error
			if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure {
				t.Fatalf("membership CHECK was silently ignored: %v", err)
			}

			for _, relation := range []string{"lexical_record", "lexical_fts", "lexical_source", "lexical_conversation"} {
				var count int
				if err = store.reader.QueryRow(`SELECT count(*) FROM ` + relation).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed membership committed %s: %d %v", relation, count, err)
				}
			}
		})
	}
}

func TestCanonicalSemanticScopeUsesByteNonemptyIdentity(t *testing.T) {
	store, _ := openTestStore(t, true)

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "\x00channel:address"}
	if err := scope.Validate(); err != nil {
		t.Fatal(err)
	}

	seedZeroCoverage(t, store, "semantic", scope, "revision")

	var exact string
	if err := store.reader.QueryRow(`SELECT scope_key FROM semantic_build WHERE build_id='semantic'`).Scan(&exact); err != nil || exact != scope.Key {
		t.Fatalf("canonical scope changed: %q %v", exact, err)
	}
}
