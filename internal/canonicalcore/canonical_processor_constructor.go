package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

// NewCanonicalTurnProcessor validates the mandatory cutover dependencies.
func NewCanonicalTurnProcessor(store CanonicalStore, extractor CanonicalSemanticExtractor, policy PolicyRegistry) (*CanonicalTurnProcessor, error) {
	if store == nil || extractor == nil {
		return nil, PermanentError(CodeStoreFailure, "canonical processor dependencies are required", nil)
	}
	if !isCanonicalID(policy.Version) {
		return nil, invalidDerived("canonical reconciliation policy version is required")
	}
	return &CanonicalTurnProcessor{store: store, extractor: extractor, policy: policy}, nil
}
