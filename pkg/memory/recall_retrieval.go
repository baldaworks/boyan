package memory

import (
	"context"
	"strings"
)

// RetrievalReport contains only bounded classifications and counts. Transport
// JSON intentionally exposes only effective mode and safe reason.
type RetrievalReport struct {
	Effective           string `json:"effective"`
	Reason              string `json:"reason,omitempty"`
	Requested           string `json:"-"`
	ModelSpacePrefix    string `json:"-"`
	LexicalCandidates   int    `json:"-"`
	SemanticCandidates  int    `json:"-"`
	ScannedChunks       int    `json:"-"`
	OmittedQueryRunes   int    `json:"-"`
	OmittedQueryWindows int    `json:"-"`
}

// Validate rejects arbitrary diagnostic prose and unbounded counters.
func (r RetrievalReport) Validate() error {
	if err := r.validateModes(); err != nil {
		return err
	}

	if r.ModelSpacePrefix != "" && (len(r.ModelSpacePrefix) != 12 ||
		strings.Trim(r.ModelSpacePrefix, "0123456789abcdef") != "") {
		return invalidDerived("recall retrieval model space is invalid")
	}

	for _, counter := range []struct{ value, limit int }{
		{r.LexicalCandidates, 100}, {r.SemanticCandidates, 100}, {r.ScannedChunks, 8192},
		{r.OmittedQueryRunes, MaxSearchQueryBytes}, {r.OmittedQueryWindows, MaxSearchQueryBytes},
	} {
		if counter.value < 0 || counter.value > counter.limit {
			return invalidDerived("recall retrieval count is invalid")
		}
	}

	return nil
}

//nolint:cyclop // Explicit enum and mode/reason cross-field checks reject arbitrary prose.
func (r RetrievalReport) validateModes() error {
	if r.Effective != "lexical" && r.Effective != "hybrid" && r.Effective != "degraded" {
		return invalidDerived("recall retrieval mode is invalid")
	}

	if r.Requested != "" && r.Requested != "hybrid" && r.Requested != "lexical" {
		return invalidDerived("recall requested retrieval mode is invalid")
	}

	if !validRetrievalReason(r.Reason) || r.Effective == "degraded" && r.Reason == "" ||
		r.Effective != "degraded" && r.Reason != "" {
		return invalidDerived("recall retrieval reason is invalid")
	}

	return nil
}

func validRetrievalReason(reason string) bool {
	switch reason {
	case "", "unavailable", "deadline", "response_limit", "invalid_response",
		"model_mismatch", "dimension_mismatch", "projection_capacity":
		return true
	default:
		return false
	}
}

// ReportedRecallProjection supplies request-local metadata. Implementations must
// honor the request's consistency minimum just as ConsistentRecallProjection does.
type ReportedRecallProjection interface {
	SearchRecallWithReport(ctx context.Context, request RecallRequest) ([]RecallProjectionHit, *RetrievalReport, error)
}
