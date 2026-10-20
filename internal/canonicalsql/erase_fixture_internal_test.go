//go:build integration

package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putEraseFixtureRecord(ctx context.Context, tx *Tx, scope memory.Scope, family Family, id string, value any) error {
	key, err := privacyKey(scope, family, id)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, family, value)
}
