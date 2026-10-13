package badger

import (
	"bytes"
	"encoding/json"
	"io"
	"math"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func isCanonicalProofRecord(recordType string) bool {
	return recordType == badgerRecordBoundaryCertificate || recordType == badgerRecordBoundaryCapability ||
		recordType == badgerRecordRevision
}

// validateCanonicalProofRecord validates bounded structural metadata only.
// Legacy revision bodies remain opaque when no inherited trust is asserted.
func validateCanonicalProofRecord(components []string, envelope memory.RecordEnvelope) error {
	if len(components) == 0 || !isCanonicalProofRecord(components[0]) {
		return nil
	}

	if len(components) != canonicalRecordComponents || envelope.RecordType != components[0] {
		return invalidCanonicalProof()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if err := scope.Validate(); err != nil {
		return invalidCanonicalProof()
	}

	switch components[0] {
	case badgerRecordBoundaryCertificate:
		return validateBoundaryCertificatePayload(envelope.Payload, components[3])
	case badgerRecordBoundaryCapability:
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
	if len(payload) > maxCanonicalLogicalLineBytes {
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

func decodeCanonicalProofJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalidCanonicalProof()
	}

	return nil
}

func invalidCanonicalProof() error {
	return memory.PermanentError(memory.CodeInvalidDerived, "canonical structural proof metadata is invalid", nil)
}

func validateStoredCanonicalProof(key []byte, envelope memory.RecordEnvelope) error {
	selected := isCanonicalProofRecord(envelope.RecordType)

	for _, kind := range []string{badgerRecordBoundaryCertificate, badgerRecordBoundaryCapability, badgerRecordRevision} {
		prefix, _ := memory.TupleKey(canonicalNamespace, canonicalVersion, kind)
		selected = selected || bytes.HasPrefix(key, prefix)
	}

	if !selected {
		return nil
	}

	components, _, err := decodeCanonicalLogicalKey(key)
	if err != nil || len(components) == 0 || !isCanonicalProofRecord(components[0]) {
		return invalidCanonicalProof()
	}

	return validateCanonicalProofRecord(components, envelope)
}
