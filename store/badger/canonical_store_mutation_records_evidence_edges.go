package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalMutationEvidenceEdges(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) error {
	for _, evidence := range revision.Evidence {
		key, err := badgerProvenanceKey(scope, badgerRecordSourceRevision, evidence.SourceID, revision.RevisionID)
		if err != nil {
			return err
		}
		edge := badgerProvenanceEdge{ChildRevisionID: revision.RevisionID, ParentRevisionID: evidence.SourceID}
		if err := putCanonicalImmutableRecord(txn, key, badgerRecordSourceRevision, edge); err != nil {
			return err
		}
	}
	return nil
}
