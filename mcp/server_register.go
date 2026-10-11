package mcp

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// RegisterTools registers the Boyan tools on an existing MCP server.
func RegisterTools(server *sdkmcp.Server, cfg Config) {
	if server == nil {
		return
	}

	New(cfg).RegisterTools(server)
}
