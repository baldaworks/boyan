package derivation //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"testing"

	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

func TestConfigValidateAcceptsBuildableProviderWithoutReasoningOverride(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Runtime: runtimeconfig.RuntimeConfig{
			Providers: map[string]agentconfig.Config{
				"memory": {
					Type:     agentconfig.AgentTypeCodexACP,
					CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
				},
			},
		},
		ProviderID: "memory",
		WorkingDir: t.TempDir(),
	}
	requireNoError(t, cfg.Validate(), "Config.Validate() error = %v")
}

func TestConfigValidateRejectsMissingProvider(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Runtime: runtimeconfig.RuntimeConfig{
			Providers: map[string]agentconfig.Config{
				"memory": {
					Type:     agentconfig.AgentTypeCodexACP,
					CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
				},
			},
		},
		ProviderID: "missing",
		WorkingDir: t.TempDir(),
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Config.Validate() error = nil, want provider validation failure")
	}
}
