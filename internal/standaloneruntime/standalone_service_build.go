package standaloneruntime

import (
	"github.com/rs/zerolog"
)

func newStandaloneService(cfg Config, logger zerolog.Logger) (_ *standaloneService, err error) {
	inputs, err := prepareStandaloneBuild(cfg)
	if err != nil {
		return nil, err
	}

	cleanups := standaloneCleanupStack{}

	defer func() {
		if err != nil {
			cleanups.Close()
		}
	}()

	storage, err := openStandaloneStorage(inputs.layout, cfg, &cleanups)
	if err != nil {
		return nil, err
	}

	services, err := buildStandaloneAppServices(cfg, inputs.workingDir, storage, &cleanups)
	if err != nil {
		return nil, err
	}

	readiness := &standaloneReadinessReporter{maintenance: storage.maintenance}

	server, err := newHTTPServer(
		standaloneHTTPConfig{
			Listen:         cfg.HTTP.Listen,
			RequestTimeout: cfg.HTTP.RequestTimeout,
			MaxBodyBytes:   cfg.HTTP.MaxBodyBytes,
		},
		logger,
		readiness,
		services.sequenced,
		services.sequenced,
		services.recall,
		services.trace,
		services.forget,
	)
	if err != nil {
		return nil, err
	}

	service := assembleStandaloneService(cfg, logger, inputs, storage, services, server)
	server.Handler = service.operationHandler(server.Handler)
	readiness.attach(service)

	return service, nil
}
