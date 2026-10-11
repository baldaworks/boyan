package badger

func isExportableCanonicalRecordType(recordType string) bool {
	return recordType != badgerRecordProjectionManifest &&
		recordType != badgerRecordProjectionActive &&
		recordType != badgerRecordProjectionRetention &&
		recordType != badgerRecordProjectionCheckpoint
}

func canonicalLogicalComponentCount(recordType string) (int, bool) {
	switch recordType {
	case badgerRecordScope, badgerRecordChange:
		return canonicalScopeComponents, recordType == badgerRecordChange
	case badgerRecordProvenanceForward,
		badgerRecordProvenanceReverse,
		badgerRecordSourceRevision,
		badgerRecordProjectionManifest:
		return canonicalEdgeComponents, false
	case badgerRecordOperation,
		badgerRecordSource,
		badgerRecordMessage,
		badgerRecordItem,
		badgerRecordRevision,
		badgerRecordLifecycle,
		badgerRecordHead,
		badgerRecordDelivery,
		badgerRecordDeliveryClaim,
		badgerRecordPayload,
		badgerRecordDeniedSource,
		badgerRecordDeniedRevision,
		badgerRecordProjectionActive,
		badgerRecordProjectionRetention,
		badgerRecordProjectionCheckpoint:
		return canonicalRecordComponents, false
	default:
		return 0, false
	}
}
