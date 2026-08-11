package portableapp

import projection "github.com/baldaworks/boyan/internal/projection"

func buildProjectionSyncer(config RuntimeConfig) (ProjectionSyncer, *projection.ProjectionRuntime, error) {
	project := config.Project
	if project != nil || config.CanonicalStore == nil || config.ProjectionCheckpoints == nil || config.ProjectionApplier == nil || config.ProjectionID == "" {
		return project, nil, nil
	}
	runtime, err := projection.NewProjectionRuntime(
		config.CanonicalStore,
		config.ProjectionCheckpoints,
		config.ProjectionApplier,
		config.ProjectionID,
		config.ProjectionBatchSize,
	)
	if err != nil {
		return nil, nil, err
	}
	return runtime, runtime, nil
}
