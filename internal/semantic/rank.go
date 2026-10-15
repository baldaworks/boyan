package semantic

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"
)

// Chunk is one storage-independent revision passage vector.
type Chunk struct {
	RevisionID string
	Ordinal    int
	Vector     []float32
}

// DenseMatch records one revision's maximum cosine and winning chunk.
type DenseMatch struct {
	RevisionID   string
	ChunkOrdinal int
	Score        float64
}

// Rank exact-scans bounded chunks and retains one maximum-cosine result per
// revision. Equal scores use revision ID, and equal evidence uses ordinal.
func Rank(ctx context.Context, queries [][]float32, chunks []Chunk, limit int) ([]DenseMatch, error) {
	dimensions, err := validateRankInput(ctx, queries, chunks, limit)
	if err != nil {
		return nil, err
	}

	winners, err := rankChunks(ctx, queries, chunks, dimensions, limit)
	if err != nil {
		return nil, err
	}

	ranked := make([]DenseMatch, 0, len(winners))
	for _, winner := range winners {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ranked = append(ranked, winner)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Slice(ranked, func(left, right int) bool {
		if ranked[left].Score != ranked[right].Score {
			return ranked[left].Score > ranked[right].Score
		}

		return ranked[left].RevisionID < ranked[right].RevisionID
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return ranked[:min(len(ranked), limit)], nil
}

func rankChunks(
	ctx context.Context,
	queries [][]float32,
	chunks []Chunk,
	dimensions int,
	limit int,
) (map[string]DenseMatch, error) {
	winners := make(map[string]DenseMatch, min(len(chunks), limit))
	for _, chunk := range chunks {
		match, err := rankChunk(ctx, queries, chunk, dimensions)
		if err != nil {
			return nil, err
		}

		previous, found := winners[chunk.RevisionID]
		if !found || match.Score > previous.Score ||
			match.Score == previous.Score && match.ChunkOrdinal < previous.ChunkOrdinal {
			winners[chunk.RevisionID] = match
		}
	}

	return winners, nil
}

func rankChunk(ctx context.Context, queries [][]float32, chunk Chunk, dimensions int) (DenseMatch, error) {
	if err := ctx.Err(); err != nil {
		return DenseMatch{}, err
	}

	if !validRevisionID(chunk.RevisionID) || chunk.Ordinal < 0 {
		return DenseMatch{}, newError(ReasonInvalidInput)
	}

	if err := ValidateVector(chunk.Vector, dimensions); err != nil {
		return DenseMatch{}, err
	}

	best, err := maximumCosine(ctx, queries, chunk.Vector)
	if err != nil {
		return DenseMatch{}, err
	}

	return DenseMatch{RevisionID: chunk.RevisionID, ChunkOrdinal: chunk.Ordinal, Score: best}, nil
}

func validateRankInput(ctx context.Context, queries [][]float32, chunks []Chunk, limit int) (int, error) {
	if ctx == nil {
		return 0, newError(ReasonInvalidInput)
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if len(queries) == 0 {
		return 0, newError(ReasonInvalidInput)
	}

	if len(queries) > QueryWindowLimit || limit < 1 || limit > SemanticCandidateLimit {
		return 0, newError(ReasonInputLimit)
	}

	if len(chunks) > ProjectionChunkLimit {
		return 0, newError(ReasonProjectionCapacity)
	}

	dimensions := len(queries[0])

	for _, query := range queries {
		if err := ValidateVector(query, dimensions); err != nil {
			return 0, err
		}
	}

	return dimensions, nil
}

func maximumCosine(ctx context.Context, queries [][]float32, vector []float32) (float64, error) {
	best := -1.0

	for _, query := range queries {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		score, err := Cosine(query, vector)
		if err != nil {
			return 0, err
		}

		best = max(best, score)
	}

	return best, nil
}

func validRevisionID(revisionID string) bool {
	return revisionID != "" && strings.TrimSpace(revisionID) == revisionID &&
		!strings.ContainsAny(revisionID, "\r\n\t") && utf8.ValidString(revisionID)
}
