package main

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func newServeCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the standalone Boyan local service.",
		Long: strings.TrimSpace(`
Run the standalone Boyan service on one loopback HTTP listener.

The listener serves:
  - /livez
  - /readyz
  - /v1/ingest/turns
  - /v1/ingest/boundaries
  - /mcp
`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadApplicationConfig(derefString(configPath), cmd.Flags())
			if err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	bindServeFlags(command.Flags())
	return command
}

func runServe(ctx context.Context, cfg applicationConfig, _ io.Writer, _ io.Writer) error {
	logger, err := newLogger(cfg.Log)
	if err != nil {
		return err
	}
	service, err := newStandaloneService(cfg, logger)
	if err != nil {
		return err
	}
	fxApp := fx.New(
		fx.Supply(service),
		fx.Supply(logger),
		fx.Invoke(func(lifecycle fx.Lifecycle, svc *standaloneService) {
			lifecycle.Append(fx.Hook{
				OnStart: svc.Start,
				OnStop:  svc.Stop,
			})
		}),
	)
	startCtx, cancel := context.WithTimeout(ctx, cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := fxApp.Start(startCtx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case err := <-service.done:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			stopCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
			defer cancel()
			_ = fxApp.Stop(stopCtx)
			return err
		}
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	return fxApp.Stop(stopCtx)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
