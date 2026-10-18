package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// captureNewPrivacySources runs before immutable sources are inserted, so a
// repeated source envelope cannot turn a later subset into initial authority.
func captureNewPrivacySources(ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation) (map[string]bool, error) {
	fresh := make(map[string]bool, len(mutation.Sources))
	for _, source := range mutation.Sources {
		key, err := privacyKey(mutation.Scope, Source, source.SourceID)
		if err != nil {
			return nil, err
		}

		_, err = txn.Get(ctx, key)
		if err != nil && !privacyMissing(err) {
			return nil, err
		}

		if privacyMissing(err) {
			historical, historyErr := hasPrivacySourceHistory(ctx, txn, mutation.Scope, source.SourceID)
			if historyErr != nil {
				return nil, historyErr
			}

			fresh[source.SourceID] = !historical
		}
	}

	return fresh, nil
}

// Capture membership additions before immutable writes. A standard subset
// cannot authorize new raw messages appended to an already classified source.
func capturePrivacyMessageExtensions(ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation) (map[string]bool, error) {
	extensions := make(map[string]bool, len(mutation.Messages))
	for _, message := range mutation.Messages {
		key, err := privacyKey(mutation.Scope, Message, message.MessageID)
		if err != nil {
			return nil, err
		}

		_, err = txn.Get(ctx, key)
		if privacyMissing(err) {
			extensions[message.SourceID] = true

			continue
		}

		if err != nil {
			return nil, err
		}
	}

	return extensions, nil
}

func capturePrivacyMembership(
	ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation,
) (map[string]bool, map[string]bool, error) {
	fresh, err := captureNewPrivacySources(ctx, txn, mutation)
	if err != nil {
		return nil, nil, err
	}

	extensions, err := capturePrivacyMessageExtensions(ctx, txn, mutation)

	return fresh, extensions, err
}
