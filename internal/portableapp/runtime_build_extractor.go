package portableapp

import (
	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildRuntimeExtractor(
	config RuntimeConfig,
) (
	memory.CanonicalSemanticExtractor,
	*structuredInvokerLifecycle,
	error,
) {
	extractor := config.Extractor

	var lifecycle *structuredInvokerLifecycle
	if config.StructuredInvoker != nil {
		lifecycle = &structuredInvokerLifecycle{invoker: config.StructuredInvoker}
	}

	if extractor != nil || config.StructuredInvoker == nil {
		return extractor, lifecycle, nil
	}

	deriver, err := structuredderive.NewDeriver(config.StructuredInvoker)
	if err != nil {
		return nil, nil, err
	}

	return deriver, lifecycle, nil
}
