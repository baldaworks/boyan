package boundarycore

import (
	. "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

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
	clock               func() time.Time
}

// NewBoundaryProcessorWithClock constructs a boundary service with an injectable
// processing clock. Each attempt captures the clock exactly once.
func NewBoundaryProcessorWithClock(
	store BoundaryStore,
	scenarios ScenarioSynthesizer,
	profiles ProfileSynthesizer,
	derivation DerivationRef,
	clock func() time.Time,
) (*BoundaryProcessor, error) {
	if store == nil || scenarios == nil || profiles == nil || clock == nil {
		return nil, PermanentError(CodeStoreFailure, "boundary processor dependencies are required", nil)
	}

	if err := derivation.Validate(); err != nil {
		return nil, err
	}

	return &BoundaryProcessor{
		store:               store,
		clock:               clock,
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

// NewBoundaryProcessor constructs a boundary service using the current clock.
func NewBoundaryProcessor(store BoundaryStore, scenarios ScenarioSynthesizer,
	profiles ProfileSynthesizer, derivation DerivationRef) (*BoundaryProcessor, error) {
	return NewBoundaryProcessorWithClock(store, scenarios, profiles, derivation, time.Now)
}
