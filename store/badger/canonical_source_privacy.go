package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const (
	badgerRecordSourcePrivacy        = "source-privacy"
	sourcePrivacyVersion      uint32 = 1
	sourcePrivacyStandard            = "standard"
	sourcePrivacySensitive           = "sensitive"
	sourcePrivacyUnknown             = "unknown"
)

// sourcePrivacy is bounded classification authority, separate from immutable
// source identity and recall eligibility. CoverageVersion certifies the writer
// protocol, never the absence of records in a reverse index.
type sourcePrivacy struct {
	Version           uint32            `json:"version"`
	CoverageVersion   uint32            `json:"coverage_version"`
	Scope             memory.Scope      `json:"scope"`
	SourceID          string            `json:"source_id"`
	Payload           memory.PayloadRef `json:"payload"`
	State             string            `json:"state"`
	MessageIDs        []string          `json:"message_ids,omitempty"`
	HasClassification bool              `json:"has_classification,omitempty"`
	Incomplete        bool              `json:"incomplete,omitempty"`
	Preparing         bool              `json:"preparing,omitempty"`
	StickyUnknown     bool              `json:"sticky_unknown,omitempty"`
}

func invalidSourcePrivacy() error {
	return memory.PermanentError(memory.CodeStoreFailure, "canonical source privacy metadata is invalid", nil)
}

func loadSourcePrivacy(txn *badger.Txn, source memory.CanonicalSourceRecord) (sourcePrivacy, bool, error) {
	key, err := canonicalKey(source.Scope, badgerRecordSourcePrivacy, source.SourceID)
	if err != nil {
		return sourcePrivacy{}, false, err
	}

	var record sourcePrivacy

	err = getCanonicalRecord(txn, key, badgerRecordSourcePrivacy, &record)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return sourcePrivacy{}, false, nil
	}

	if err != nil {
		return sourcePrivacy{}, false, invalidSourcePrivacy()
	}

	if !sourcePrivacyBindingsMatch(record, source) || !sourcePrivacyStateValid(record) {
		return sourcePrivacy{}, false, invalidSourcePrivacy()
	}

	return record, true, nil
}

// captureNewPrivacySources runs before immutable sources are inserted, so a
// repeated source envelope cannot turn a later subset into initial authority.
func captureNewPrivacySources(txn *badger.Txn, mutation memory.CanonicalMutation) (map[string]bool, error) {
	fresh := make(map[string]bool, len(mutation.Sources))
	for _, source := range mutation.Sources {
		key, err := canonicalKey(mutation.Scope, badgerRecordSource, source.SourceID)
		if err != nil {
			return nil, err
		}

		_, err = txn.Get(key)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return nil, err
		}

		if errors.Is(err, badger.ErrKeyNotFound) {
			historical, historyErr := hasPrivacySourceHistory(txn, mutation.Scope, source.SourceID)
			if historyErr != nil {
				return nil, historyErr
			}

			fresh[source.SourceID] = !historical
		}
	}

	return fresh, nil
}

// mergeMutationSourcePrivacy visits only mutation sources and evidence. It
// never scans historical revisions, source indexes, or unrelated scope data.
func loadPrivacySource(txn *badger.Txn, scope memory.Scope, sourceID string) (
	memory.CanonicalSourceRecord, bool, error,
) {
	key, err := canonicalKey(scope, badgerRecordSource, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, false, err
	}

	var source memory.CanonicalSourceRecord

	err = getCanonicalRecord(txn, key, badgerRecordSource, &source)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return source, false, nil
	}

	if err != nil {
		return source, false, err
	}

	if source.Validate() != nil || source.Scope != scope || source.SourceID != sourceID {
		return source, false, invalidSourcePrivacy()
	}

	return source, true, nil
}

func validPrivacyState(state string) bool {
	switch state {
	case sourcePrivacyStandard, sourcePrivacySensitive, sourcePrivacyUnknown:
		return true
	default:
		return false
	}
}

// Capture membership additions before immutable writes. A standard subset
// cannot authorize new raw messages appended to an already classified source.
func capturePrivacyMessageExtensions(txn *badger.Txn, mutation memory.CanonicalMutation) (map[string]bool, error) {
	extensions := make(map[string]bool, len(mutation.Messages))
	for _, message := range mutation.Messages {
		key, err := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
		if err != nil {
			return nil, err
		}

		_, err = txn.Get(key)
		if errors.Is(err, badger.ErrKeyNotFound) {
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
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
) (map[string]bool, map[string]bool, error) {
	fresh, err := captureNewPrivacySources(txn, mutation)
	if err != nil {
		return nil, nil, err
	}

	extensions, err := capturePrivacyMessageExtensions(txn, mutation)

	return fresh, extensions, err
}
