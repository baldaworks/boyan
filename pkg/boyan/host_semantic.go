package boyan

import (
	"context"
	"fmt"

	"github.com/baldaworks/boyan/internal/standaloneruntime"
	"github.com/baldaworks/boyan/pkg/memory"
)

// SemanticStatus is a bounded local diagnostic; it contains no source text,
// query, endpoint, credential, or upstream response.
type SemanticStatus = standaloneruntime.SemanticStatus

// SemanticStatus reports semantic compatibility for one exact scope without
// probing the provider. Lexical service readiness remains available separately.
func (host *Host) SemanticStatus(ctx context.Context, scope memory.Scope) (SemanticStatus, error) {
	if host == nil || host.runtime == nil {
		return SemanticStatus{}, fmt.Errorf("Boyan host runtime is unavailable")
	}

	if reporter, ok := host.runtime.(interface {
		SemanticStatus(ctx context.Context, scope memory.Scope) (standaloneruntime.SemanticStatus, error)
	}); ok {
		return reporter.SemanticStatus(ctx, scope)
	}

	return SemanticStatus{State: "lexical"}, nil
}
