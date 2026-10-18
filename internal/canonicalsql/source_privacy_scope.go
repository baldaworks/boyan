package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type sourcePrivacyScope struct {
	Version    uint32       `json:"version"`
	Scope      memory.Scope `json:"scope"`
	Incomplete bool         `json:"incomplete"`
}

func putPrivacyScope(ctx context.Context, txn *Tx, scope memory.Scope, incomplete bool) error {
	existing, err := privacyScopeIncomplete(ctx, txn, scope)
	if err != nil {
		return err
	}

	key, err := privacyKey(scope, SourcePrivacyScope, "coverage")
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, key, SourcePrivacyScope,
		sourcePrivacyScope{Version: sourcePrivacyVersion, Scope: scope, Incomplete: incomplete || existing})
}

func privacyScopeIncomplete(ctx context.Context, txn *Tx, scope memory.Scope) (bool, error) {
	key, err := privacyKey(scope, SourcePrivacyScope, "coverage")
	if err != nil {
		return false, err
	}

	var record sourcePrivacyScope

	err = getPrivacyRecord(ctx, txn, key, SourcePrivacyScope, &record)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil || record.Version != sourcePrivacyVersion || record.Scope != scope {
		return false, invalidSourcePrivacy()
	}

	return record.Incomplete, nil
}
