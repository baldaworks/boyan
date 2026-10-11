package mcp

import (
	"fmt"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) failureForTool(
	toolName string,
	code memory.ErrorCode,
	message string,
) (
	*sdkmcp.CallToolResult,
	ToolOutcome,
) {
	if code == "" {
		code = memory.CodePermanent
	}

	message = strings.TrimSpace(message)
	if message == "" {
		message = publicErrorMessageForCode(code)
	}

	return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("%s: %s", toolName, message)}},
		}, ToolOutcome{
			OK: false,
			Error: &ToolError{
				Operation: toolName,
				Code:      string(code),
				Message:   fmt.Sprintf("%s: %s", toolName, message),
			},
		}
}
