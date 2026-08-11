package portableapp

import memoryread "github.com/baldaworks/boyan/internal/memoryread"

func buildRuntimeTrace(config RuntimeConfig) (TraceReader, error) {
	if config.Trace != nil || config.CanonicalDerived == nil {
		return config.Trace, nil
	}
	return memoryread.NewTraceService(config.CanonicalDerived)
}
