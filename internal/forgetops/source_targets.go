package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type sourceForgetTargets struct {
	sourceID     string
	scopeVersion uint64
	revisions    []memory.RevisionRef
}

func (a *CanonicalForgetAdapter) loadSourceForgetTargets(ctx context.Context, command memory.ForgetSourceCommand, sourceID string) (sourceForgetTargets, error) {
	scopeState, err := a.store.LoadScopeState(ctx, command.Source.Scope)
	if err != nil {
		return sourceForgetTargets{}, err
	}
	revisionIDs, err := a.sourceRevisionIDs(ctx, command.Source.Scope, sourceID, command.ForgottenAt)
	if err != nil {
		return sourceForgetTargets{}, err
	}
	revisions, err := canonicalRevisionRefs(ctx, a.store, command.Source.Scope, revisionIDs)
	if err != nil {
		return sourceForgetTargets{}, err
	}
	return sourceForgetTargets{
		sourceID:     sourceID,
		scopeVersion: scopeState.Version,
		revisions:    revisions,
	}, nil
}
