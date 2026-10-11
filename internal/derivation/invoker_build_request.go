package derivation

import (
	"fmt"
	"strings"

	"github.com/normahq/runtime/v2/agentfactory"
)

func dedicatedRuntimeBuildRequest(providerID, workingDir, instruction string) (agentfactory.BuildRequest, error) {
	trimmedProvider := strings.TrimSpace(providerID)
	if trimmedProvider == "" {
		return agentfactory.BuildRequest{}, fmt.Errorf("Boyan derivation provider is required")
	}

	trimmedDir := strings.TrimSpace(workingDir)
	if trimmedDir == "" {
		return agentfactory.BuildRequest{}, fmt.Errorf("Boyan derivation workspace is required")
	}

	return agentfactory.BuildRequest{
		AgentID:          trimmedProvider,
		Name:             trimmedProvider + "-boyan-derivation",
		Description:      "Dedicated Boyan structured derivation runtime",
		WorkingDirectory: trimmedDir,
		Instruction:      strings.TrimSpace(instruction),
		MCPServerIDs:     []string{},
	}, nil
}
