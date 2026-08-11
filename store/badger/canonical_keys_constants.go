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
