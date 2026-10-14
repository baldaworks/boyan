package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func mergeMutationSourcePrivacy(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	fresh,
	extensions map[string]bool,
) error {
	if err := validatePrivacyMessageSources(txn, mutation); err != nil {
		return err
	}

	states := make(map[string]string, len(mutation.Sources))
	for _, revision := range mutation.Revisions {
		refs, complete, err := mutationPrivacyEvidence(txn, mutation.Scope, revision)
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
		if err := mergeOneSourcePrivacy(txn, mutation.Scope, sourceID, state, fresh[sourceID]); err != nil {
			return err
		}
	}

	return completeMutationPrivacyMembership(txn, mutation, fresh, extensions)
}

func mergeOneSourcePrivacy(txn *badger.Txn, scope memory.Scope, sourceID, state string, fresh bool) error {
	source, found, err := loadPrivacySource(txn, scope, sourceID)
	if err != nil || !found {
		return err
	}

	state, err = validateStandardPrivacyState(txn, source, state)
	if err != nil {
		return err
	}

	record, exists, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	record = mergedPrivacyRecord(record, source, state, exists, fresh)

	key, err := canonicalKey(scope, badgerRecordSourcePrivacy, sourceID)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordSourcePrivacy, record)
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

func validateStandardPrivacyState(txn *badger.Txn, source memory.CanonicalSourceRecord, state string) (string, error) {
	if state != sourcePrivacyStandard {
		return state, nil
	}

	incomplete, err := privacyScopeIncomplete(txn, source.Scope)
	if err != nil {
		return "", err
	}

	if incomplete {
		return sourcePrivacyUnknown, nil
	}

	valid, err := validatePrivacySourcePayload(txn, source)
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
func validatePrivacyMessageSources(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	for _, message := range mutation.Messages {
		_, found, err := loadPrivacySource(txn, mutation.Scope, message.SourceID)
		if err != nil {
			return err
		}

		if !found {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical message source is unavailable", nil)
		}
	}

	return nil
}
