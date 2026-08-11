package portableapp

import ingestapp "github.com/baldaworks/boyan/internal/ingestapp"

func buildRuntimeBoundary(config RuntimeConfig, project ProjectionSyncer) (*BoundaryService, error) {
	if config.Boundary == nil {
		return nil, nil
	}
	return ingestapp.NewBoundaryService(config.Boundary, project)
}
