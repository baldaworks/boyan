package mcp

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// NewServer constructs a standalone Boyan MCP server with all four tools registered.
func NewServer(cfg Config) (*sdkmcp.Server, error) {
	server := sdkmcp.NewServer(serverImplementation(cfg.Implementation), cfg.ServerOptions)
	New(cfg).RegisterTools(server)

	return server, nil
}
