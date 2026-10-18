package canonicalsql

import (
	"encoding/json"
	"math"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func isCanonicalProofRecord(recordType string) bool {
	return recordType == string(BoundaryCertificate) || recordType == string(BoundaryCapability) ||
		recordType == string(Revision)
}

// validateCanonicalProofRecord validates bounded structural metadata only.
// Legacy revision bodies remain opaque when no inherited trust is asserted.
func validateCanonicalProofRecord(components []string, envelope memory.RecordEnvelope) error {
	if len(components) == 0 || !isCanonicalProofRecord(components[0]) {
		return validateCanonicalPrivacyRecord(components, envelope)
	}

	if len(components) != canonicalRecordComponents || envelope.RecordType != components[0] {
		return invalidCanonicalProof()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if err := scope.Validate(); err != nil {
		return invalidCanonicalProof()
	}

	switch components[0] {
	case string(BoundaryCertificate):
		return validateBoundaryCertificatePayload(envelope.Payload, components[3])
	case string(BoundaryCapability):
		return validateBoundaryCapabilityPayload(envelope.Payload, scope, components[3])
	default:
		return validateInheritedRevisionPayload(envelope.Payload, components[3])
	}
}

func validateBoundaryCertificatePayload(payload []byte, id string) error {
	var certificate memory.CanonicalBoundaryCertificate
	if len(payload) > 512 || decodeCanonicalProofJSON(payload, &certificate) != nil {
		return invalidCanonicalProof()
	}

	if certificate.Validate() != nil || certificate.ExportID != id {
		return invalidCanonicalProof()
	}

	return nil
}

func validateBoundaryCapabilityPayload(payload []byte, scope memory.Scope, id string) error {
	var marker boundaryCapability
	// TupleKey limits the scope component to uint16 bytes. JSON escaping can
	// expand each byte sixfold; keep all valid existing scope locators supported.
	const maxCapabilityBytes = 6*math.MaxUint16 + 256
	if len(payload) > maxCapabilityBytes || decodeCanonicalProofJSON(payload, &marker) != nil {
		return invalidCanonicalProof()
	}

	if marker.Version != 1 || marker.Scope != scope || id != boundaryCapabilityID {
		return invalidCanonicalProof()
	}

	return nil
}

func validateInheritedRevisionPayload(payload []byte, id string) error {
	if len(payload) > MaxEnvelopeBytes {
		return invalidCanonicalProof()
	}

	var revision memory.MemoryRevision
	if json.Unmarshal(payload, &revision) != nil {
		return invalidCanonicalProof()
	}

	if revision.GroundingBoundsVersion == 0 && len(revision.GroundingParents) == 0 {
		return nil
	}

	if revision.RevisionID != id || revision.Validate() != nil {
		return invalidCanonicalProof()
	}

	if _, err := memory.IntersectGroundingBounds([]memory.Temporal{revision.Temporal}); err != nil {
		return invalidCanonicalProof()
	}

	return nil
}

func invalidCanonicalProof() error {
	return memory.PermanentError(memory.CodeInvalidDerived, "canonical structural proof metadata is invalid", nil)
}
