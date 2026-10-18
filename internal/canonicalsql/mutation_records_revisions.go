package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type mutationProvenanceEdge struct {
	ChildRevisionID  string `json:"child_revision_id"`
	ParentRevisionID string `json:"parent_revision_id"`
}

func putCanonicalMutationRevisions(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	revisions []memory.MemoryRevision,
) error {
	for _, revision := range revisions {
		if err := putMutationRecord(ctx, txn, scope, Revision, revision.RevisionID, revision); err != nil {
			return err
		}

		if err := putCanonicalMutationParentEdges(ctx, txn, scope, revision); err != nil {
			return err
		}

		if err := putCanonicalMutationEvidenceEdges(ctx, txn, scope, revision); err != nil {
			return err
		}
	}

	return nil
}
