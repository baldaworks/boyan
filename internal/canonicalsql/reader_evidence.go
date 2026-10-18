package canonicalsql

import (
	"context"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Preparation may retain partial captures for retry. Readers instead need a
// complete capture, a valid certificate, or an independently bound embedded
// Turn before source-wide classification can authorize content.
func readerEmbeddedMembership(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord) (bool, error) {
	data, err := readPrivacySourcePayload(ctx, tx, source)
	if err != nil {
		return false, err
	}

	var embedded memory.SourceRecord
	if json.Unmarshal(data, &embedded) != nil || embedded.Ref.Scope.Key == "" {
		return false, nil
	}

	valid, err := historicalEmbeddedSourceValid(source, embedded)
	if err != nil || !valid {
		return false, err
	}

	for _, message := range embedded.Turn.Messages {
		if err := validatePrivacyMemberBinding(ctx, tx, source, message.MessageID); err != nil {
			return false, err
		}
	}

	return true, nil
}

// Check every consumed evidence reference, not merely the first reference per
// source. The same bounded exact-key checks run in hydration and the fresh
// final snapshot, including evidence of resolved legacy ancestors.
func validateReaderSnapshotEvidence(ctx context.Context, tx *Tx, captured readerSnapshot) error {
	for _, revision := range captured.revisions {
		for _, ref := range revision.Evidence {
			source, found := captured.sources[ref.SourceID]
			if !found {
				return invalidSourcePrivacy()
			}

			if err := validatePrivacyCapturedMember(ctx, tx, source, ref.MessageID); err != nil {
				return err
			}

			if err := validatePrivacyCertifiedMember(ctx, tx, source, ref.MessageID); err != nil {
				return err
			}

			valid, err := validateHistoricalPrivacyEvidence(ctx, tx, source.Scope, ref)
			if err != nil {
				return err
			}

			if !valid {
				return invalidSourcePrivacy()
			}
		}
	}

	return ctx.Err()
}
