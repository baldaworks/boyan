// Package canonicalsql implements bounded canonical SQL persistence primitives.
// Owners must serialize access to the supplied dedicated connection. This package
// does not acquire ownership, reconnect, or implement memory.CanonicalStore.
package canonicalsql

// Family identifies a fixed durable envelope table; arbitrary SQL names are rejected.
type Family string

const (
	// ScopeState identifies the scope envelope family.
	ScopeState Family = "scope"
	// Operation identifies the operation envelope family.
	Operation Family = "operation"
	// Source identifies the source envelope family.
	Source Family = "source"
	// Message identifies the message envelope family.
	Message Family = "message"
	// Item identifies the item envelope family.
	Item Family = "item"
	// Revision identifies the revision envelope family.
	Revision Family = "revision"
	// Lifecycle identifies the lifecycle envelope family.
	Lifecycle Family = "lifecycle"
	// Head identifies the head envelope family.
	Head Family = "head"
	// ScopeChange identifies the change envelope family.
	ScopeChange Family = "change"
	// Delivery identifies the delivery envelope family.
	Delivery Family = "delivery"
	// DeliveryClaim identifies the delivery-claim envelope family.
	DeliveryClaim Family = "delivery-claim"
	// Payload identifies the payload envelope family.
	Payload Family = "payload"
	// ProvenanceForward identifies the provenance-forward envelope family.
	ProvenanceForward Family = "provenance-forward"
	// ProvenanceReverse identifies the provenance-reverse envelope family.
	ProvenanceReverse Family = "provenance-reverse"
	// SourceRevision identifies the source-revision envelope family.
	SourceRevision Family = "source-revision"
	// DeniedSource identifies the denied-source envelope family.
	DeniedSource Family = "denied-source"
	// DeniedRevision identifies the denied-revision envelope family.
	DeniedRevision Family = "denied-revision"
	// ProjectionManifest identifies the projection-manifest envelope family.
	ProjectionManifest Family = "projection-manifest"
	// ProjectionActive identifies the projection-active envelope family.
	ProjectionActive Family = "projection-active"
	// ProjectionRetention identifies the projection-retention envelope family.
	ProjectionRetention Family = "projection-retention"
	// ProjectionCheckpoint identifies the projection-checkpoint envelope family.
	ProjectionCheckpoint Family = "projection-checkpoint"
	// ForgetOutcome identifies the forget-outcome envelope family.
	ForgetOutcome Family = "forget-outcome"
	// IngestSequence identifies the ingest-sequence envelope family.
	IngestSequence Family = "ingest-sequence"
	// IngestCursor identifies the ingest-cursor envelope family.
	IngestCursor Family = "ingest-cursor"
	// TurnCertificate identifies the turn-certificate envelope family.
	TurnCertificate Family = "turn-certificate"
	// BoundaryCertificate identifies the boundary-certificate envelope family.
	BoundaryCertificate Family = "boundary-certificate"
	// BoundaryCapability identifies the boundary-capability envelope family.
	BoundaryCapability Family = "boundary-capability"
	// SourcePrivacy identifies the source-privacy envelope family.
	SourcePrivacy Family = "source-privacy"
	// SourcePrivacyScope identifies the source-privacy-scope envelope family.
	SourcePrivacyScope Family = "source-privacy-scope"
	// EraseJournal identifies the erase-journal envelope family.
	EraseJournal Family = "erase-journal"
	// EraseFence identifies the erase-fence envelope family.
	EraseFence Family = "erase-fence"
	// EraseSourceIdentity identifies the erase-source-identity envelope family.
	EraseSourceIdentity Family = "erase-source-identity"
	// SourcePrivacyReady identifies the source-privacy-ready envelope family.
	SourcePrivacyReady Family = "source-privacy-ready"
	// EraseGuard identifies the erase-guard envelope family.
	EraseGuard Family = "erase-guard"
	// EraseSourceRef identifies the erase-source-ref envelope family.
	EraseSourceRef Family = "erase-source-ref"
	// EraseLockRef identifies the erase-lock-ref envelope family.
	EraseLockRef Family = "erase-lock-ref"
	// EraseBlobLock identifies the erase-blob-lock envelope family.
	EraseBlobLock Family = "erase-blob-lock"
	// ErasedBlob identifies the erased-blob envelope family.
	ErasedBlob Family = "erased-blob"
)

type familyDef struct {
	table  string
	scoped bool
	parts  []string
}

//nolint:cyclop,gocyclo,funlen // Exhaustive fixed table dispatch, not a pluggable schema registry.
func familyDefinition(f Family) (familyDef, error) {
	switch f {
	case ScopeState:
		return familyDef{table: "scope_state", scoped: true, parts: []string{}}, nil
	case Operation:
		return familyDef{table: "operation", scoped: true, parts: []string{"id"}}, nil
	case Source:
		return familyDef{table: "source", scoped: true, parts: []string{"id"}}, nil
	case Message:
		return familyDef{table: "message", scoped: true, parts: []string{"id"}}, nil
	case Item:
		return familyDef{table: "item", scoped: true, parts: []string{"id"}}, nil
	case Revision:
		return familyDef{table: "revision", scoped: true, parts: []string{"id"}}, nil
	case Lifecycle:
		return familyDef{table: "lifecycle", scoped: true, parts: []string{"id"}}, nil
	case Head:
		return familyDef{table: "head", scoped: true, parts: []string{"id"}}, nil
	case ScopeChange:
		return familyDef{table: "scope_change", scoped: true, parts: []string{"sequence_u64"}}, nil
	case Delivery:
		return familyDef{table: "delivery", scoped: true, parts: []string{"id"}}, nil
	case DeliveryClaim:
		return familyDef{table: "delivery_claim", scoped: true, parts: []string{"id"}}, nil
	case Payload:
		return familyDef{table: "payload", scoped: false, parts: []string{"id"}}, nil
	case ProvenanceForward:
		return familyDef{table: "provenance_forward", scoped: true, parts: []string{"from_id", "to_id"}}, nil
	case ProvenanceReverse:
		return familyDef{table: "provenance_reverse", scoped: true, parts: []string{"from_id", "to_id"}}, nil
	case SourceRevision:
		return familyDef{table: "source_revision", scoped: true, parts: []string{"from_id", "to_id"}}, nil
	case DeniedSource:
		return familyDef{table: "denied_source", scoped: true, parts: []string{"id"}}, nil
	case DeniedRevision:
		return familyDef{table: "denied_revision", scoped: true, parts: []string{"id"}}, nil
	case ProjectionManifest:
		return familyDef{table: "projection_manifest", scoped: true, parts: []string{"projection_id", "generation_id"}}, nil
	case ProjectionActive:
		return familyDef{table: "projection_active", scoped: true, parts: []string{"projection_id"}}, nil
	case ProjectionRetention:
		return familyDef{table: "projection_retention", scoped: true, parts: []string{"projection_id"}}, nil
	case ProjectionCheckpoint:
		return familyDef{table: "projection_checkpoint", scoped: true, parts: []string{"kind"}}, nil
	case ForgetOutcome:
		return familyDef{table: "forget_outcome", scoped: true, parts: []string{"kind", "id"}}, nil
	case IngestSequence:
		return familyDef{table: "ingest_sequence", scoped: true, parts: []string{"producer_id", "sequence_u64"}}, nil
	case IngestCursor:
		return familyDef{table: "ingest_cursor", scoped: true, parts: []string{"producer_id"}}, nil
	case TurnCertificate:
		return familyDef{table: "turn_certificate", scoped: true, parts: []string{"id"}}, nil
	case BoundaryCertificate:
		return familyDef{table: "boundary_certificate", scoped: true, parts: []string{"id"}}, nil
	case BoundaryCapability:
		return familyDef{table: "boundary_capability", scoped: true, parts: []string{"id"}}, nil
	case SourcePrivacy:
		return familyDef{table: "source_privacy", scoped: true, parts: []string{"id"}}, nil
	case SourcePrivacyScope:
		return familyDef{table: "source_privacy_scope", scoped: true, parts: []string{"id"}}, nil
	case EraseJournal:
		return familyDef{table: "erase_journal", scoped: true, parts: []string{"id"}}, nil
	case EraseFence:
		return familyDef{table: "erase_fence", scoped: true, parts: []string{"id"}}, nil
	case EraseSourceIdentity:
		return familyDef{table: "erase_source_identity", scoped: true, parts: []string{"id"}}, nil
	case SourcePrivacyReady:
		return familyDef{table: "source_privacy_ready", scoped: false, parts: []string{}}, nil
	case EraseGuard:
		return familyDef{table: "erase_guard", scoped: false, parts: []string{}}, nil
	case EraseSourceRef:
		return familyDef{table: "erase_source_ref", scoped: true, parts: []string{"operation_id", "id"}}, nil
	case EraseLockRef:
		return familyDef{table: "erase_lock_ref", scoped: true, parts: []string{"operation_id", "id"}}, nil
	case EraseBlobLock:
		return familyDef{table: "erase_blob_lock", scoped: false, parts: []string{"id"}}, nil
	case ErasedBlob:
		return familyDef{table: "erased_blob", scoped: false, parts: []string{"id"}}, nil
	default:
		return familyDef{}, invalid("unknown canonical row family")
	}
}

func (d familyDef) columns() []string {
	var cols []string
	if d.scoped {
		cols = append(cols, "scope_key", "scope_kind")
	}

	cols = append(cols, d.parts...)
	if len(cols) == 0 {
		cols = append(cols, "singleton")
	}

	return cols
}
