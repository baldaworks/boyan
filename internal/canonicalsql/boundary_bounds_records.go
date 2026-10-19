package canonicalsql

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func loadProofRevision(ctx context.Context, tx *Tx, scope memory.Scope, id string) (memory.MemoryRevision, error) {
	key, err := privacyKey(scope, Revision, id)
	if err != nil {
		return memory.MemoryRevision{}, err
	}

	data, err := tx.Get(ctx, key)
	if err != nil {
		return memory.MemoryRevision{}, err
	}

	return decodeProofRevision(key, data)
}

func decodeProofRevision(key Key, data []byte) (memory.MemoryRevision, error) {
	var revision memory.MemoryRevision
	if err := decodePrivacyRecord(key, Revision, data, &revision); err != nil {
		return revision, err
	}

	if err := revision.Validate(); err != nil {
		return revision, proofStoreFailure("invalid canonical revision", err)
	}

	return revision, nil
}

func loadProofItem(ctx context.Context, tx *Tx, scope memory.Scope, id string) (memory.MemoryItem, error) {
	key, err := privacyKey(scope, Item, id)
	if err != nil {
		return memory.MemoryItem{}, err
	}

	data, err := tx.Get(ctx, key)
	if err != nil {
		return memory.MemoryItem{}, err
	}

	return decodeProofItem(key, data)
}

func decodeProofItem(key Key, data []byte) (memory.MemoryItem, error) {
	var item memory.MemoryItem
	if err := decodePrivacyRecord(key, Item, data, &item); err != nil {
		return item, err
	}

	if err := item.Validate(); err != nil || item.Scope != key.Scope || item.ItemID != string(key.Parts[0]) {
		return item, proofStoreFailure("invalid canonical item", err)
	}

	return item, nil
}

func requireProofHead(ctx context.Context, tx *Tx, scope memory.Scope, ref memory.RevisionRef) error {
	key, err := privacyKey(scope, Head, ref.ItemID)
	if err != nil {
		return err
	}

	var head memory.ItemHead
	if err := getPrivacyRecord(ctx, tx, key, Head, &head); err != nil {
		return err
	}

	if head.Validate() != nil || head.ItemID != ref.ItemID || head.RevisionID != ref.RevisionID {
		return invalidBoundaryAncestry()
	}

	return nil
}

func checkProofDenial(ctx context.Context, tx *Tx, scope memory.Scope, family Family, id string) error {
	denied, err := privacyDenied(ctx, tx, scope, family, id)
	if err != nil {
		return err
	}

	if denied {
		return invalidBoundaryAncestry()
	}

	return nil
}

// resolveBoundaryRecordBounds shares the caller's admitted snapshot. Reader
// integration can use this without opening another owner transaction.
func resolveBoundaryRecordBounds(ctx context.Context, tx *Tx, scope memory.Scope,
	records []memory.RecallRecord, at time.Time) ([]memory.RecallRecord, error) {
	resolver := newBoundaryBoundsResolver(ctx, tx, scope, at)

	for index, record := range records {
		ref := memory.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID}
		if err := resolver.requireCurrent(ref); err != nil {
			return nil, err
		}

		parent, err := resolver.resolve(ref)
		if err != nil {
			return nil, err
		}

		records[index].Temporal = cloneBoundaryTemporal(parent.Temporal)
		records[index].Sensitivity = parent.Sensitivity
		records[index].Retention = parent.Retention
	}

	return records, nil
}
