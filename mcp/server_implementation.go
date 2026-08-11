package mcp

import (
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func serverImplementation(impl *sdkmcp.Implementation) *sdkmcp.Implementation {
	if impl == nil {
		return &sdkmcp.Implementation{
			Name:    defaultServerName,
			Title:   defaultServerTitle,
			Version: defaultServerVersion,
		}
	}
	copyOfImpl := *impl
	if strings.TrimSpace(copyOfImpl.Name) == "" {
		copyOfImpl.Name = defaultServerName
	}
	if strings.TrimSpace(copyOfImpl.Title) == "" {
		copyOfImpl.Title = defaultServerTitle
	}
	if strings.TrimSpace(copyOfImpl.Version) == "" {
		copyOfImpl.Version = defaultServerVersion
	}
	return &copyOfImpl
}
