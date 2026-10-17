//nolint:cyclop,lll // Real SQL callbacks cancel while an atomic record mutation is in flight.
package sqlite

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
	driver "modernc.org/sqlite"
)

var cancellationFunctionSequence atomic.Uint64

//nolint:funlen // Insert and copy share the same real SQL cancellation/rollback oracle.
func TestCanonicalCancelDuringOversizedMembershipAndCopy(t *testing.T) {
	for _, copying := range []bool{false, true} {
		name := "cancel_large_insert"
		if copying {
			name = "cancel_large_copy"
		}

		name = fmt.Sprintf("%s_%d", name, cancellationFunctionSequence.Add(1))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		if err := driver.RegisterScalarFunction(name, 0, func(*driver.FunctionContext, []sqldriver.Value) (sqldriver.Value, error) {
			cancel()

			return int64(0), nil
		}); err != nil {
			t.Fatal(err)
		}

		store, _ := openTestStore(t, false)

		large := strings.Repeat("c", lexicalPageBytes+1)
		if copying {
			seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "old", raw: "needle", conversations: []string{large, "zz-cancel"}}})
		}

		if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TRIGGER cancel_membership AFTER INSERT ON lexical_conversation WHEN NEW.id='zz-cancel' BEGIN SELECT `+name+`(); END`)

			return err
		}); err != nil {
			t.Fatal(err)
		}

		record := applyRecord("new")
		record.ConversationIDs = []string{large, "zz-cancel"}

		applier, err := NewSQLiteCanonicalApplier(store, &applyReader{records: map[string]memory.RecallRecord{"new": record}}, fixtureCanonicalPrivacy(t, store, &applyReader{records: map[string]memory.RecallRecord{"new": record}}))
		if err != nil {
			t.Fatal(err)
		}

		generation := "new"
		if copying {
			generation = "active"
		}

		err = applier.Apply(ctx, lexicalTestScope, generation, nil, []memory.MemoryRevision{{RevisionID: "new"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatal("mutation did not preserve cancellation", err)
		}

		var records, fts, members int
		if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record WHERE physical_build_id=?`, applier.stages[lexicalTestScope].id).Scan(&records); err != nil {
			t.Fatal(err)
		}

		if records != 0 {
			t.Fatal("partial large record survived", records)
		}

		if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_fts`).Scan(&fts); err != nil {
			t.Fatal(err)
		}

		if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_conversation`).Scan(&members); err != nil {
			t.Fatal(err)
		}

		wantFTS, wantMembers := 0, 0
		if copying {
			wantFTS, wantMembers = 1, 2
		}

		if fts != wantFTS || members != wantMembers {
			t.Fatalf("FTS/membership partial mutation: %d %d", fts, members)
		}
	}
}

func TestCanonicalCancelDuringOversizedScrubPreservesPinnedReader(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	name := fmt.Sprintf("cancel_large_scrub_%d", cancellationFunctionSequence.Add(1))
	if err := driver.RegisterScalarFunction(name, 0, func(*driver.FunctionContext, []sqldriver.Value) (sqldriver.Value, error) {
		cancel()

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	store, _ := openTestStore(t, false)
	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "large", raw: "needle", conversations: []string{strings.Repeat("c", lexicalPageBytes+1)}}})

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	build, err := snapshot.activeLexicalBuild(lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}

	if err = store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER cancel_scrub BEFORE DELETE ON lexical_record BEGIN SELECT `+name+`(); END`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{}, fixtureCanonicalPrivacy(t, store, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(ctx, lexicalTestScope, nil, []string{"large"}); !errors.Is(err, context.Canceled) {
		t.Fatal("scrub cancellation", err)
	}

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}

	hits, err := snapshot.searchLexical(request, lexicalTestTime, build, 4)
	if err != nil || len(hits) != 1 {
		t.Fatalf("pinned reader lost: %v %v", hits, err)
	}

	hits, err = store.SearchRecall(t.Context(), request)
	if err != nil || len(hits) != 1 {
		t.Fatalf("current page rollback lost: %v %v", hits, err)
	}
}
