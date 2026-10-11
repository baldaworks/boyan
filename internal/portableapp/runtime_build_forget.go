package portableapp

import memoryops "github.com/baldaworks/boyan/internal/memoryops"

func buildRuntimeForget(config RuntimeConfig, project ProjectionSyncer) (Forgetter, error) {
	if config.Forget == nil {
		return nil, nil
	}

	return memoryops.NewForgetServiceWithProjection(config.Forget, project)
}
