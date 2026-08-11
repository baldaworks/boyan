package portableapp

// NewRuntime composes the supported application capabilities. Missing
// optional capabilities leave their methods disabled; this is useful for a
// host that grants only ingestion or only read access. Turn ingestion is
// constructed when CanonicalStore and Extractor (or structuredderive.StructuredInvoker) are
// present. Recall and projection adapters are constructed from their narrow
// ports without importing a storage implementation.
func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	derivation, err := resolveRuntimeDerivation(config)
	if err != nil {
		return nil, err
	}
	project, projectionRuntime, err := buildProjectionSyncer(config)
	if err != nil {
		return nil, err
	}
	extractor, invokerLifecycle, err := buildRuntimeExtractor(config)
	if err != nil {
		return nil, err
	}
	turn, err := buildRuntimeTurnProcessor(config, extractor, derivation, project)
	if err != nil {
		return nil, err
	}
	recall, err := buildRuntimeRecall(config)
	if err != nil {
		return nil, err
	}
	trace, err := buildRuntimeTrace(config)
	if err != nil {
		return nil, err
	}
	boundary, err := buildRuntimeBoundary(config, project)
	if err != nil {
		return nil, err
	}
	forget, err := buildRuntimeForget(config, project)
	if err != nil {
		return nil, err
	}
	lifecycle, err := buildRuntimeLifecycle(config, projectionRuntime, invokerLifecycle, project)
	if err != nil {
		return nil, err
	}
	return &Runtime{turn: turn, boundary: boundary, recall: recall, trace: trace, forget: forget, lifecycle: lifecycle}, nil
}
