package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func mergeMutationSourcePrivacy(
	ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation,
	fresh,
	extensions map[string]bool,
) error {
	if err := validatePrivacyMessageSources(ctx, txn, mutation); err != nil {
		return err
	}

	states := make(map[string]string, len(mutation.Sources))
	for _, revision := range mutation.Revisions {
		refs, complete, err := mutationPrivacyEvidence(ctx, txn, mutation.Scope, revision)
		if err != nil {
			return err
		}

		state := sourcePrivacyUnknown
		if complete || revision.Sensitivity == memory.SensitivitySensitive {
			state = string(revision.Sensitivity)
		}

		for _, evidence := range refs {
			mergePrivacyState(states, evidence.SourceID, state)
		}
	}

	addUnclassifiedPrivacySources(states, mutation)
	invalidatePrivacyExtensions(states, fresh, extensions)

	for sourceID, state := range states {
		if err := mergeOneSourcePrivacy(ctx, txn, mutation.Scope, sourceID, state, fresh[sourceID]); err != nil {
			return err
		}
	}

	return completeMutationPrivacyMembership(ctx, txn, mutation, fresh, extensions)
}

func mergeOneSourcePrivacy(ctx context.Context, txn *Tx, scope memory.Scope, sourceID, state string, fresh bool) error {
	source, found, err := loadPrivacySource(ctx, txn, scope, sourceID)
	if err != nil || !found {
		return err
	}

	state, err = validateStandardPrivacyState(ctx, txn, source, state)
	if err != nil {
		return err
	}

	record, exists, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	record = mergedPrivacyRecord(record, source, state, exists, fresh)

	key, err := privacyKey(scope, SourcePrivacy, sourceID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, key, SourcePrivacy, record)
}

func addUnclassifiedPrivacySources(states map[string]string, mutation memory.CanonicalMutation) {
	for _, source := range mutation.Sources {
		if _, classified := states[source.SourceID]; !classified {
			states[source.SourceID] = sourcePrivacyUnknown
		}
	}

	for _, message := range mutation.Messages {
		if _, classified := states[message.SourceID]; !classified {
			states[message.SourceID] = sourcePrivacyUnknown
		}
	}
}

func invalidatePrivacyExtensions(states map[string]string, fresh, extensions map[string]bool) {
	for sourceID, extended := range extensions {
		if extended && !fresh[sourceID] {
			mergePrivacyState(states, sourceID, sourcePrivacyUnknown)
		}
	}
}

func validateStandardPrivacyState(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord,
	state string) (string, error) {
	if state != sourcePrivacyStandard {
		return state, nil
	}

	incomplete, err := privacyScopeIncomplete(ctx, txn, source.Scope)
	if err != nil {
		return "", err
	}

	if incomplete {
		return sourcePrivacyUnknown, nil
	}

	valid, err := validatePrivacySourcePayload(ctx, txn, source)
	if err != nil {
		return "", err
	}

	if !valid {
		return sourcePrivacyUnknown, nil
	}

	return state, nil
}

func mergedPrivacyRecord(
	record sourcePrivacy,
	source memory.CanonicalSourceRecord,
	state string,
	exists,
	fresh bool,
) sourcePrivacy {
	if !exists {
		record = sourcePrivacy{Version: sourcePrivacyVersion, CoverageVersion: sourcePrivacyVersion,
			Scope: source.Scope, SourceID: source.SourceID, Payload: source.Payload, State: sourcePrivacyUnknown}
		if fresh {
			record.State = state
		}
	}

	if state == sourcePrivacySensitive || source.Sensitivity == memory.SensitivitySensitive {
		record.State = sourcePrivacySensitive
	} else if record.State == sourcePrivacyStandard && state == sourcePrivacyUnknown {
		record.State = sourcePrivacyUnknown
	}

	return record
}

// A dangling message could later be incorporated into a newly created source
// without any source-revision edge to reveal its unclassified history.
func validatePrivacyMessageSources(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	for _, message := range mutation.Messages {
		_, found, err := loadPrivacySource(ctx, txn, mutation.Scope, message.SourceID)
		if err != nil {
			return err
		}

		if !found {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical message source is unavailable", nil)
		}
	}

	return nil
}
