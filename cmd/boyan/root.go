package main

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	configFlagPath     = "config"
	configFlagStateDir = "state-dir"
	configFlagListen   = "listen"
)

func run(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return runContext(ctx, args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	command := newRootCommand(stdout, stderr)
	command.SetArgs(args)
	command.SetContext(ctx)

	return command.Execute()
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	var configPath string

	command := &cobra.Command{
		Use:           "boyan",
		Short:         "Boyan is a standalone local service for grounded temporal memory.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.PersistentFlags().StringVar(&configPath, configFlagPath, "", "path to a Boyan YAML config file")
	command.AddCommand(newServeCommand(&configPath))

	return command
}

func formatUsageError(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
