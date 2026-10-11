package mcp

import stdhttp "net/http"

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// NewHandler constructs the Streamable HTTP MCP handler for the Boyan tools.
func NewHandler(cfg Config) (stdhttp.Handler, error) {
	server, err := NewServer(cfg)
	if err != nil {
		return nil, err
	}

	return sdkmcp.NewStreamableHTTPHandler(
		func(
			*stdhttp.Request,
		) *sdkmcp.Server {
			return server
		},
		cfg.StreamableOptions,
	), nil
}
