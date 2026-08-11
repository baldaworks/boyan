package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

type boundaryConfig struct {
	Derivation            DerivationRef
	MaxCandidateCount     int
	MaxSourcesPerRevision int
	MaxDerivedTextBytes   int
	MaxSnapshotItems      int
}

// BoundaryProcessor owns scenario/profile synthesis after a validated
// boundary reaches typed ingest. It uses only the narrow BoundaryStore port
// and keeps stage idempotency and provenance grounding in the memory layer.
type BoundaryProcessor struct {
	store               BoundaryStore
	scenarioSynthesizer ScenarioSynthesizer
	profileSynthesizer  ProfileSynthesizer
	config              boundaryConfig
}

// NewBoundaryProcessor constructs the memory-owned boundary application
// service with package hard limits and the supplied derivation identity.
func NewBoundaryProcessor(store BoundaryStore, scenarios ScenarioSynthesizer, profiles ProfileSynthesizer, derivation DerivationRef) (*BoundaryProcessor, error) {
	if store == nil || scenarios == nil || profiles == nil {
		return nil, PermanentError(CodeStoreFailure, "boundary processor dependencies are required", nil)
	}
	if err := derivation.Validate(); err != nil {
		return nil, err
	}
	return &BoundaryProcessor{
		store:               store,
		scenarioSynthesizer: scenarios,
		profileSynthesizer:  profiles,
		config: boundaryConfig{
			Derivation:            derivation,
			MaxCandidateCount:     MaxCandidateCount,
			MaxSourcesPerRevision: MaxSourcesPerRevision,
			MaxDerivedTextBytes:   MaxDerivedTextBytes,
			MaxSnapshotItems:      MaxSnapshotItems,
		},
	}, nil
}
