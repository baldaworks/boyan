package memory

import (
	"math"
	"sort"
	"time"
)

// RankRecall combines deterministic projection score and freshness.
func RankRecall(score float64, createdAt, now time.Time) RecallScore {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		score = 0
	}

	freshness := 0.0

	if !createdAt.IsZero() && !now.Before(createdAt) {
		age := now.Sub(createdAt).Hours()
		freshness = 1 / (1 + age/24)
	}

	return RecallScore{Lexical: score, Freshness: freshness, ExactScope: 1, Total: score + freshness + 1}
}

// RankRecallProjection preserves legacy raw lexical scores unless a projection
// supplies channel contributions. Invalid derived contributions fail closed.
func RankRecallProjection(hit RecallProjectionHit, createdAt, now time.Time) (RecallScore, error) {
	for _, value := range []float64{hit.Lexical, hit.Semantic} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return RecallScore{}, invalidDerived("recall projection contribution is invalid")
		}
	}

	if hit.Lexical == 0 && hit.Semantic == 0 {
		return RankRecall(hit.Score, createdAt, now), nil
	}

	score := RankRecall(hit.Lexical, createdAt, now)
	score.Semantic = hit.Semantic

	score.Total = score.Lexical + score.Semantic + score.Freshness + score.ExactScope
	if math.IsInf(score.Total, 0) {
		return RecallScore{}, invalidDerived("recall projection total is invalid")
	}

	return score, nil
}

// SortRecallReferences applies the stable score/freshness/revision tie-breaks.
func SortRecallReferences(values []RecallReference) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Score != values[j].Score {
			return values[i].Score > values[j].Score
		}

		if !values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].CreatedAt.After(values[j].CreatedAt)
		}

		return values[i].RevisionID < values[j].RevisionID
	})
}
