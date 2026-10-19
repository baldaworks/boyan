package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ScanActiveHeads reads a bounded exclusive page in encoded item-ID order.
func (o *Owner) ScanActiveHeads(ctx context.Context, request memory.ActiveHeadScanRequest) ([]memory.ItemHead, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, err
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}

	var heads []memory.ItemHead

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		heads, err = scanReaderHeads(ctx, tx, request)

		return err
	})
	if err != nil {
		return nil, err
	}

	return heads, nil
}

func scanReaderHeads(ctx context.Context, tx *Tx, request memory.ActiveHeadScanRequest) ([]memory.ItemHead, error) {
	rows, err := tx.activeHeadPage(ctx, request)
	if err != nil {
		return nil, err
	}

	heads := make([]memory.ItemHead, 0, len(rows))
	for _, row := range rows {
		var head memory.ItemHead
		if err := Decode(Head, row.Envelope, &head); err != nil {
			return nil, err
		}

		if head.Validate() != nil || head.ItemID != string(row.Key.Parts[0]) {
			return nil, corruptRow()
		}

		heads = append(heads, head)
	}

	return heads, nil
}

// ScanActiveMemory reads heads and their exact revisions in one snapshot.
func (o *Owner) ScanActiveMemory(ctx context.Context,
	request memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, err
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}

	var active []memory.ActiveCanonicalMemory

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		heads, err := scanReaderHeads(ctx, tx, memory.ActiveHeadScanRequest(request))
		if err != nil {
			return err
		}

		active = make([]memory.ActiveCanonicalMemory, 0, len(heads))
		for _, head := range heads {
			item, err := loadProofItem(ctx, tx, request.Scope, head.ItemID)
			if err != nil {
				return err
			}

			revision, err := loadProofRevision(ctx, tx, request.Scope, head.RevisionID)
			if err != nil {
				return err
			}

			if revision.ItemID != item.ItemID || revision.RevisionID != head.RevisionID {
				return corruptRow()
			}

			active = append(active, memory.ActiveCanonicalMemory{Item: item, RevisionID: revision.RevisionID,
				Revision: revision.Revision,
				Evidence: append([]memory.EvidenceRef(nil), revision.Evidence...)})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return active, nil
}
