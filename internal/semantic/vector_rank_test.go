package semantic_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
)

func TestValidateVectorRejectsMalformedVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		vector     []float32
		dimensions int
		code       semantic.Reason
	}{
		{name: "zero dimensions", vector: []float32{1}, dimensions: 0, code: semantic.ReasonDimensionMismatch},
		{
			name: "dimensions over limit", vector: []float32{1},
			dimensions: semantic.MaxEmbeddingDimensions + 1, code: semantic.ReasonDimensionMismatch,
		},
		{name: "wrong dimension", vector: []float32{1}, dimensions: 2, code: semantic.ReasonDimensionMismatch},
		{name: "empty", vector: nil, dimensions: 1, code: semantic.ReasonDimensionMismatch},
		{name: "NaN", vector: []float32{float32(math.NaN()), 0}, dimensions: 2, code: semantic.ReasonInvalidResponse},
		{
			name: "positive infinity", vector: []float32{float32(math.Inf(1)), 0},
			dimensions: 2, code: semantic.ReasonInvalidResponse,
		},
		{
			name: "negative infinity", vector: []float32{float32(math.Inf(-1)), 0},
			dimensions: 2, code: semantic.ReasonInvalidResponse,
		},
		{name: "zero norm", vector: []float32{0, 0}, dimensions: 2, code: semantic.ReasonInvalidResponse},
		{name: "not unit", vector: []float32{0.5, 0}, dimensions: 2, code: semantic.ReasonInvalidResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertReason(t, semantic.ValidateVector(test.vector, test.dimensions), test.code)
		})
	}

	if err := semantic.ValidateVector([]float32{0.6, 0.8}, 2); err != nil {
		t.Fatalf("ValidateVector(unit) error = %v", err)
	}
}

func TestCosineUsesValidatedVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		left, right []float32
		want        float64
	}{
		{name: "same", left: []float32{1, 0}, right: []float32{1, 0}, want: 1},
		{name: "orthogonal", left: []float32{1, 0}, right: []float32{0, 1}, want: 0},
		{name: "opposite", left: []float32{1, 0}, right: []float32{-1, 0}, want: -1},
		{name: "mixed", left: []float32{0.6, 0.8}, right: []float32{0.8, 0.6}, want: 0.960000052452088},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := semantic.Cosine(test.left, test.right)
			if err != nil {
				t.Fatalf("Cosine() error = %v", err)
			}

			if got != test.want {
				t.Fatalf("Cosine() = %.17g, want %.17g", got, test.want)
			}
		})
	}

	_, err := semantic.Cosine([]float32{1}, []float32{1, 0})
	assertReason(t, err, semantic.ReasonDimensionMismatch)
}

func TestRankUsesMaximumCosinePerRevisionAndStableTies(t *testing.T) {
	t.Parallel()

	queries := [][]float32{{1, 0}, {0, 1}}
	chunks := []semantic.Chunk{
		{RevisionID: "revision-b", Ordinal: 0, Vector: []float32{-1, 0}},
		{RevisionID: "revision-a", Ordinal: 1, Vector: []float32{0.6, 0.8}},
		{RevisionID: "revision-c", Ordinal: 4, Vector: []float32{0, 1}},
		{RevisionID: "revision-a", Ordinal: 0, Vector: []float32{0.8, 0.6}},
		{RevisionID: "revision-d", Ordinal: 2, Vector: []float32{1, 0}},
	}
	want := []semantic.DenseMatch{
		{RevisionID: "revision-c", ChunkOrdinal: 4, Score: 1},
		{RevisionID: "revision-d", ChunkOrdinal: 2, Score: 1},
		{RevisionID: "revision-a", ChunkOrdinal: 0, Score: 0.800000011920929},
		{RevisionID: "revision-b", ChunkOrdinal: 0, Score: 0},
	}

	for rotation := range len(chunks) {
		ordered := append(append([]semantic.Chunk(nil), chunks[rotation:]...), chunks[:rotation]...)
		for range 20 {
			got, err := semantic.Rank(context.Background(), queries, ordered, semantic.SemanticCandidateLimit)
			if err != nil {
				t.Fatalf("Rank() error = %v", err)
			}

			if !slices.Equal(got, want) {
				t.Fatalf("Rank() = %#v, want %#v", got, want)
			}
		}
	}
}

func TestRankHonorsLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	queries := [][]float32{{1}}
	chunks := make([]semantic.Chunk, semantic.SemanticCandidateLimit+1)

	for index := range chunks {
		chunks[index] = semantic.Chunk{RevisionID: revisionID(index), Ordinal: 0, Vector: []float32{1}}
	}

	got, err := semantic.Rank(context.Background(), queries, chunks, semantic.SemanticCandidateLimit)
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}

	first, last := got[0].RevisionID, got[len(got)-1].RevisionID
	if len(got) != semantic.SemanticCandidateLimit || first != "revision-000" || last != "revision-099" {
		t.Fatalf("Rank() capped result = %#v", got)
	}

	_, err = semantic.Rank(context.Background(), make([][]float32, semantic.QueryWindowLimit+1), chunks, 1)
	assertReason(t, err, semantic.ReasonInputLimit)

	_, err = semantic.Rank(context.Background(), queries, make([]semantic.Chunk, semantic.ProjectionChunkLimit+1), 1)
	assertReason(t, err, semantic.ReasonProjectionCapacity)

	_, err = semantic.Rank(context.Background(), queries, chunks, semantic.SemanticCandidateLimit+1)
	assertReason(t, err, semantic.ReasonInputLimit)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = semantic.Rank(ctx, queries, chunks, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Rank() error = %v, want context.Canceled", err)
	}
}

func revisionID(index int) string {
	const digits = "0123456789"

	return "revision-" + string([]byte{digits[index/100%10], digits[index/10%10], digits[index%10]})
}
