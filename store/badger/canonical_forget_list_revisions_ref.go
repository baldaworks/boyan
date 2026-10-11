package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func scanCanonicalRevisionRef(
	txn *badger.Txn,
	scope memory.Scope,
	key []byte,
	afterRevisionID string,
) (memory.RevisionRef, bool, error) {
	var revision memory.MemoryRevision
	if err := getCanonicalRecord(txn, key, badgerRecordRevision, &revision); err != nil {
		return memory.RevisionRef{}, false, err
	}

	if revision.RevisionID <= afterRevisionID {
		return memory.RevisionRef{}, true, nil
	}

	denied, err := isDeniedCanonicalRevision(txn, scope, revision.RevisionID)
	if err != nil || denied {
		return memory.RevisionRef{}, denied, err
	}

	return memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID}, false, nil
}
