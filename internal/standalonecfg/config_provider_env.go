package standalonecfg

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// bindConfiguredProviderEnvironment discovers only providers already defined in
// YAML. Registering only nonempty overrides preserves absent optional blocks and
// Viper's existing treatment of empty environment values as unset.
func bindConfiguredProviderEnvironment(loader *viper.Viper, ids []string) error {
	const providerRoot = "derivation.runtime.providers."

	normalizedCounts := make(map[string]int, len(ids))
	for _, id := range ids {
		normalizedCounts[providerEnvironmentName(id)]++
	}

	sort.Strings(ids)

	for _, id := range ids {
		for _, leaf := range providerEnvironmentLeaves() {
			key := providerRoot + id + "." + leaf

			envName := "BOYAN_" + providerEnvironmentName(key)
			if os.Getenv(envName) == "" {
				continue
			}
			// A dot in a map ID is also Viper's path separator. Reject attempted
			// bindings rather than routing them into a different provider or block.
			if strings.Contains(id, ".") || normalizedCounts[providerEnvironmentName(id)] > 1 {
				return errors.New("decode Boyan config: ambiguous configured-provider environment override")
			}

			if err := loader.BindEnv(key, envName); err != nil {
				return fmt.Errorf("bind Boyan configured-provider environment: %w", err)
			}
		}
	}

	return nil
}

func providerEnvironmentName(key string) string {
	return strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// This finite catalog matches agentconfig.Config in runtime/v2 v2.0.8.
// Arbitrary registries (including MCP server maps) are intentionally excluded.
func providerEnvironmentLeaves() []string {
	leaves := []string{
		"type", "mcp_servers", "system_instructions",
		"openai.api_key", "openai.model",
		"aistudio.api_key", "aistudio.model",
		"pool.members",
	}

	for _, block := range []string{
		"generic_acp", "gemini_acp", "codex_acp", "opencode_acp",
		"copilot_acp", "claude_code_acp", "grok_acp",
	} {
		for _, leaf := range []string{"cmd", "extra_args", "model", "reasoning_effort", "mode", "bridge_version"} {
			leaves = append(leaves, block+"."+leaf)
		}
	}

	return leaves
}

// configuredProviderIDs retains the original map keys before Viper case folding.
// Traverse only the fixed registry path; environment values never create IDs.
func configuredProviderIDs(data []byte) ([]string, error) {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, errors.New("read Boyan config: invalid YAML provider configuration")
	}

	sections := []map[string]any{document}

	for _, section := range []string{"derivation", "runtime"} {
		var next []map[string]any

		for _, parent := range sections {
			for key, value := range parent {
				if strings.EqualFold(key, section) {
					if child, ok := value.(map[string]any); ok {
						next = append(next, child)
					}
				}
			}
		}

		sections = next
	}

	var ids []string

	for _, runtime := range sections {
		for key, providers := range runtime {
			if strings.EqualFold(key, "providers") {
				ids = append(ids, providerRegistryIDs(providers)...)
			}
		}
	}

	return ids, nil
}

// YAML uses a mixed-key map for unquoted scalar IDs; Viper converts these
// IDs to strings. Keep every original entry so normalization collisions remain
// detectable, including a numeric ID next to its quoted string spelling.
func providerRegistryIDs(providers any) []string {
	var ids []string

	switch registry := providers.(type) {
	case map[string]any:
		for id := range registry {
			ids = append(ids, id)
		}
	case map[any]any:
		for id := range registry {
			ids = append(ids, fmt.Sprint(id))
		}
	}

	return ids
}
