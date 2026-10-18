package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// VerifyCanonicalIntegrity composes structural integrity with the portable
// proof, privacy and committed-outcome validators in the same snapshot.
func (o *Owner) VerifyCanonicalIntegrity(ctx context.Context) error {
	if err := checkArchiveContext(ctx); err != nil {
		return err
	}

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return tx.verifyCanonicalIntegrity(ctx, archivePageSize)
	})
	if ctxErr := checkArchiveContext(ctx); ctxErr != nil {
		return ctxErr
	}

	return err
}

func (tx *Tx) verifyCanonicalIntegrity(ctx context.Context, pageSize int) error {
	if err := tx.checkIntegrity(ctx, pageSize); err != nil {
		return err
	}

	var after *StampCursor
	for {
		page, err := tx.ScanStamps(ctx, after, pageSize)
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return nil
		}

		for _, stamp := range page {
			if stamp.Deleted {
				continue
			}

			if err := tx.validateArchiveRecord(ctx, stamp.Key, stamp.Envelope); err != nil {
				return err
			}
		}

		cursor := page[len(page)-1].Cursor
		after = &cursor
	}
}

func (tx *Tx) validateArchiveRecord(ctx context.Context, key Key, encoded []byte) error {
	envelope, err := memory.UnmarshalRecordEnvelope(encoded)
	if err != nil {
		return corruptRow()
	}

	components := []string{string(key.Family)}

	def, _, err := key.values()
	if err != nil {
		return err
	}

	if def.scoped {
		components = append(components, key.Scope.Key, string(key.Scope.Kind))
	}

	for _, part := range key.Parts {
		components = append(components, string(part))
	}

	if err := validateCanonicalProofRecord(components, envelope); err != nil {
		return err
	}

	if err := validateStoredPrivacyBindings(ctx, tx, components, envelope); err != nil {
		return err
	}

	return validateStoredCanonicalOutcomeBindings(ctx, tx, components, envelope)
}
