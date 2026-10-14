package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func sourcePrivacyBindingsMatch(record sourcePrivacy, source memory.CanonicalSourceRecord) bool {
	return record.Version == sourcePrivacyVersion && record.CoverageVersion == sourcePrivacyVersion &&
		record.Scope == source.Scope && record.SourceID == source.SourceID && record.Payload == source.Payload
}

func sourcePrivacyStateValid(record sourcePrivacy) bool {
	if !validPrivacyState(record.State) || !validPrivacyMembership(record.MessageIDs) {
		return false
	}

	if !record.Preparing && (record.HasClassification || record.Incomplete) {
		return false
	}

	if record.State == sourcePrivacyStandard && (record.Preparing || record.StickyUnknown) {
		return false
	}

	return true
}
