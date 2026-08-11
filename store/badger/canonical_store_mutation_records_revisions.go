package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerProvenanceEdge struct {
	ChildRevisionID  string `json:"child_revision_id"`
	ParentRevisionID string `json:"parent_revision_id"`
}

func putCanonicalMutationRevisions(
	txn *badger.Txn,
	scope memory.Scope,
	revisions []memory.MemoryRevision,
) error {
	for _, revision := range revisions {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordRevision, revision.RevisionID, revision); err != nil {
			return err
		}
		if err := putCanonicalMutationParentEdges(txn, scope, revision); err != nil {
			return err
		}
		if err := putCanonicalMutationEvidenceEdges(txn, scope, revision); err != nil {
			return err
		}
	}
	return nil
}
