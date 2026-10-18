package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	sourcePrivacyVersion   uint32 = 1
	sourcePrivacyStandard         = "standard"
	sourcePrivacySensitive        = "sensitive"
	sourcePrivacyUnknown          = "unknown"
)

// sourcePrivacy is bounded classification authority, separate from immutable
// source identity and recall eligibility. CoverageVersion certifies the writer
// protocol, never the absence of records in a reverse index.
type sourcePrivacy struct {
	Version                   uint32            `json:"version"`
	CoverageVersion           uint32            `json:"coverage_version"`
	Scope                     memory.Scope      `json:"scope"`
	SourceID                  string            `json:"source_id"`
	Payload                   memory.PayloadRef `json:"payload"`
	State                     string            `json:"state"`
	MessageIDs                []string          `json:"message_ids,omitempty"`
	MessageMembershipComplete bool              `json:"message_membership_complete,omitempty"`
	HasClassification         bool              `json:"has_classification,omitempty"`
	Incomplete                bool              `json:"incomplete,omitempty"`
	Preparing                 bool              `json:"preparing,omitempty"`
	StickyUnknown             bool              `json:"sticky_unknown,omitempty"`
}

func invalidSourcePrivacy() error {
	return memory.PermanentError(memory.CodeStoreFailure, "canonical source privacy metadata is invalid", nil)
}

func loadSourcePrivacy(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord) (sourcePrivacy, bool, error) {
	key, err := privacyKey(source.Scope, SourcePrivacy, source.SourceID)
	if err != nil {
		return sourcePrivacy{}, false, err
	}

	var record sourcePrivacy

	err = getPrivacyRecord(ctx, txn, key, SourcePrivacy, &record)
	if privacyMissing(err) {
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

func loadPrivacySource(ctx context.Context, txn *Tx, scope memory.Scope, sourceID string) (
	memory.CanonicalSourceRecord, bool, error,
) {
	key, err := privacyKey(scope, Source, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, false, err
	}

	var source memory.CanonicalSourceRecord

	err = getPrivacyRecord(ctx, txn, key, Source, &source)
	if privacyMissing(err) {
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
