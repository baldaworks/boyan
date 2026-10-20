//go:build integration

//nolint:lll // Keep literal SQL fixtures and pre-authored controls readable together.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func lexicalProjection(t *testing.T, store *Store) memory.RecallProjection {
	t.Helper()

	projection, ok := any(store).(memory.RecallProjection)
	if !ok {
		t.Fatal("SQLite does not provide lexical retrieval")
	}

	return projection
}

// Removing any eligibility predicate lets its deliberately stronger decoy win.
//
//nolint:funlen // Each filter and time boundary has an explicit expected result set.
func TestLexicalFiltersAndTemporalBoundaries(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	projection := lexicalProjection(t, store)
	before, after := lexicalTestTime.Add(-time.Nanosecond), lexicalTestTime.Add(time.Nanosecond)
	fixtures := []lexicalFixture{
		{id: "eligible", raw: "needle filler extra", kind: "state", category: "decision", key: "chosen", sources: []string{"source-a", "source-b"}, conversations: []string{"conversation-a", "conversation-b"}, from: &lexicalTestTime},
		{id: "kind", raw: "needle", kind: "event", category: "decision", key: "chosen", sources: []string{"source-a"}, conversations: []string{"conversation-a"}},
		{id: "category", raw: "needle", category: "fact", key: "chosen", sources: []string{"source-a"}, conversations: []string{"conversation-a"}},
		{id: "key", raw: "needle", category: "decision", key: "other", sources: []string{"source-a"}, conversations: []string{"conversation-a"}},
		{id: "source", raw: "needle", category: "decision", key: "chosen", sources: []string{"other"}, conversations: []string{"conversation-a"}},
		{id: "conversation", raw: "needle", category: "decision", key: "chosen", sources: []string{"source-a"}, conversations: []string{"other"}},
		{id: "future-observed", raw: "needle", observed: &after}, {id: "future-valid", raw: "needle", from: &after},
		{id: "ended", raw: "needle", until: &lexicalTestTime}, {id: "expired", raw: "needle", expiry: &lexicalTestTime},
		{id: "until-next", raw: "needle", until: &after}, {id: "expiry-next", raw: "needle", expiry: &after},
		{id: "observed-before", raw: "needle", observed: &before},
	}
	seedLexical(t, store, "filters", lexicalTestScope, fixtures)
	seedLexical(t, store, "wrong-kind", memory.Scope{Key: lexicalTestScope.Key, Kind: memory.ScopeKindPersonal}, []lexicalFixture{{id: "wrong-scope", raw: "needle"}})

	state := memory.MemoryKindState
	category := memory.AtomCategoryDecision

	for _, tc := range []struct {
		name     string
		apply    func(*memory.RecallRequest)
		excluded string
	}{
		{"time", func(*memory.RecallRequest) {}, ""},
		{"kind", func(r *memory.RecallRequest) { r.Kind = &state }, "kind"},
		{"category", func(r *memory.RecallRequest) { r.Category = &category }, "category"},
		{"key", func(r *memory.RecallRequest) { r.MemoryKey = "chosen" }, "key"},
		{"source", func(r *memory.RecallRequest) { r.SourceID = "source-a" }, "source"},
		{"conversation", func(r *memory.RecallRequest) { r.ConversationID = "conversation-a" }, "conversation"},
		{"combined", func(r *memory.RecallRequest) {
			r.Kind = &state
			r.Category = &category
			r.MemoryKey = "chosen"
			r.SourceID = "source-a"
			r.ConversationID = "conversation-a"
		}, "combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 100, AsOf: &lexicalTestTime}
			tc.apply(&r)

			hits, err := projection.SearchRecall(t.Context(), r)
			if err != nil {
				t.Fatal(err)
			}

			got := lexicalHitIDs(t, hits)
			sort.Strings(got)

			var want []string
			if tc.name == "time" {
				want = []string{"category", "conversation", "eligible", "expiry-next", "key", "kind", "observed-before", "source", "until-next"}
			} else if tc.name == "combined" {
				want = []string{"eligible"}
			} else {
				want = []string{"category", "conversation", "eligible", "key", "kind", "source"}

				var filtered []string

				for _, id := range want {
					if id != tc.excluded {
						filtered = append(filtered, id)
					}
				}

				want = filtered
				if tc.name == "kind" {
					want = append(want, "expiry-next", "observed-before", "until-next")
					sort.Strings(want)
				}
			}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%v want=%v", got, want)
			}
		})
	}
}

//nolint:cyclop // Verify scope-size, depth and binary-tie invariants against real SQL.
func TestLexicalScopeSizeAndDepth(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	projection := lexicalProjection(t, store)

	fixtures := make([]lexicalFixture, 8193)
	for i := range fixtures {
		fixtures[i] = lexicalFixture{id: fmt.Sprintf("r%05d", i), raw: "needle"}
	}

	seedLexical(t, store, "large", lexicalTestScope, fixtures)

	for _, tc := range []struct{ limit, want int }{{1, 4}, {64, 256}, {100, 256}} {
		hits, err := projection.SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: tc.limit, AsOf: &lexicalTestTime})
		if err != nil {
			t.Fatal(err)
		}

		if len(hits) != tc.want || hits[0].RevisionID != "r00000" || hits[len(hits)-1].RevisionID != fmt.Sprintf("r%05d", tc.want-1) {
			t.Fatalf("cap/tie failure: %d hits", len(hits))
		}
	}
	// A match beyond the semantic bound must remain searchable in a lexical scope.
	err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE lexical_record SET memory_key='last' WHERE revision_id='r08192'`)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	hits, err := projection.SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", MemoryKey: "last", Limit: 1, AsOf: &lexicalTestTime})
	if err != nil {
		t.Fatal(err)
	}

	if len(hits) != 1 || hits[0].RevisionID != "r08192" {
		t.Fatalf("lexical scope capped: %+v", hits)
	}
}

func TestLexicalCloseCancellationAndUncertifiedMinimum(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	projection := lexicalProjection(t, store)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}
	if _, err := projection.SearchRecall(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}

	request.MinScopeChangeSeq = 1
	if _, err := projection.SearchRecall(t.Context(), request); err == nil {
		t.Fatal("uncertified minimum accepted")
	}

	request.MinScopeChangeSeq = 0

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := projection.SearchRecall(t.Context(), request); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

//nolint:cyclop // Keep the reader/publication/time/close sequence in one coherent transaction test.
func TestLexicalSnapshotPinsPhysicalBuildAndTime(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	after := lexicalTestTime.Add(time.Nanosecond)
	seedLexical(t, store, "old", lexicalTestScope, []lexicalFixture{{id: "old", raw: "needle", expiry: &after}})

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	build, err := snapshot.activeLexicalBuild(lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}

	seedLexical(t, store, "new", lexicalTestScope, []lexicalFixture{{id: "new", raw: "needle"}})
	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}
	// Captured request eligibility is not advanced when SQL publication changes.
	hits, err := snapshot.searchLexical(request, lexicalTestTime, build, 4)
	if err != nil {
		t.Fatal(err)
	}

	if got := lexicalHitIDs(t, hits); !reflect.DeepEqual(got, []string{"old"}) {
		t.Fatalf("pinned view: %v", got)
	}

	hits, err = lexicalProjection(t, store).SearchRecall(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if got := lexicalHitIDs(t, hits); !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatalf("current view: %v", got)
	}

	hits, err = snapshot.searchLexical(request, after, build, 4)
	if err != nil {
		t.Fatal(err)
	}

	if len(hits) != 0 {
		t.Fatal("exclusive expiry ignored")
	}
	// Lifecycle cancellation reaches the query's private snapshot context.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := snapshot.searchLexical(request, lexicalTestTime, build, 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot close: %v", err)
	}
}

func TestLexicalCapturedNowAndLiteralOperator(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	seedLexical(t, store, "now", lexicalTestScope, []lexicalFixture{
		{id: "now", raw: "or needle", observed: &past, expiry: &future},
		{id: "expired", raw: "needle", observed: &past, expiry: &past},
		{id: "future", raw: "needle", observed: &future},
		{id: "decoy", raw: "banana", observed: &past},
	})

	hits, err := lexicalProjection(t, store).SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "OR", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if got := lexicalHitIDs(t, hits); !reflect.DeepEqual(got, []string{"now"}) {
		t.Fatalf("OR was an operator: %v", got)
	}

	hits, err = lexicalProjection(t, store).SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if got := lexicalHitIDs(t, hits); !reflect.DeepEqual(got, []string{"now"}) {
		t.Fatalf("captured now: %v", got)
	}
	// Explicit time supersedes today's observation/expiry checks.
	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 10, AsOf: &future}

	hits, err = lexicalProjection(t, store).SearchRecall(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if got := lexicalHitIDs(t, hits); !reflect.DeepEqual(got, []string{"future"}) {
		t.Fatalf("explicit time: %v", got)
	}
}

func TestLexicalHybridChannelBound(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	fixtures := make([]lexicalFixture, 110)
	for i := range fixtures {
		fixtures[i] = lexicalFixture{id: fmt.Sprintf("r%03d", i), raw: "needle"}
	}

	seedLexical(t, store, "channel", lexicalTestScope, fixtures)

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}

	hits, err := snapshot.searchLexicalChannel(request, lexicalTestTime, "channel", 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(hits) != 100 {
		t.Fatalf("channel depth=%d", len(hits))
	}

	for _, depth := range []int{0, 101} {
		if _, err := snapshot.searchLexicalChannel(request, lexicalTestTime, "channel", depth); err == nil {
			t.Fatalf("accepted channel depth %d", depth)
		}
	}
}

type lexicalFixture struct {
	id, raw, ru, en               string
	text                          string
	kind, category, key           string
	observed, from, until, expiry *time.Time
	sources, conversations        []string
}

//nolint:cyclop,gocognit // Fixture setup propagates SQL failures without hiding transaction ownership.
func seedLexical(t *testing.T, store *Store, build string, scope memory.Scope, fixtures []lexicalFixture) {
	t.Helper()

	registerLexicalPrivacy(t, store, scope, fixtures)

	err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state) VALUES(?,?,?,?,?,'committed')`, build, scope.Kind, scope.Key, build, encodeUint64(math.MaxUint64)); err != nil {
			return err
		}

		for _, f := range fixtures {
			if f.text != "" {
				fields := analyzeLexicalText(f.text)
				f.raw, f.ru, f.en = fields.raw, fields.ru, fields.en
			}

			kind := f.kind
			if kind == "" {
				kind = "state"
			}

			observed := lexicalTestTime
			if f.observed != nil {
				observed = *f.observed
			}

			result, err := tx.ExecContext(ctx, `INSERT INTO lexical_record(physical_build_id,item_id,revision_id,revision,change_seq,kind,category,memory_key,created_at,observed_at,valid_from,valid_until,expires_at,raw_tokens,ru_tokens,en_tokens) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, build, f.id, f.id, encodeUint64(math.MaxUint64), encodeUint64(math.MaxUint64), kind, f.category, f.key, encodeTime(lexicalTestTime), encodeTime(observed), nullableFixtureTime(f.from), nullableFixtureTime(f.until), nullableFixtureTime(f.expiry), f.raw, f.ru, f.en)
			if err != nil {
				return err
			}

			row, err := result.LastInsertId()
			if err != nil {
				return err
			}

			if err := insertLexicalFTS(ctx, tx, row, f.raw, f.ru, f.en); err != nil {
				return err
			}

			for _, id := range f.sources {
				if _, err := tx.ExecContext(ctx, `INSERT INTO lexical_source VALUES(?,?)`, row, id); err != nil {
					return err
				}
			}

			for _, id := range f.conversations {
				if _, err := tx.ExecContext(ctx, `INSERT INTO lexical_conversation VALUES(?,?)`, row, id); err != nil {
					return err
				}
			}
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO lexical_active VALUES(?,?,?) ON CONFLICT(scope_kind,scope_key) DO UPDATE SET physical_build_id=excluded.physical_build_id`, scope.Kind, scope.Key, build)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func nullableFixtureTime(value *time.Time) any {
	if value == nil {
		return nil
	}

	return encodeTime(*value)
}

func lexicalHitIDs(t *testing.T, hits []memory.RecallProjectionHit) []string {
	t.Helper()

	var ids []string

	for _, hit := range hits {
		if hit.Scope != lexicalTestScope || hit.Revision != math.MaxUint64 || hit.ScopeChangeSeq != math.MaxUint64 || math.IsNaN(hit.Score) || math.IsInf(hit.Score, 0) || hit.Score < 0 || hit.Lexical != hit.Score || hit.Semantic != 0 {
			t.Fatalf("invalid identity/score: %+v", hit)
		}

		ids = append(ids, hit.RevisionID)
	}

	return ids
}

// Expectations are the pre-authored TASK-001/002 controls, independent of ranks.
func TestLexicalPreauthoredRelevance(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)
	seedLexical(t, store, "controls", lexicalTestScope, []lexicalFixture{
		{id: "ru", text: "Кошки гуляют возле домов"},
		{id: "en", text: "The engineers are running tests"},
		{id: "mixed", text: "Настройки database connections"},
		{id: "identifiers", text: "user_id HTTP/2 cache-key v1.2"},
		{id: "stopwords", text: "the и"},
		{id: "nfc", text: "cafe\u0301"},
		{id: "tie-a", text: "identical punctuation!!!"},
		{id: "tie-b", text: "identical punctuation!!!"},
		{id: "decoy", text: "banana volcano"},
	})

	projection := lexicalProjection(t, store)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"кошка", []string{"ru"}}, {"дом", []string{"ru"}}, {"кошка дом", []string{"ru"}},
		{"engineer", []string{"en"}}, {"run", []string{"en"}}, {"test", []string{"en"}}, {"engineer run test", []string{"en"}},
		{"настройка", []string{"mixed"}}, {"connection", []string{"mixed"}}, {"настройка connection", []string{"mixed"}},
		{"user_id", []string{"identifiers"}}, {"cache-key", []string{"identifiers"}}, {"HTTP/2", []string{"identifiers"}}, {"v1.2", []string{"identifiers"}},
		{"the и", []string{"en", "stopwords"}}, {"café", []string{"nfc"}}, {"cafe\u0301", []string{"nfc"}},
		{"punctuation", []string{"tie-a", "tie-b"}}, {`"кошка"`, []string{"ru"}}, {`" OR banana --`, []string{"decoy"}}, {"!!!", nil},
		{"OR", nil}, {`NEAR(foo) : * - + {}`, nil}, {`'; DROP TABLE lexical_record; --`, nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			for range 3 {
				hits, err := projection.SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: tc.query, Limit: 10, AsOf: &lexicalTestTime})
				if err != nil {
					t.Fatal(err)
				}

				got := lexicalHitIDs(t, hits)
				if tc.query != "punctuation" {
					sort.Strings(got)
				}

				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got=%v want=%v", got, tc.want)
				}
			}
		})
	}
}
