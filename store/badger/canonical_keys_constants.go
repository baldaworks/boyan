package badger

const (
	canonicalNamespace               byte = 0x53
	canonicalVersion                 byte = 1
	badgerRecordScope                     = "scope"
	badgerRecordOperation                 = "operation"
	badgerRecordSource                    = "source"
	badgerRecordMessage                   = "message"
	badgerRecordItem                      = "item"
	badgerRecordRevision                  = "revision"
	badgerRecordLifecycle                 = "lifecycle"
	badgerRecordHead                      = "head"
	badgerRecordChange                    = "change"
	badgerRecordDelivery                  = "delivery"
	badgerRecordDeliveryClaim             = "delivery-claim"
	badgerRecordPayload                   = "payload"
	badgerRecordProvenanceForward         = "provenance-forward"
	badgerRecordProvenanceReverse         = "provenance-reverse"
	badgerRecordSourceRevision            = "source-revision"
	badgerRecordDeniedSource              = "denied-source"
	badgerRecordDeniedRevision            = "denied-revision"
	badgerRecordProjectionManifest        = "projection-manifest"
	badgerRecordProjectionActive          = "projection-active"
	badgerRecordProjectionRetention       = "projection-retention"
	badgerRecordProjectionCheckpoint      = "projection-checkpoint"
	badgerRecordForgetOutcome             = "forget-outcome"
	badgerRecordIngestSequence            = "ingest-sequence"
	badgerRecordIngestCursor              = "ingest-cursor"
)

const (
	maxCanonicalLogicalBatchSize             = 4096
	maxCanonicalRestorePendingWrites         = 4096
	maxCanonicalForgetScrubIDs               = 4096
	canonicalSequenceBytes                   = 8
	canonicalTupleLengthBytes                = 2
	canonicalScopeComponents                 = 3
	canonicalRecordComponents                = 4
	canonicalEdgeComponents                  = 5
	canonicalLogicalScannerBufferBytes       = 64 * 1024
	maxCanonicalTraceSourceMessages          = 32
	messageRoleOrderOther                    = 2
	provenanceVisiting                 uint8 = 1
	provenanceVisited                  uint8 = 2
)
