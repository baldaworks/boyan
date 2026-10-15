package standaloneruntime

import (
	"context"
	"fmt"

	"github.com/baldaworks/boyan/internal/semanticprojection"
	"github.com/baldaworks/boyan/pkg/memory"
)

const semanticModelSpacePrefixLength = 12

// SemanticStatus contains bounded, non-secret diagnostics for one exact scope.
type SemanticStatus struct {
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
	ModelSpacePrefix string `json:"model_space_prefix,omitempty"`
	LexicalWatermark uint64 `json:"lexical_watermark,omitempty"`
}

// SemanticStatus reads only local compatibility metadata; it never probes the provider.
//
//nolint:cyclop // Context, scope, lifetime and compatibility gates remain explicit.
func (runtime *Runtime) SemanticStatus(ctx context.Context, scope memory.Scope) (SemanticStatus, error) {
	if ctx == nil {
		return SemanticStatus{}, fmt.Errorf("semantic status context is required")
	}

	if err := ctx.Err(); err != nil {
		return SemanticStatus{}, err
	}

	if err := scope.Validate(); err != nil {
		return SemanticStatus{}, err
	}

	if runtime == nil || runtime.service == nil {
		return SemanticStatus{}, fmt.Errorf("Boyan runtime is unavailable")
	}

	if runtime.service.semantic == nil {
		return SemanticStatus{State: "lexical"}, nil
	}

	operation, done, err := runtime.service.beginHealthOperation(ctx)
	if err != nil {
		return SemanticStatus{State: "stopped"}, nil
	}
	defer done()

	semantic := runtime.service.semantic

	status, err := semantic.manager.Status(operation, scope)
	if err != nil {
		return SemanticStatus{}, err
	}

	result := SemanticStatus{State: string(status.State), Reason: string(status.SafeReason),
		LexicalWatermark: status.LexicalWatermark}
	if len(status.ModelSpace) >= semanticModelSpacePrefixLength {
		result.ModelSpacePrefix = status.ModelSpace[:semanticModelSpacePrefixLength]
	}

	if status.State == semanticprojection.StateReady {
		result.State = "hybrid-ready"
		if semantic.provider.failed.Load() {
			result.State = "degraded"
			result.Reason = "unavailable"
		}
	}

	return result, nil
}
