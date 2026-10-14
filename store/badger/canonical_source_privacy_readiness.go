package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const badgerRecordSourcePrivacyReady = "source-privacy-ready"

type sourcePrivacyReadiness struct {
	Version         uint32 `json:"version"`
	CoverageVersion uint32 `json:"coverage_version"`
}

func sourcePrivacyReadinessKey() []byte {
	key, _ := memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordSourcePrivacyReady)

	return key
}

func requireSourcePrivacyReady(txn *badger.Txn) error {
	var marker sourcePrivacyReadiness

	err := getCanonicalRecord(txn, sourcePrivacyReadinessKey(), badgerRecordSourcePrivacyReady, &marker)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.RetryableError(memory.CodeUnavailable, "canonical source privacy is not prepared", nil)
	}

	if err != nil || marker.Version != sourcePrivacyVersion || marker.CoverageVersion != sourcePrivacyVersion {
		return invalidSourcePrivacy()
	}

	return nil
}

func publishSourcePrivacyReady(txn *badger.Txn) error {
	return putCanonicalRecord(txn, sourcePrivacyReadinessKey(), badgerRecordSourcePrivacyReady,
		sourcePrivacyReadiness{Version: sourcePrivacyVersion, CoverageVersion: sourcePrivacyVersion})
}

// Only a truly empty canonical namespace can acquire capability at open.
// Populated old stores require the explicit offline preparation pass.
func initializeEmptySourcePrivacy(db *badger.DB) error {
	return db.Update(func(txn *badger.Txn) error {
		prefix := []byte{canonicalNamespace, canonicalVersion}
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		options.PrefetchValues = false
		iterator := txn.NewIterator(options)
		iterator.Seek(prefix)
		populated := iterator.ValidForPrefix(prefix)
		iterator.Close()

		if populated {
			return nil
		}

		return publishSourcePrivacyReady(txn)
	})
}

func checkCanonicalWriterFence(txn *badger.Txn, scope memory.Scope) error {
	if err := requireSourcePrivacyReady(txn); err != nil {
		return err
	}

	return checkScopeEraseFence(txn, scope, "")
}
