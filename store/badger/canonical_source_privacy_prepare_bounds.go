package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// Bound encoded values before ValueCopy or JSON allocation, including opaque
// compatibility records. A page stores at most32 keys; transactions load one
// authoritative record and at most the existing256 direct evidence/parent refs.
func getPrivacyPreparationRecord(txn *badger.Txn, key []byte, kind string, target any) error {
	if err := checkPrivacyPreparationValue(txn, key); err != nil {
		return err
	}

	return getCanonicalRecord(txn, key, kind, target)
}

func checkPrivacyPreparationValue(txn *badger.Txn, key []byte) error {
	item, err := txn.Get(key)
	if err != nil {
		return err
	}

	if item.ValueSize() > maxCanonicalLogicalLineBytes {
		return invalidSourcePrivacy()
	}

	return nil
}

func loadHistoricalSourcePrivacy(txn *badger.Txn, source memory.CanonicalSourceRecord) (sourcePrivacy, bool, error) {
	key, err := canonicalKey(source.Scope, badgerRecordSourcePrivacy, source.SourceID)
	if err != nil {
		return sourcePrivacy{}, false, err
	}

	if err := checkPrivacyPreparationValue(txn, key); err != nil && !privacyMissing(err) {
		return sourcePrivacy{}, false, err
	}

	return loadSourcePrivacy(txn, source)
}

func loadHistoricalTurnCertificate(
	txn *badger.Txn,
	scope memory.Scope,
	id string,
) (memory.CanonicalTurnCertificate, bool, error) {
	key, err := canonicalKey(scope, badgerRecordTurnCertificate, id)
	if err != nil {
		return memory.CanonicalTurnCertificate{}, false, err
	}

	if err := checkPrivacyPreparationValue(txn, key); err != nil && !privacyMissing(err) {
		return memory.CanonicalTurnCertificate{}, false, err
	}

	return loadTurnCertificate(txn, scope, id)
}
