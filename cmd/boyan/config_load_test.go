package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
	"github.com/spf13/pflag"
)

const configPrecedenceTestYAML = `
state_dir: /from-config/state
log:
  level: info
http:
  listen: 127.0.0.1:8686
derivation:
  provider: memory
  working_dir: /from-config/work
  runtime:
    providers:
      memory:
        type: codex_acp
        codex_acp:
          model: memory-fast
maintenance:
  value_log_gc_interval: 2h
`

func TestLoadApplicationConfigHonorsConfigEnvAndFlags(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "boyan.yaml")
	requireNoError(
		t,
		os.WriteFile(configPath, []byte(strings.TrimSpace(configPrecedenceTestYAML)+"\n"), 0o600),
		"WriteFile(config) error = %v",
	)

	t.Setenv("BOYAN_LOG_LEVEL", "debug")

	flags := pflag.NewFlagSet("boyan", pflag.ContinueOnError)
	bindServeFlags(flags)

	requireNoError(t, flags.Set(configFlagStateDir, "/from-flag/state"), "flags.Set(state-dir) error = %v")

	requireNoError(t, flags.Set(configFlagListen, "127.0.0.1:9999"), "flags.Set(listen) error = %v")

	cfg, err := standalonecfg.LoadConfig(configPath)
	requireNoError(t, err, "standalonecfg.LoadConfig() error = %v")

	cfg, err = applyConfigFlags(cfg, flags)
	requireNoError(t, err, "applyConfigFlags() error = %v")

	if cfg.StateDir != "/from-flag/state" {
		t.Fatalf("cfg.StateDir = %q, want /from-flag/state", cfg.StateDir)
	}

	if cfg.HTTP.Listen != "127.0.0.1:9999" {
		t.Fatalf("cfg.HTTP.Listen = %q, want 127.0.0.1:9999", cfg.HTTP.Listen)
	}

	if cfg.Log.Level != "debug" {
		t.Fatalf("cfg.Log.Level = %q, want debug", cfg.Log.Level)
	}

	if cfg.Derivation.Provider != "memory" ||
		cfg.Derivation.WorkingDir != "/from-config/work" {
		t.Fatalf("cfg.Derivation = %#v, want provider and working_dir from config", cfg.Derivation)
	}

	if got := cfg.Maintenance.ValueLogGCInterval; got != 2*time.Hour {
		t.Fatalf("cfg.Maintenance.ValueLogGCInterval = %v, want 2h", got)
	}
}
