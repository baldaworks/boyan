package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
)

type serveRunnerFunc func(context.Context) error

func (f serveRunnerFunc) Run(ctx context.Context) error {
	return f(ctx)
}

func TestServeCommandUsesPublicHostPath(t *testing.T) {
	const configYAML = `
state_dir: /from-config/state
http:
  listen: 127.0.0.1:8686
derivation:
  provider: memory
  working_dir: /from-config/work
`

	configPath := filepath.Join(t.TempDir(), "boyan.yaml")
	requireNoError(
		t,
		os.WriteFile(configPath, []byte(strings.TrimSpace(configYAML)+"\n"), 0o600),
		"WriteFile(config) error = %v",
	)

	previousNewServeHost := newServeHost

	t.Cleanup(func() { newServeHost = previousNewServeHost })

	ctx := context.WithValue(context.Background(), struct{}{}, "serve")

	var (
		received         boyan.Config
		constructorCalls int
		runCalls         int
	)

	newServeHost = func(gotCtx context.Context, cfg boyan.Config) (serveRunner, error) {
		constructorCalls++

		if gotCtx != ctx {
			t.Fatalf("constructor context mismatch")
		}

		received = cfg

		return serveRunnerFunc(func(runCtx context.Context) error {
			runCalls++

			if runCtx != ctx {
				t.Fatalf("run context mismatch")
			}

			return nil
		}), nil
	}

	command := newServeCommand(&configPath)
	command.SetContext(ctx)

	requireNoError(t, command.Flags().Set(configFlagStateDir, "/from-flag/state"), "Flags().Set(state-dir) error = %v")

	requireNoError(t, command.Flags().Set(configFlagListen, "127.0.0.1:9999"), "Flags().Set(listen) error = %v")

	requireNoError(t, command.Execute(), "command.Execute() error = %v")

	assertServeHostConfig(t, received, constructorCalls, runCalls)
}

func TestServeCommandRejectsPositionalArguments(t *testing.T) {
	previousNewServeHost := newServeHost
	t.Cleanup(func() { newServeHost = previousNewServeHost })

	constructorCalls := 0
	newServeHost = func(context.Context, boyan.Config) (serveRunner, error) {
		constructorCalls++
		return serveRunnerFunc(func(context.Context) error { return nil }), nil
	}

	command := newServeCommand(new(string))
	command.SetArgs([]string{"unexpected"})
	if err := command.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want positional-argument rejection")
	}
	if constructorCalls != 0 {
		t.Fatalf("host constructor calls = %d, want 0", constructorCalls)
	}
}

func assertServeHostConfig(t *testing.T, received boyan.Config, constructorCalls, runCalls int) {
	t.Helper()

	if constructorCalls != 1 || runCalls != 1 {
		t.Fatalf("host path calls = constructor:%d run:%d, want 1/1", constructorCalls, runCalls)
	}

	if received.StateDir != "/from-flag/state" {
		t.Fatalf("received.StateDir = %q, want /from-flag/state", received.StateDir)
	}

	if received.HTTP.Listen != "127.0.0.1:9999" {
		t.Fatalf("received.HTTP.Listen = %q, want 127.0.0.1:9999", received.HTTP.Listen)
	}

	if received.Derivation.Provider != "memory" ||
		received.Derivation.WorkingDir != "/from-config/work" {
		t.Fatalf(
			"received.Derivation = %#v, want provider and working_dir from config",
			received.Derivation,
		)
	}
}
