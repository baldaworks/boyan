package semantic

import (
	"context"
	"sort"
)

// FusedMatch exposes independent RRF channel contributions and their sum.
type FusedMatch struct {
	RevisionID string
	Lexical    float64
	Semantic   float64
	Score      float64
}

// CandidateLimit returns the bounded per-channel recall depth.
func CandidateLimit(limit int) int {
	return min(SemanticCandidateLimit, max(MinimumCandidateLimit, CandidateLimitFactor*limit))
}

// Fuse applies reciprocal-rank fusion to two independently ranked channels.
// Duplicate entries preserve the positions of later unique entries.
func Fuse(ctx context.Context, lexical, dense []string, limit int) ([]FusedMatch, error) {
	if err := validateFusionInput(ctx, limit); err != nil {
		return nil, err
	}

	matches := make(map[string]FusedMatch, min(len(lexical)+len(dense), FusedCandidateLimit))
	if err := addChannel(ctx, matches, lexical, false); err != nil {
		return nil, err
	}

	if err := addChannel(ctx, matches, dense, true); err != nil {
		return nil, err
	}

	fused := make([]FusedMatch, 0, len(matches))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		fused = append(fused, match)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Slice(fused, func(left, right int) bool {
		if fused[left].Score != fused[right].Score {
			return fused[left].Score > fused[right].Score
		}

		return fused[left].RevisionID < fused[right].RevisionID
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(fused) > limit {
		fused = fused[:limit]
	}

	return fused, nil
}

func validateFusionInput(ctx context.Context, limit int) error {
	if ctx == nil {
		return newError(ReasonInvalidInput)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if limit < 1 || limit > FusedCandidateLimit {
		return newError(ReasonInputLimit)
	}

	return nil
}

func addChannel(ctx context.Context, matches map[string]FusedMatch, ids []string, semantic bool) error {
	seen := make(map[string]struct{}, min(len(ids), SemanticCandidateLimit))
	for rank, revisionID := range ids[:min(len(ids), SemanticCandidateLimit)] {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !validRevisionID(revisionID) {
			return newError(ReasonInvalidInput)
		}

		if _, duplicate := seen[revisionID]; duplicate {
			continue
		}

		seen[revisionID] = struct{}{}
		match := matches[revisionID]
		match.RevisionID = revisionID
		contribution := 1 / float64(FusionRankConstant+rank+1)

		if semantic {
			match.Semantic = contribution
		} else {
			match.Lexical = contribution
		}

		match.Score = match.Lexical + match.Semantic
		matches[revisionID] = match
	}

	return nil
}
