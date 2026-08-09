package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	"github.com/spf13/pflag"
)

func testDerivationRuntimeConfig() runtimeconfig.RuntimeConfig {
	return runtimeconfig.RuntimeConfig{
		Providers: map[string]agentconfig.Config{
			"memory": {
				Type:     agentconfig.AgentTypeCodexACP,
				CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
			},
		},
	}
}

func testStandaloneConfig(t *testing.T) applicationConfig {
	t.Helper()
	cfg := defaultApplicationConfig()
	cfg.StateDir = filepath.Join(t.TempDir(), "boyan-state")
	cfg.HTTP.Listen = "127.0.0.1:0"
	cfg.Derivation.Provider = "memory"
	cfg.Derivation.WorkingDir = t.TempDir()
	cfg.Derivation.Runtime = testDerivationRuntimeConfig()
	return cfg
}

func TestLoadApplicationConfigHonorsConfigEnvAndFlags(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "boyan.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(`
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
`)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	t.Setenv("BOYAN_LOG_LEVEL", "debug")
	flags := pflag.NewFlagSet("boyan", pflag.ContinueOnError)
	bindServeFlags(flags)
	if err := flags.Set(configFlagStateDir, "/from-flag/state"); err != nil {
		t.Fatalf("flags.Set(state-dir) error = %v", err)
	}
	if err := flags.Set(configFlagListen, "127.0.0.1:9999"); err != nil {
		t.Fatalf("flags.Set(listen) error = %v", err)
	}
	cfg, err := loadApplicationConfig(configPath, flags)
	if err != nil {
		t.Fatalf("loadApplicationConfig() error = %v", err)
	}
	if cfg.StateDir != "/from-flag/state" {
		t.Fatalf("cfg.StateDir = %q, want /from-flag/state", cfg.StateDir)
	}
	if cfg.HTTP.Listen != "127.0.0.1:9999" {
		t.Fatalf("cfg.HTTP.Listen = %q, want 127.0.0.1:9999", cfg.HTTP.Listen)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("cfg.Log.Level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Derivation.Provider != "memory" || cfg.Derivation.WorkingDir != "/from-config/work" {
		t.Fatalf("cfg.Derivation = %#v, want provider and working_dir from config", cfg.Derivation)
	}
	if got := cfg.Maintenance.ValueLogGCInterval; got != 2*time.Hour {
		t.Fatalf("cfg.Maintenance.ValueLogGCInterval = %v, want 2h", got)
	}
}

func TestRunContextHelpListsStandaloneCommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runContext(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("runContext(--help) error = %v", err)
	}
	output := stdout.String()
	for _, needle := range []string{"serve", "migrate", "standalone local service"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("help output missing %q:\n%s", needle, output)
		}
	}
}

func TestServeCommandHelpExposesLoopbackFlags(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runContext(context.Background(), []string{"serve", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("runContext(serve --help) error = %v", err)
	}
	output := stdout.String()
	for _, needle := range []string{"/livez", "/mcp", "--listen", "--state-dir"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("serve help output missing %q:\n%s", needle, output)
		}
	}
}

func TestStandaloneServiceServesHealthAndStops(t *testing.T) {
	t.Parallel()

	cfg := testStandaloneConfig(t)
	logger, err := newLogger(cfg.Log)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}
	service, err := newStandaloneService(cfg, logger)
	if err != nil {
		t.Fatalf("newStandaloneService() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatalf("service.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := service.Stop(stopCtx); err != nil {
			t.Fatalf("service.Stop() error = %v", err)
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/livez", "/readyz"} {
		response, err := client.Get("http://" + service.address() + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, response.StatusCode)
		}
	}
}
