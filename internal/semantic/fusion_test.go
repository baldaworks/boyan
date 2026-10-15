package semantic_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
)

func TestFuseRetainsChannelOnlyCandidatesAndDeduplicates(t *testing.T) {
	t.Parallel()

	lexical := []string{"revision-a", "revision-b", "revision-a", "revision-lexical"}
	dense := []string{"revision-b", "revision-a", "revision-semantic", "revision-b"}
	lexicalA, semanticA := float64(1.0/61), float64(1.0/62)
	lexicalB, semanticB := float64(1.0/62), float64(1.0/61)
	want := []semantic.FusedMatch{
		{
			RevisionID: "revision-a",
			Lexical:    lexicalA,
			Semantic:   semanticA,
			Score:      lexicalA + semanticA,
		},
		{
			RevisionID: "revision-b",
			Lexical:    lexicalB,
			Semantic:   semanticB,
			Score:      lexicalB + semanticB,
		},
		{RevisionID: "revision-semantic", Semantic: 1.0 / 63, Score: 1.0 / 63},
		{RevisionID: "revision-lexical", Lexical: 1.0 / 64, Score: 1.0 / 64},
	}

	for range 100 {
		got, err := semantic.Fuse(context.Background(), lexical, dense, semantic.FusedCandidateLimit)
		if err != nil {
			t.Fatalf("Fuse() error = %v", err)
		}

		if !slices.Equal(got, want) {
			t.Fatalf("Fuse() = %#v, want %#v", got, want)
		}
	}
}

func TestFuseBreaksEqualScoresByRevisionID(t *testing.T) {
	t.Parallel()

	got, err := semantic.Fuse(
		context.Background(),
		[]string{"revision-b", "revision-a"},
		[]string{"revision-a", "revision-b"},
		2,
	)
	if err != nil {
		t.Fatalf("Fuse() error = %v", err)
	}

	if got[0].RevisionID != "revision-a" || got[1].RevisionID != "revision-b" || got[0].Score != got[1].Score {
		t.Fatalf("Fuse() ties = %#v", got)
	}
}

func TestFuseHonorsChannelAndOutputCaps(t *testing.T) {
	t.Parallel()

	lexical := make([]string, semantic.SemanticCandidateLimit+1)
	dense := make([]string, semantic.SemanticCandidateLimit+1)

	for index := range lexical {
		lexical[index] = "lexical-" + revisionID(index)
		dense[index] = "semantic-" + revisionID(index)
	}

	got, err := semantic.Fuse(context.Background(), lexical, dense, semantic.FusedCandidateLimit)
	if err != nil {
		t.Fatalf("Fuse() error = %v", err)
	}

	if len(got) != semantic.FusedCandidateLimit {
		t.Fatalf("Fuse() results = %d, want %d", len(got), semantic.FusedCandidateLimit)
	}

	for _, match := range got {
		if strings.HasSuffix(match.RevisionID, "100") {
			t.Fatalf("Fuse() included candidate beyond channel cap: %q", match.RevisionID)
		}
	}

	_, err = semantic.Fuse(context.Background(), lexical, dense, semantic.FusedCandidateLimit+1)
	assertReason(t, err, semantic.ReasonInputLimit)
}

func TestFusePropagatesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := semantic.Fuse(ctx, []string{"revision"}, nil, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fuse() error = %v, want context.Canceled", err)
	}
}

func TestFuseRejectsNonCanonicalRevisionIdentity(t *testing.T) {
	t.Parallel()

	_, err := semantic.Fuse(context.Background(), []string{"revision\tsecret"}, nil, 1)
	assertReason(t, err, semantic.ReasonInvalidInput)
}

func TestCandidateLimitBoundsProjectionWork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		requested int
		want      int
	}{
		{requested: 1, want: 20},
		{requested: 5, want: 20},
		{requested: 6, want: 24},
		{requested: 25, want: 100},
		{requested: 100, want: 100},
	}
	for _, test := range tests {
		if got := semantic.CandidateLimit(test.requested); got != test.want {
			t.Errorf("CandidateLimit(%d) = %d, want %d", test.requested, got, test.want)
		}
	}
}

func TestSemanticErrorsExposeOnlySafeReason(t *testing.T) {
	t.Parallel()

	reasons := []semantic.Reason{
		semantic.ReasonInvalidInput,
		semantic.ReasonInputLimit,
		semantic.ReasonUnavailable,
		semantic.ReasonDeadline,
		semantic.ReasonResponseLimit,
		semantic.ReasonInvalidResponse,
		semantic.ReasonModelMismatch,
		semantic.ReasonDimensionMismatch,
		semantic.ReasonProjectionCapacity,
	}
	for _, reason := range reasons {
		err := &semantic.Error{Reason: reason}
		if !reason.Valid() || err.Error() != string(reason) {
			t.Errorf("reason %q validity/error = %t/%q", reason, reason.Valid(), err.Error())
		}

		got, ok := semantic.Classify(err)
		if !ok || got != reason {
			t.Errorf("Classify(%q) = %q, %t", reason, got, ok)
		}
	}

	if semantic.Reason("provider said secret text").Valid() {
		t.Fatal("arbitrary provider prose is a valid semantic reason")
	}

	unsafe := (&semantic.Error{Reason: "provider said secret text"}).Error()
	if strings.Contains(unsafe, "secret") || unsafe != string(semantic.ReasonInvalidResponse) {
		t.Fatalf("unsafe Error() = %q, want safe fallback", unsafe)
	}
}
