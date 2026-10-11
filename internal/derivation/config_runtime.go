package derivation

import (
	"time"

	"github.com/baldaworks/boyan/internal/structuredderive"
	"github.com/normahq/runtime/v2/agentfactory"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
	"github.com/normahq/runtime/v2/mcpregistry"
)

func (c Config) maxOutputBytes() int {
	if c.MaxOutputBytes > 0 {
		return c.MaxOutputBytes
	}

	return structuredderive.DefaultDerivationMaxOutputBytes
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}

	return structuredderive.DefaultDerivationTimeout
}

func newAgentFactory(cfg runtimeconfig.RuntimeConfig) *agentfactory.Factory {
	return agentfactory.New(cfg.Providers, mcpregistry.New(cfg.MCPServers))
}
