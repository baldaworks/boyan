package standaloneruntime

import (
	"context"

	"github.com/baldaworks/boyan/internal/boundarycore"
	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/derivation"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildStandaloneIngestServices(
	cfg Config,
	workingDir string,
	storage *standaloneStorage,
	cleanups *standaloneCleanupStack,
) (*derivation.Invoker, *orderedingest.SequencedIngestRuntime, error) {
	invoker, err := derivation.NewInvoker(standaloneInvokerConfig(cfg, workingDir))
	if err != nil {
		return nil, nil, err
	}

	cleanups.Add(func() { _ = invoker.Close(context.Background()) })

	deriver, err := structuredderive.NewDeriver(invoker)
	if err != nil {
		return nil, nil, err
	}

	turnProcessor, err := canonicalcore.NewCanonicalTurnProcessor(
		storage.store,
		deriver,
		memory.PolicyRegistry{
			Version: "policy-v1",
		},
	)
	if err != nil {
		return nil, nil, err
	}

	boundaryStore, err := canonicalops.NewCanonicalBoundaryStore(storage.store, storage.reader)
	if err != nil {
		return nil, nil, err
	}

	boundaryProcessor, err := boundarycore.NewBoundaryProcessor(
		boundaryStore,
		deriver,
		deriver,
		memory.LegacyDerivationRef(),
	)
	if err != nil {
		return nil, nil, err
	}

	sequenced, err := orderedingest.NewSequencedIngestRuntime(
		storage.store,
		storage.store,
		turnProcessor,
		memory.LegacyDerivationRef(),
		storage.projection,
		boundaryProcessor,
		storage.projection,
		orderedingest.SequencedIngestConfig{},
	)
	if err != nil {
		return nil, nil, err
	}

	return invoker, sequenced, nil
}

func standaloneInvokerConfig(cfg Config, workingDir string) derivation.Config {
	return derivation.Config{
		Runtime:        cfg.Derivation.Runtime,
		ProviderID:     cfg.Derivation.Provider,
		WorkingDir:     workingDir,
		MaxOutputBytes: cfg.Derivation.MaxOutputBytes,
		Timeout:        cfg.Derivation.Timeout,
	}
}
