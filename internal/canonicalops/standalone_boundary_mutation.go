package canonicalops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) buildBoundaryMutation(
	ctx context.Context,
	request memory.CommitRequest,
	view memory.CanonicalBoundaryView,
) (
	memory.CanonicalMutation,
	error,
) {
	parentRefs, evidence, err := s.translateBoundaryParents(ctx, request.Scope, view.LegacyToCanonical, request)
	if err != nil {
		return memory.CanonicalMutation{}, err
	}

	if len(evidence) == 0 {
		return memory.CanonicalMutation{}, errBoundaryEvidenceRequired
	}

	mutation := memory.CanonicalMutation{
		BoundaryIdentity:     request.BoundaryIdentity,
		SchemaVersion:        memory.CanonicalSchemaVersionV1,
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		Operation: memory.OperationRecord{
			OperationID: request.OperationID,
			Fingerprint: stableCanonicalFingerprint("boundary-mutation", request),
			CommittedAt: operationCommittedAt(request),
		},
	}

	existingItems := existingBoundaryItems(view)
	if err := appendBoundaryScenarioMutation(&mutation, request, existingItems, parentRefs, evidence); err != nil {
		return memory.CanonicalMutation{}, err
	}

	if err := appendBoundaryProfileMutation(&mutation, request, existingItems, parentRefs, evidence); err != nil {
		return memory.CanonicalMutation{}, err
	}

	if err := inheritBoundaryRevisions(request, view, &mutation); err != nil {
		return memory.CanonicalMutation{}, err
	}

	if err := mutation.Validate(); err != nil {
		return memory.CanonicalMutation{}, err
	}

	return mutation, nil
}
