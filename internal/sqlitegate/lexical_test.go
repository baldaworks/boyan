//go:build integration

package sqlitegate_test

import (
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/kljensen/snowball/english"
	"github.com/kljensen/snowball/russian"
	"golang.org/x/text/unicode/norm"
)

type lexicalControl struct{ id, text string }

func lexicalControls() []lexicalControl {
	return []lexicalControl{
		{"ru", "Кошки гуляют возле домов"},
		{"en", "The engineers are running tests"},
		{"mixed", "Настройки database connections"},
		{"identifiers", "user_id HTTP/2 cache-key v1.2"},
		{"stopwords", "the и"},
		{"nfc", "cafe\u0301"},
		{"tie-a", "identical punctuation!!!"},
		{"tie-b", "identical punctuation!!!"},
		{"decoy", "banana volcano"},
	}
}

func analyzedFields(text string) (string, string, string) {
	tokens := strings.FieldsFunc(strings.ToLower(norm.NFC.String(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var ru, en []string

	for _, token := range tokens {
		cyrillic, latin := false, false
		for _, r := range token {
			cyrillic = cyrillic || unicode.In(r, unicode.Cyrillic)
			latin = latin || unicode.In(r, unicode.Latin)
		}

		if cyrillic {
			ru = append(ru, russian.Stem(token, false))
		}

		if latin {
			en = append(en, english.Stem(token, false))
		}
	}

	return strings.Join(tokens, " "), strings.Join(ru, " "), strings.Join(en, " ")
}

func literalMatch(text string) string {
	raw, ru, en := analyzedFields(text)

	var clauses []string

	for _, field := range []struct{ name, value string }{{"raw_tokens", raw}, {"ru_tokens", ru}, {"en_tokens", en}} {
		for _, token := range strings.Fields(field.value) {
			clauses = append(clauses, field.name+`:"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
		}
	}

	return strings.Join(clauses, " OR ")
}

// These expectations were authored in TASK-001 before observing this task's
// old-engine ranks. Both gates reuse the same corpus and relevance policy.
func lexicalRelevanceQueries() []struct {
	query string
	want  []string
} {
	return []struct {
		query string
		want  []string
	}{
		{"кошка", []string{"ru"}}, {"дом", []string{"ru"}}, {"кошка дом", []string{"ru"}},
		{"engineer", []string{"en"}}, {"run", []string{"en"}}, {"test", []string{"en"}},
		{"engineer run test", []string{"en"}},
		{"настройка", []string{"mixed"}}, {"connection", []string{"mixed"}}, {"настройка connection", []string{"mixed"}},
		{"user_id", []string{"identifiers"}}, {"cache-key", []string{"identifiers"}},
		{"HTTP/2", []string{"identifiers"}}, {"v1.2", []string{"identifiers"}},
		{"the и", []string{"en", "stopwords"}}, {"café", []string{"nfc"}},
		{"punctuation", []string{"tie-a", "tie-b"}}, {`"кошка"`, []string{"ru"}},
		{`" OR banana --`, []string{"decoy"}}, {"!!!", nil},
	}
}

// Dropping a language stem field loses required inflections; emitting caller
// MATCH syntax broadens injection controls or turns punctuation into errors.
func TestPinnedAnalyzerRelevance(t *testing.T) {
	t.Parallel()
	db, _, _ := openFixture(t)
	execFixture(t, db, `CREATE VIRTUAL TABLE gate_fts USING fts5(
	 id UNINDEXED,raw_tokens,ru_tokens,en_tokens,tokenize='unicode61 remove_diacritics 0')`)

	for _, doc := range lexicalControls() {
		raw, ru, en := analyzedFields(doc.text)
		execFixture(t, db, `INSERT INTO gate_fts VALUES (?,?,?,?)`, doc.id, raw, ru, en)
	}

	for _, tc := range lexicalRelevanceQueries() {
		t.Run(tc.query, func(t *testing.T) {
			for attempt := 0; attempt < 3; attempt++ {
				got := lexicalIDs(t, db, tc.query)

				if tc.query != "punctuation" {
					sort.Strings(got)
				}

				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("query=%q got=%v want=%v", tc.query, got, tc.want)
				}
			}
		})
	}
}

func lexicalIDs(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()

	match := literalMatch(query)
	if match == "" {
		return nil
	}

	rows, err := db.QueryContext(t.Context(),
		`SELECT id FROM gate_fts WHERE gate_fts MATCH ? ORDER BY bm25(gate_fts),id COLLATE BINARY`, match)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return ids
}
