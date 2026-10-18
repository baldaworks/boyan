package canonicalsql

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Validate against immutable parents and their current heads in the same write
// transaction as the new stage. Already-committed operation replay skips this
// new-write path and retains its original outcome.
func validateCanonicalBoundaryGrounding(ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation) error {
	proof := mutation.BoundaryGrounding
	if proof == nil {
		return nil
	}

	if !mutation.Operation.CommittedAt.Equal(proof.ProcessingAt) {
		return memory.PermanentError(memory.CodeInvalidDerived, "boundary commit time differs from processing time", nil)
	}

	resolver := newBoundaryBoundsResolver(ctx, txn, mutation.Scope, proof.ProcessingAt)
	for _, revision := range mutation.Revisions {
		parents, err := resolveGroundingCommitParents(resolver, proof.Parents[revision.RevisionID], proof.ProcessingAt)
		if err != nil {
			return err
		}

		expected, err := memory.InheritBoundaryMetadata(proof.ProcessingAt, parents)
		if err != nil {
			return err
		}

		if !groundingTemporalMatches(revision.Temporal, expected.Temporal) ||
			revision.Sensitivity != expected.Sensitivity || revision.Retention != expected.Retention {
			return memory.PermanentError(memory.CodeInvalidDerived,
				"synthesized revision does not preserve verified parent constraints", nil)
		}
	}

	return nil
}

func resolveGroundingCommitParents(resolver *boundaryBoundsResolver,
	refs []memory.RevisionRef, at time.Time) ([]memory.BoundaryParentMetadata, error) {
	parents := make([]memory.BoundaryParentMetadata, 0, len(refs))
	for _, ref := range refs {
		if err := resolver.requireCurrent(ref); err != nil {
			return nil, err
		}

		parent, err := resolver.resolve(ref)
		if err != nil {
			return nil, err
		}

		if !memory.TemporalEligibleAt(parent.Temporal, at) || parent.Sensitivity != memory.SensitivityStandard {
			return nil, memory.PermanentError(memory.CodeInvalidDerived,
				"boundary semantic parent is not eligible at processing time", nil)
		}

		parents = append(parents, parent)
	}

	return parents, nil
}

func groundingTemporalMatches(actual, expected memory.Temporal) bool {
	return actual.ObservedAt.Equal(expected.ObservedAt) &&
		groundingOptionalTimeMatches(actual.ValidFrom, expected.ValidFrom) &&
		groundingOptionalTimeMatches(actual.ValidUntil, expected.ValidUntil) &&
		groundingOptionalTimeMatches(actual.ExpiresAt, expected.ExpiresAt)
}

func groundingOptionalTimeMatches(actual, expected *time.Time) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}

	return actual.Equal(*expected)
}
