package sqlitegate_test

import (
	"math"
	"testing"
)

func TestDimensionRangeAndNearTiePrecision(t *testing.T) {
	t.Parallel()
	db, _, _ := openFixture(t)

	for _, dimensions := range []int{1, 768, 4096} {
		unit, zero := make([]float32, dimensions), make([]float32, dimensions)
		unit[0] = 1

		var (
			length           int
			norm, similarity float64
		)

		err := db.QueryRowContext(t.Context(),
			`SELECT vec_length(?),vec_distance_L2(?,?),1-vec_distance_cosine(?,?)`,
			blob(unit...), blob(unit...), blob(zero...), blob(unit...), blob(unit...),
		).Scan(&length, &norm, &similarity)
		if err != nil {
			t.Fatal(err)
		}

		if length != dimensions || norm != 1 || similarity != 1 {
			t.Fatalf("dimensions=%d length=%d norm=%g similarity=%g", dimensions, length, norm, similarity)
		}
	}

	var similarity float64

	err := db.QueryRowContext(t.Context(), `SELECT 1-vec_distance_cosine(?,?)`,
		blob(1, 0.0001), blob(1, 0)).Scan(&similarity)
	if err != nil {
		t.Fatal(err)
	}

	oracle := 1 / math.Sqrt(1+1e-8)
	if math.Abs(similarity-oracle) > 1e-6 {
		t.Fatalf("near tie similarity=%g oracle=%g", similarity, oracle)
	}

	t.Logf("near tie SQL cosine=%.17g independent double oracle=%.17g delta=%.3g", similarity, oracle, similarity-oracle)
}
