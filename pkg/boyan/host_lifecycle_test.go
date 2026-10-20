package boyan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

func TestNewRejectsCanceledContextBeforeOpeningResources(t *testing.T) {
	config := testHostConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	host, err := boyan.New(ctx, config)
	if host != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("New(canceled) = host:%v error:%v, want nil context.Canceled", host, err)
	}

	if _, statErr := os.Stat(config.StateDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state directory stat error = %v, want not-exist", statErr)
	}
}

func testHostConfig(t *testing.T) boyan.Config {
	t.Helper()

	cfg := boyan.DefaultConfig()
	cfg.StateDir = filepath.Join(t.TempDir(), "boyan-state")
	cfg.HTTP.Listen = "127.0.0.1:0"
	cfg.Derivation.Provider = "memory"
	cfg.Derivation.WorkingDir = t.TempDir()
	cfg.Derivation.Runtime = runtimeconfig.RuntimeConfig{
		Providers: map[string]agentconfig.Config{
			"memory": {
				Type:     agentconfig.AgentTypeCodexACP,
				CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
			},
		},
	}

	return cfg
}
