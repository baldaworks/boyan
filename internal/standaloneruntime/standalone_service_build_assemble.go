package standaloneruntime

import (
	"context"
	stdhttp "net/http"

	"github.com/rs/zerolog"
)

func assembleStandaloneService(
	cfg Config,
	logger zerolog.Logger,
	inputs standaloneBuildInputs,
	storage *standaloneStorage,
	services *standaloneAppServices,
	server *stdhttp.Server,
) *standaloneService {
	shutdownContext, cancelOperations := context.WithCancel(context.Background())

	return &standaloneService{
		stateLock:        storage.stateLock,
		cfg:              cfg,
		logger:           logger,
		state:            inputs.layout,
		store:            storage.store,
		reader:           storage.reader,
		index:            storage.index,
		applier:          storage.applier,
		projection:       storage.projection,
		semantic:         storage.semantic,
		sequenced:        services.sequenced,
		invoker:          services.invoker,
		recall:           services.recall,
		trace:            services.trace,
		forget:           services.forget,
		server:           server,
		done:             make(chan struct{}),
		shutdownContext:  shutdownContext,
		cancelOperations: cancelOperations,
	}
}
