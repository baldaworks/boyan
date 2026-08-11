package standaloneruntime

import (
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
	return &standaloneService{
		cfg:         cfg,
		logger:      logger,
		state:       inputs.layout,
		store:       storage.store,
		maintenance: storage.maintenance,
		reader:      storage.reader,
		applier:     storage.applier,
		projection:  storage.projection,
		sequenced:   services.sequenced,
		invoker:     services.invoker,
		recall:      services.recall,
		trace:       services.trace,
		forget:      services.forget,
		server:      server,
		done:        make(chan error, 1),
	}
}
