package derivation

import (
	"fmt"
	"strings"
	"time"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	"github.com/normahq/runtime/v2/agentfactory"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	"github.com/normahq/runtime/v2/mcpregistry"
)

const (
	defaultRuntimeAppName = "boyan-derivation"
	defaultDerivationUser = "boyan-derivation"
)

// Config configures the isolated Boyan derivation runtime.
type Config struct {
	// Runtime is the provider and MCP registry used by the derivation runtime.
	Runtime runtimeconfig.RuntimeConfig
	// ProviderID selects the dedicated derivation provider entry.
	ProviderID string
	// WorkingDir is the runtime working directory for derivation sessions.
	WorkingDir string
	// MaxOutputBytes bounds one structured derivation result. Zero uses the
	// portable app default.
	MaxOutputBytes int
	// Timeout bounds one structured derivation invocation. Zero uses the
	// portable app default.
	Timeout time.Duration
}

// Validate checks the runtime registry and the selected derivation provider.
func (c Config) Validate() error {
	return c.validateWithFactory(newAgentFactory(c.Runtime))
}

func (c Config) validateWithFactory(factory validatingFactory) error {
	if err := c.Runtime.Validate(); err != nil {
		return fmt.Errorf("boyan derivation runtime: %w", err)
	}
	if factory == nil {
		return fmt.Errorf("boyan derivation runtime factory is required")
	}
	if strings.TrimSpace(c.ProviderID) == "" {
		return fmt.Errorf("boyan derivation provider is required")
	}
	if strings.TrimSpace(c.WorkingDir) == "" {
		return fmt.Errorf("boyan derivation working directory is required")
	}
	if c.MaxOutputBytes < 0 {
		return fmt.Errorf("boyan derivation max_output_bytes must be non-negative")
	}
	if c.Timeout < 0 {
		return fmt.Errorf("boyan derivation timeout must be non-negative")
	}
	if err := factory.ValidateAgent(strings.TrimSpace(c.ProviderID)); err != nil {
		return fmt.Errorf("boyan derivation provider %q: %w", strings.TrimSpace(c.ProviderID), err)
	}
	return nil
}

func (c Config) maxOutputBytes() int {
	if c.MaxOutputBytes > 0 {
		return c.MaxOutputBytes
	}
	return portableapp.DefaultDerivationMaxOutputBytes
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return portableapp.DefaultDerivationTimeout
}

func newAgentFactory(cfg runtimeconfig.RuntimeConfig) *agentfactory.Factory {
	return agentfactory.New(cfg.Providers, mcpregistry.New(cfg.MCPServers))
}
