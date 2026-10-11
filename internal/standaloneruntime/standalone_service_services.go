package standaloneruntime

import (
	"github.com/baldaworks/boyan/internal/derivation"
	memoryops "github.com/baldaworks/boyan/internal/memoryops"
	memoryread "github.com/baldaworks/boyan/internal/memoryread"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
)

type standaloneAppServices struct {
	invoker   *derivation.Invoker
	sequenced *orderedingest.SequencedIngestRuntime
	recall    *memoryread.RecallService
	trace     *memoryread.TraceService
	forget    *memoryops.ForgetService
}

func buildStandaloneAppServices(
	cfg Config,
	workingDir string,
	storage *standaloneStorage,
	cleanups *standaloneCleanupStack,
) (*standaloneAppServices, error) {
	invoker, sequenced, err := buildStandaloneIngestServices(cfg, workingDir, storage, cleanups)
	if err != nil {
		return nil, err
	}

	recall, trace, forget, err := buildStandaloneReadServices(storage)
	if err != nil {
		return nil, err
	}

	return &standaloneAppServices{
		invoker:   invoker,
		sequenced: sequenced,
		recall:    recall,
		trace:     trace,
		forget:    forget,
	}, nil
}
