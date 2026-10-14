package memory

import (
	"math"
	"strings"
)

// Validate verifies a bounded untrusted recall response for one request.
func (r RecallResponse) Validate(request RecallRequest) error {
	if r.SchemaVersion != RecallSchemaVersionV1 || r.Trust != ReferenceTrustUntrusted ||
		r.Scope != request.Scope {
		return PermanentError(CodeScopeViolation, "recall response scope or trust marker is invalid", nil)
	}

	if r.ScopeChangeSeq < request.MinScopeChangeSeq {
		return RetryableError(CodeConflict, "recall response is below the requested consistency sequence", nil)
	}

	if len(r.Results) > request.Limit {
		return limitExceeded("recall response exceeds the requested limit")
	}

	seen := make(map[string]struct{}, len(r.Results))
	for _, result := range r.Results {
		if err := validateRecallReference(result, request, r.ScopeChangeSeq); err != nil {
			return err
		}

		if _, exists := seen[result.RevisionID]; exists {
			return invalidDerived("recall response contains duplicate revisions")
		}

		seen[result.RevisionID] = struct{}{}
	}

	return nil
}

func validateRecallReference(result RecallReference, request RecallRequest, responseSequence uint64) error {
	if err := validateRecallReferenceIdentity(result, request.Scope); err != nil {
		return err
	}

	if err := validateRecallReferenceKind(result); err != nil {
		return err
	}

	if err := validateRecallReferenceScore(result); err != nil {
		return err
	}

	if err := validateRecallReferenceEvidence(result.Evidence); err != nil {
		return err
	}

	if err := validateRecallReferenceSequence(
		result.ScopeChangeSeq,
		responseSequence,
		request.MinScopeChangeSeq,
	); err != nil {
		return err
	}

	return validateRecallReferenceFilters(result, request)
}

func validateRecallReferenceIdentity(result RecallReference, scope Scope) error {
	if !validRecallReferenceEnvelope(result, scope) ||
		!validRecallReferenceIDs(result) ||
		!validRecallReferenceContent(result) {
		return invalidDerived("recall response contains an invalid reference")
	}

	return nil
}

func validRecallReferenceEnvelope(result RecallReference, scope Scope) bool {
	return result.SchemaVersion == RecallSchemaVersionV1 &&
		result.Trust == ReferenceTrustUntrusted &&
		result.Scope == scope &&
		result.State == RevisionStateActive
}

func validRecallReferenceIDs(result RecallReference) bool {
	return isCanonicalID(result.ItemID) &&
		isCanonicalID(result.RevisionID) &&
		result.Revision != 0
}

func validRecallReferenceContent(result RecallReference) bool {
	return strings.TrimSpace(result.Text) == result.Text &&
		result.Text != "" &&
		len(result.Text) <= MaxDerivedTextBytes &&
		!result.CreatedAt.IsZero()
}

func validateRecallReferenceSequence(sequence, responseSequence, minimum uint64) error {
	if sequence == 0 || sequence > responseSequence {
		return invalidDerived("recall response reference sequence is invalid")
	}

	if sequence < minimum {
		return RetryableError(CodeConflict, "recall response reference is below the requested consistency sequence", nil)
	}

	return nil
}

func validateRecallReferenceKind(result RecallReference) error {
	switch result.Kind {
	case MemoryKindState:
		if !isCanonicalID(string(result.MemoryKey)) {
			return invalidDerived("recall state reference memory key is invalid")
		}
	case MemoryKindEvent:
		if result.MemoryKey != "" {
			return invalidDerived("recall event reference has a memory key")
		}
	default:
		return invalidDerived("recall response reference kind is invalid")
	}

	if result.Category != nil {
		return result.Category.Validate()
	}

	return nil
}

func validateRecallReferenceScore(result RecallReference) error {
	values := [...]float64{
		result.Score,
		result.Explain.Lexical,
		result.Explain.Freshness,
		result.Explain.ExactScope,
		result.Explain.Total,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return invalidDerived("recall response reference score is invalid")
		}
	}

	if result.Score != result.Explain.Total ||
		result.Explain.Total != result.Explain.Lexical+result.Explain.Freshness+result.Explain.ExactScope {
		return invalidDerived("recall response reference score explanation is inconsistent")
	}

	return nil
}

func validateRecallReferenceEvidence(evidence []EvidenceRef) error {
	if len(evidence) > MaxRecallEvidence {
		return limitExceeded("recall response reference evidence exceeds the compact bound")
	}

	seen := make(map[EvidenceRef]struct{}, len(evidence))
	for _, reference := range evidence {
		if err := reference.Validate(); err != nil {
			return err
		}

		if _, exists := seen[reference]; exists {
			return invalidDerived("recall response reference contains duplicate evidence")
		}

		seen[reference] = struct{}{}
	}

	return nil
}

func validateRecallReferenceFilters(result RecallReference, request RecallRequest) error {
	if request.Kind != nil && result.Kind != *request.Kind {
		return invalidDerived("recall response reference does not match the kind filter")
	}

	if request.Category != nil && (result.Category == nil || *result.Category != *request.Category) {
		return invalidDerived("recall response reference does not match the category filter")
	}

	if request.MemoryKey != "" && result.MemoryKey != request.MemoryKey {
		return invalidDerived("recall response reference does not match the memory key filter")
	}

	if request.SourceID != "" && !recallEvidenceContainsSource(result.Evidence, request.SourceID) {
		return invalidDerived("recall response reference does not match the source filter")
	}

	return nil
}

func recallEvidenceContainsSource(evidence []EvidenceRef, sourceID string) bool {
	for _, reference := range evidence {
		if reference.SourceID == sourceID {
			return true
		}
	}

	return false
}
