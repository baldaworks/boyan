package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalViewCore(txn *badger.Txn, scope memory.Scope, revisionID string, activeOnly bool, view *canonicalRecallView) error {
	if err := loadCanonicalViewRevision(txn, scope, revisionID, view); err != nil {
		return err
	}
	if err := loadCanonicalViewItem(txn, scope, view); err != nil {
		return err
	}
	return loadCanonicalViewHead(txn, scope, activeOnly, view)
}
