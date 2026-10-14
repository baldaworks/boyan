package standalonecfg_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	standalonecfg "github.com/baldaworks/boyan/internal/standalonecfg"
)

func loadProviderConfig(t *testing.T, providers string) (standalonecfg.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boyan.yaml")
	yaml := "derivation:\n  runtime:\n    providers:\n" + providers
	requireNoError(t, os.WriteFile(path, []byte(yaml), 0o600), "%v")

	return standalonecfg.LoadConfig(path)
}

func TestConfiguredProviderAPIKeyEnvironment(t *testing.T) {
	for _, leaf := range []string{"", "          api_key: yaml-placeholder\n"} {
		t.Run(strings.TrimSpace(leaf), func(t *testing.T) {
			t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_OPENAI_API_KEY", "dummy-credential")

			providers := "      memory:\n        type: openai\n        openai:\n          model: test-model\n"
			cfg, err := loadProviderConfig(t, providers+leaf)
			requireNoError(t, err, "%v")

			if cfg.Derivation.Runtime.Providers["memory"].OpenAI.APIKey != "dummy-credential" {
				t.Fatal("configured provider did not load credential override")
			}
		})
	}
}

func TestConfiguredProviderNestedEnvironment(t *testing.T) {
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_CLI_CODEX_ACP_CMD", "test-cli,--stdio")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_CLI_CODEX_ACP_EXTRA_ARGS", "--model,test")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_CLI_CODEX_ACP_BRIDGE_VERSION", "test-version")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_CLI_SYSTEM_INSTRUCTIONS", "test instructions")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_CLI_MCP_SERVERS", "one,two")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_FALLBACK_POOL_MEMBERS", "cli,hosted")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_HOSTED_AISTUDIO_API_KEY", "dummy-hosted")
	cfg, err := loadProviderConfig(t, `      cli:
        type: codex_acp
      fallback:
        type: pool
      hosted:
        type: aistudio
`)
	requireNoError(t, err, "%v")

	cli := cfg.Derivation.Runtime.Providers["cli"]
	if cli.CodexACP == nil {
		t.Fatal("ACP block was not loaded")
	}

	if !slices.Equal(cli.CodexACP.Cmd, []string{"test-cli", "--stdio"}) {
		t.Fatal("ACP command override was not loaded")
	}

	if !slices.Equal(cli.CodexACP.ExtraArgs, []string{"--model", "test"}) {
		t.Fatal("ACP arguments override was not loaded")
	}

	if cli.CodexACP.BridgeVersion != "test-version" {
		t.Fatal("ACP overrides were not loaded")
	}

	if cli.SystemInstructions != "test instructions" || strings.Join(cli.MCPServers, ",") != "one,two" {
		t.Fatal("common overrides were not loaded")
	}
}

func TestConfiguredProviderPoolAndHostedEnvironment(t *testing.T) {
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_FALLBACK_POOL_MEMBERS", "cli,hosted")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_HOSTED_AISTUDIO_API_KEY", "dummy-hosted")
	cfg, err := loadProviderConfig(t, `      fallback:
        type: pool
      hosted:
        type: aistudio
`)
	requireNoError(t, err, "%v")

	pool := cfg.Derivation.Runtime.Providers["fallback"].PoolConfig
	if pool == nil || strings.Join(pool.Members, ",") != "cli,hosted" {
		t.Fatal("pool members override was not loaded")
	}

	hosted := cfg.Derivation.Runtime.Providers["hosted"].AIStudio
	if hosted == nil || hosted.APIKey != "dummy-hosted" {
		t.Fatal("hosted override was not loaded")
	}
}

func TestConfiguredProviderEnvironmentDiscoveryLimits(t *testing.T) {
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_UNKNOWN_OPENAI_API_KEY", "dummy-unknown")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_UNKNOWN_API_KEY", "dummy-unknown")
	cfg, err := loadProviderConfig(t, `      memory:
        type: openai
        openai:
          model: yaml-model
`)
	requireNoError(t, err, "%v")

	if len(cfg.Derivation.Runtime.Providers) != 1 {
		t.Fatal("environment constructed unknown provider")
	}

	memory := cfg.Derivation.Runtime.Providers["memory"]
	if memory.OpenAI == nil || memory.OpenAI.Model != "yaml-model" || memory.OpenAI.APIKey != "" {
		t.Fatal("unset environment changed YAML")
	}

	if memory.CodexACP != nil || memory.AIStudio != nil || memory.PoolConfig != nil {
		t.Fatal("unset environment changed YAML or instantiated unused blocks")
	}

	cfg, err = standalonecfg.LoadConfig("")
	requireNoError(t, err, "%v")

	if len(cfg.Derivation.Runtime.Providers) != 0 {
		t.Fatal("environment constructed a provider registry")
	}
}

func TestConfiguredProviderAmbiguousEnvironment(t *testing.T) {
	for _, test := range []struct{ name, providers, env string }{
		{"case_fold", "      MEMORY:\n        type: openai\n      memory:\n        type: openai\n",
			"BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_OPENAI_API_KEY"},
		{"dot_underscore", "      a.b:\n        type: openai\n      a_b:\n        type: openai\n",
			"BOYAN_DERIVATION_RUNTIME_PROVIDERS_A_B_OPENAI_API_KEY"},
		{"dotted_id", "      a.b:\n        type: openai\n",
			"BOYAN_DERIVATION_RUNTIME_PROVIDERS_A_B_OPENAI_API_KEY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadProviderConfig(t, test.providers)
			requireNoError(t, err, "%v")
			t.Setenv(test.env, "dummy-private-override")

			_, err = loadProviderConfig(t, test.providers)
			if err == nil || !strings.Contains(err.Error(), "ambiguous") ||
				strings.Contains(err.Error(), "dummy-private-override") {
				t.Fatal("ambiguous provider override must fail without revealing its value")
			}
		})
	}
}

func TestConfigurationInvalidEnvironmentRedacted(t *testing.T) {
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_OPENAI_API_KEY", "dummy-private-credential")
	t.Setenv("BOYAN_DERIVATION_TIMEOUT", "dummy-private-credential")

	_, err := loadProviderConfig(t, `      memory:
        type: openai
        openai:
          model: yaml-model
`)
	if err == nil ||
		!strings.Contains(err.Error(), "decode Boyan config") || strings.Contains(err.Error(), "dummy-private-credential") {
		t.Fatal("invalid typed environment must return a redacted decode error")
	}
}

func TestConfiguredProviderNumericYAMLIDEnvironment(t *testing.T) {
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_123_OPENAI_API_KEY", "dummy-numeric")
	t.Setenv("BOYAN_DERIVATION_RUNTIME_PROVIDERS_MEMORY_OPENAI_API_KEY", "dummy-memory")
	cfg, err := loadProviderConfig(t, `      123:
        type: openai
        openai:
          model: numeric-model
      memory:
        type: openai
        openai:
          model: memory-model
`)
	requireNoError(t, err, "%v")

	for id, want := range map[string]string{"123": "dummy-numeric", "memory": "dummy-memory"} {
		provider := cfg.Derivation.Runtime.Providers[id]
		if provider.OpenAI == nil || provider.OpenAI.APIKey != want {
			t.Errorf("provider %q did not load credential override", id)
		}
	}
}
