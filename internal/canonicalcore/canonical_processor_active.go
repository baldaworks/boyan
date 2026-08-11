package canonicalcore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) loadCanonicalActiveView(ctx context.Context, scope Scope) (ScopeState, []ActiveCanonicalMemory, []ActiveMemoryItem, error) {
	scopeState, err := p.store.LoadScopeState(ctx, scope)
	if err != nil {
		return ScopeState{}, nil, nil, err
	}
	if scopeState.Scope != scope {
		return ScopeState{}, nil, nil, PermanentError(CodeScopeViolation, "canonical scope state does not match turn", nil)
	}
	active, err := p.store.ScanActiveMemory(ctx, ActiveMemoryScanRequest{
		Scope: scope,
		Limit: maxCanonicalReadRecords,
	})
	if err != nil {
		return ScopeState{}, nil, nil, err
	}
	activeView := make([]ActiveMemoryItem, len(active))
	for i, memory := range active {
		if err := validateActiveCanonicalMemory(scope, memory); err != nil {
			return ScopeState{}, nil, nil, err
		}
		activeView[i] = ActiveMemoryItem{
			Item:     memory.Item,
			Evidence: append([]EvidenceRef(nil), memory.Evidence...),
		}
	}
	return scopeState, active, activeView, nil
}
