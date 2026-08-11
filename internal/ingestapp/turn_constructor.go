package ingestapp

import (
	"github.com/baldaworks/boyan/internal/canonicalcore"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// NewTurnProcessor constructs the portable turn application service.
func NewTurnProcessor(store memory.CanonicalStore, extractor memory.CanonicalSemanticExtractor, policy memory.PolicyRegistry, derivation memory.DerivationRef, projection ProjectionSyncer) (*TurnProcessor, error) {
	if store == nil || extractor == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical turn dependencies are required", nil)
	}
	if policy.Version == "" {
		policy.Version = "policy-v1"
	}
	if derivation == (memory.DerivationRef{}) {
		derivation = memory.LegacyDerivationRef()
	}
	if err := derivation.Validate(); err != nil {
		return nil, err
	}
	processor, err := canonicalcore.NewCanonicalTurnProcessor(store, extractor, policy)
	if err != nil {
		return nil, err
	}
	return &TurnProcessor{processor: processor, projection: projection, derivation: derivation}, nil
}
