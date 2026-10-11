package forgetops

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type scopeForgetTargets struct {
	scopeVersion uint64
	sources      []memory.SourceRef
	sourceIDs    []string
	revisions    []memory.RevisionRef
	revisionIDs  []string
}

func (a *CanonicalForgetAdapter) loadScopeForgetTargets(
	ctx context.Context,
	scope memory.Scope,
) (
	scopeForgetTargets,
	error,
) {
	scopeState, err := a.store.LoadScopeState(ctx, scope)
	if err != nil {
		return scopeForgetTargets{}, err
	}

	sources, sourceIDs, err := listCanonicalSources(ctx, a.store, scope)
	if err != nil {
		return scopeForgetTargets{}, err
	}

	revisions, revisionIDs, err := listCanonicalRevisions(ctx, a.store, scope)
	if err != nil {
		return scopeForgetTargets{}, err
	}

	return scopeForgetTargets{
		scopeVersion: scopeState.Version,
		sources:      sources,
		sourceIDs:    sourceIDs,
		revisions:    revisions,
		revisionIDs:  revisionIDs,
	}, nil
}

func (a *CanonicalForgetAdapter) denyScopeForgetTargets(
	ctx context.Context,
	scope memory.Scope,
	sourceIDs []string,
	revisionIDs []string,
	forgottenAt time.Time,
) error {
	for _, sourceID := range sourceIDs {
		if err := a.store.DenySource(ctx, scope, sourceID, forgottenAt); err != nil {
			return err
		}
	}

	for _, revisionID := range revisionIDs {
		if err := a.store.DenyRevision(ctx, scope, revisionID, forgottenAt); err != nil {
			return err
		}
	}

	return nil
}
