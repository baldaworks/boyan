package main

import (
	"context"
	"strings"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
	boyan "github.com/baldaworks/boyan/pkg/boyan"
	"github.com/spf13/cobra"
)

type serveRunner interface {
	Run(context.Context) error
}

var newServeHost = func(ctx context.Context, config boyan.Config) (serveRunner, error) {
	return boyan.New(ctx, config)
}

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
			cfg, err := standalonecfg.LoadConfig(derefString(configPath))
			if err != nil {
				return err
			}
			cfg, err = applyConfigFlags(cfg, cmd.Flags())
			if err != nil {
				return err
			}
			host, err := newServeHost(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			return host.Run(cmd.Context())
		},
	}
	bindServeFlags(command.Flags())
	return command
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
