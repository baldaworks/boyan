package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const badgerRecordSourcePrivacyScope = "source-privacy-scope"

type sourcePrivacyScope struct {
	Version    uint32       `json:"version"`
	Scope      memory.Scope `json:"scope"`
	Incomplete bool         `json:"incomplete"`
}

func putPrivacyScope(txn *badger.Txn, scope memory.Scope, incomplete bool) error {
	existing, err := privacyScopeIncomplete(txn, scope)
	if err != nil {
		return err
	}

	key, err := canonicalKey(scope, badgerRecordSourcePrivacyScope, "coverage")
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordSourcePrivacyScope,
		sourcePrivacyScope{Version: sourcePrivacyVersion, Scope: scope, Incomplete: incomplete || existing})
}

func privacyScopeIncomplete(txn *badger.Txn, scope memory.Scope) (bool, error) {
	key, err := canonicalKey(scope, badgerRecordSourcePrivacyScope, "coverage")
	if err != nil {
		return false, err
	}

	var record sourcePrivacyScope

	err = getPrivacyPreparationRecord(txn, key, badgerRecordSourcePrivacyScope, &record)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil || record.Version != sourcePrivacyVersion || record.Scope != scope {
		return false, invalidSourcePrivacy()
	}

	return record.Incomplete, nil
}
