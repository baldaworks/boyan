package standaloneruntime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
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

func testStandaloneConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		StateDir: filepath.Join(t.TempDir(), "boyan-state"),
		Log: LogConfig{
			Level: "info",
		},
		HTTP: HTTPConfig{
			Listen:          "127.0.0.1:0",
			RequestTimeout:  15 * time.Second,
			ShutdownTimeout: 10 * time.Second,
			MaxBodyBytes:    1 << 20,
		},
		Derivation: DerivationConfig{
			Provider:       "memory",
			WorkingDir:     t.TempDir(),
			Timeout:        30 * time.Second,
			MaxOutputBytes: 262144,
			Runtime:        testDerivationRuntimeConfig(),
		},
		Maintenance: MaintenanceConfig{
			ValueLogGCInterval: 6 * time.Hour,
			DiscardRatio:       0.5,
		},
	}
}
