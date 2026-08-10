package derivation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	portableapp "github.com/baldaworks/boyan/internal/apps/boyan"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentfactory"
	adksession "google.golang.org/adk/v2/session"
)

func (i *Invoker) ensureRuntime(ctx context.Context) (*builtRuntime, error) {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil, memory.PermanentError(memory.CodeModelFailure, "Boyan derivation runtime is closed", nil)
	}
	if i.runtime == nil {
		runtime, err := i.buildRuntime(ctx)
		if err != nil {
			i.mu.Unlock()
			return nil, err
		}
		i.runtime = runtime
	}
	runtime := i.runtime
	i.mu.Unlock()
	return runtime, nil
}

func (i *Invoker) buildRuntime(ctx context.Context) (*builtRuntime, error) {
	req, err := dedicatedRuntimeBuildRequest(i.providerID, i.workingDir, portableapp.DefaultDerivationInstruction)
	if err != nil {
		return nil, err
	}
	agent, err := i.factory.Build(ctx, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, memory.RetryableError(memory.CodeTimeout, "build Boyan derivation runtime timed out", nil)
		}
		return nil, memory.RetryableError(memory.CodeModelFailure, "build Boyan derivation runtime", nil)
	}
	return &builtRuntime{
		agent:      agent,
		sessionSvc: adksession.InMemoryService(),
		appName:    defaultRuntimeAppName,
	}, nil
}

// Close releases resources owned by the isolated derivation runtime.
func (i *Invoker) Close(context.Context) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	runtime := i.runtime
	i.runtime = nil
	i.mu.Unlock()

	if runtime == nil || runtime.agent == nil {
		return nil
	}
	if closer, ok := runtime.agent.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

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

func processorSessionID(operationID, stage string) string {
	hash := sha256.Sum256([]byte(operationID + "\x00" + stage))
	return "boyan-derivation-" + hex.EncodeToString(hash[:])
}
