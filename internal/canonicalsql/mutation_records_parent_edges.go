package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putCanonicalMutationParentEdges(ctx context.Context, txn *Tx, scope memory.Scope,
	revision memory.MemoryRevision) error {
	for _, parentID := range revision.Parents {
		edge := mutationProvenanceEdge{ChildRevisionID: revision.RevisionID, ParentRevisionID: parentID}

		forwardKey, err := mutationEdgeKey(scope, ProvenanceForward, revision.RevisionID, parentID)
		if err != nil {
			return err
		}

		if err := putMutationImmutableRecord(ctx, txn, forwardKey, ProvenanceForward, edge); err != nil {
			return err
		}

		reverseKey, err := mutationEdgeKey(scope, ProvenanceReverse, parentID, revision.RevisionID)
		if err != nil {
			return err
		}

		if err := putMutationImmutableRecord(ctx, txn, reverseKey, ProvenanceReverse, edge); err != nil {
			return err
		}
	}

	return nil
}
