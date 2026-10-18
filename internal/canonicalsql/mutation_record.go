package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	mutationVisiting uint8 = 1
	mutationVisited  uint8 = 2
)

func putMutationRecord(ctx context.Context, tx *Tx, scope memory.Scope, family Family, id string, value any) error {
	key, err := privacyKey(scope, family, id)
	if err != nil {
		return err
	}

	return putImmutablePrivacyRecord(ctx, tx, key, value)
}

func putMutationImmutableRecord(ctx context.Context, tx *Tx, key Key, family Family, value any) error {
	if key.Family != family {
		return tx.fail(invalidSourcePrivacy())
	}

	return putImmutablePrivacyRecord(ctx, tx, key, value)
}

func mutationEdgeKey(scope memory.Scope, family Family, first, second string) (Key, error) {
	key := Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(first), []byte(second)}}
	_, _, err := key.values()

	return key, err
}
