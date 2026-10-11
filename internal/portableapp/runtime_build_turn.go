package portableapp

import (
	ingestapp "github.com/baldaworks/boyan/internal/ingestapp"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildRuntimeTurnProcessor(
	config RuntimeConfig,
	extractor memory.CanonicalSemanticExtractor,
	derivation memory.DerivationRef,
	project ProjectionSyncer,
) (*TurnProcessor, error) {
	if config.CanonicalStore == nil || extractor == nil {
		return nil, nil
	}

	return ingestapp.NewTurnProcessor(config.CanonicalStore, extractor, config.Policy, derivation, project)
}
