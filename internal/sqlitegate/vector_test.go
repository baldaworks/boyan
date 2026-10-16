package sqlitegate_test

import (
	"database/sql"
	"math"
	"reflect"
	"strings"
	"testing"
)

// This gate catches an extension whose scalar BLOB contracts cannot validate
// stored vectors without copying a full generation into Go.
func TestVectorIntegrityAndCosine(t *testing.T) {
	t.Parallel()

	db, _, _ := openFixture(t)
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{"unit", blob(0.6, 0.8), true}, {"zero", blob(0, 0), false},
		{"wrong norm", blob(0.5, 0.5), false}, {"wrong dimension", blob(1), false},
		{"malformed", []byte{1, 2, 3}, false}, {"text", "[1,0]", false},
		{"nan", blob(float32(math.NaN()), 0), false},
		{"positive infinity", blob(float32(math.Inf(1)), 0), false},
		{"negative infinity", blob(float32(math.Inf(-1)), 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				kind, vectorType string
				dimensions       int
				norm             sql.NullFloat64
			)

			err := db.QueryRowContext(t.Context(),
				`SELECT typeof(?),vec_type(?),vec_length(?),vec_distance_L2(?,?)`,
				tc.value, tc.value, tc.value, tc.value, blob(0, 0),
			).Scan(&kind, &vectorType, &dimensions, &norm)

			valid := err == nil && kind == "blob" && vectorType == "float32" && dimensions == 2 && validNorm(norm)
			if valid != tc.want {
				t.Fatalf("valid=%v want=%v; kind=%s dimensions=%d norm=%+v err=%v", valid, tc.want, kind, dimensions, norm, err)
			}

			t.Logf("kind=%s dimensions=%d norm=%+v err=%v", kind, dimensions, norm, err)
		})
	}

	assertCosine(t, db)
}

func validNorm(norm sql.NullFloat64) bool {
	return norm.Valid && !math.IsNaN(norm.Float64) && !math.IsInf(norm.Float64, 0) &&
		math.Abs(norm.Float64*norm.Float64-1) <= 0.001
}

func assertCosine(t *testing.T, db *sql.DB) {
	t.Helper()

	var (
		typ    string
		cosine float64
	)

	err := db.QueryRowContext(t.Context(), `SELECT vec_type(?),1-vec_distance_cosine(?,?)`,
		blob(0.6, 0.8), blob(0.6, 0.8), blob(1, 0)).Scan(&typ, &cosine)
	if err != nil {
		t.Fatal(err)
	}

	if typ != "float32" || math.Abs(cosine-0.6) > 1e-6 {
		t.Fatalf("type=%s cosine=%g", typ, cosine)
	}
}

const rankSQL = `WITH query_windows(q) AS (VALUES (?),(?)),
eligible AS MATERIALIZED (
 SELECT revision,embedding FROM chunks WHERE scope_kind=? AND scope_key=? AND allowed=1
), per_revision AS (
 SELECT revision,max(max(-1.0,min(1.0,1.0-vec_distance_cosine(embedding,q)))) AS similarity
 FROM eligible CROSS JOIN query_windows GROUP BY revision
)
SELECT revision,similarity FROM per_revision ORDER BY similarity DESC,revision COLLATE BINARY ASC LIMIT ?`

// Invalid, excluded payloads make premature distance evaluation fail; repeated
// top chunks of one revision catch a chunk LIMIT applied before revision MAX.
func TestMaterializedFilteringAndRevisionMaxima(t *testing.T) {
	t.Parallel()
	db, _, _ := openFixture(t)
	execFixture(t, db, `CREATE TABLE chunks(revision TEXT,scope_kind TEXT,scope_key TEXT,allowed INTEGER,embedding BLOB)`)

	for i := 0; i < 20; i++ {
		execFixture(t, db, `INSERT INTO chunks VALUES ('a','personal','same',1,?)`, blob(1, 0))
	}

	execFixture(t, db, `INSERT INTO chunks VALUES ('b','personal','same',1,?)`, blob(0, 1))
	execFixture(t, db, `INSERT INTO chunks VALUES ('c','personal','same',1,?)`, blob(0.6, 0.8))
	execFixture(t, db, `INSERT INTO chunks VALUES ('wrong-kind','shared','same',1,?)`, []byte{1})
	execFixture(t, db, `INSERT INTO chunks VALUES ('wrong-key','personal','other',1,?)`, []byte{1})
	execFixture(t, db, `INSERT INTO chunks VALUES ('excluded','personal','same',0,?)`, []byte{1})

	for _, tc := range []struct {
		name, key string
		k         int
		want      []string
		scores    []float64
	}{
		{"top two", "same", 2, []string{"a", "b"}, []float64{1, 1}},
		{"fewer than k", "same", 9, []string{"a", "b", "c"}, []float64{1, 1, 0.8}},
		{"empty", "missing", 9, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(t.Context(), rankSQL, blob(1, 0), blob(0, 1), "personal", tc.key, tc.k)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			var got []string

			for rows.Next() {
				var (
					id    string
					score float64
				)
				if err := rows.Scan(&id, &score); err != nil {
					t.Fatal(err)
				}

				got = append(got, id)
				if math.Abs(score-tc.scores[len(got)-1]) > 1e-6 {
					t.Fatalf("%s cosine=%g want=%g", id, score, tc.scores[len(got)-1])
				}
			}

			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}

	assertMaterializedPlan(t, db)
}

func assertMaterializedPlan(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+rankSQL, blob(1, 0), blob(0, 1), "personal", "same", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	materialized := false

	for rows.Next() {
		var (
			id, parent, unused int
			detail             string
		)
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}

		t.Log(detail)
		materialized = materialized || strings.Contains(detail, "MATERIALIZE eligible")
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if !materialized {
		t.Fatal("eligible filter was not materialized")
	}
}
