package standaloneruntime

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
)

func newStandaloneService(ctx context.Context, cfg Config, logger zerolog.Logger) (_ *standaloneService, err error) {
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

	storage, err := openStandaloneStorage(ctx, inputs.layout, cfg, &cleanups)
	if err != nil {
		return nil, err
	}

	oldPath := filepath.Join(inputs.layout.Root, "badger")
	if _, err := os.Stat(oldPath); err == nil {
		logger.Info().Str("old_state_dir", oldPath).
			Msg("Existing Badger state is preserved; SQL state is separate and old identities are not imported")
	}

	services, err := buildStandaloneAppServices(cfg, inputs.workingDir, storage, &cleanups)
	if err != nil {
		return nil, err
	}

	readiness := &standaloneReadinessReporter{}

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
