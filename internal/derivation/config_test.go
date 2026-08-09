package derivation

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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Config.Validate() error = %v", err)
	}
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
