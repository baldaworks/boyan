package portableapp

import memoryread "github.com/baldaworks/boyan/internal/memoryread"

func buildRuntimeRecall(config RuntimeConfig) (RecallReader, error) {
	if config.Recall != nil || config.RecallCanonical == nil {
		return config.Recall, nil
	}
	return memoryread.NewRecallService(config.RecallCanonical, config.RecallProjection)
}
