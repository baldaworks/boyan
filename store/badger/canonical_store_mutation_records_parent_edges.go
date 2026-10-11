package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalMutationParentEdges(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) error {
	for _, parentID := range revision.Parents {
		edge := badgerProvenanceEdge{ChildRevisionID: revision.RevisionID, ParentRevisionID: parentID}

		forwardKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceForward, revision.RevisionID, parentID)
		if err != nil {
			return err
		}

		if err := putCanonicalImmutableRecord(txn, forwardKey, badgerRecordProvenanceForward, edge); err != nil {
			return err
		}

		reverseKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceReverse, parentID, revision.RevisionID)
		if err != nil {
			return err
		}

		if err := putCanonicalImmutableRecord(txn, reverseKey, badgerRecordProvenanceReverse, edge); err != nil {
			return err
		}
	}

	return nil
}
