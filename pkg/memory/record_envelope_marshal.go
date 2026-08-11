package memory

import (
	"crypto/sha256"
	"encoding/hex"
)

// MarshalRecordEnvelope returns a deterministic protobuf-wire envelope.
func MarshalRecordEnvelope(recordType string, payload []byte) ([]byte, error) {
	if !isCanonicalID(recordType) || len(payload) == 0 {
		return nil, invalidDerived("record envelope is invalid")
	}
	digest := sha256.Sum256(payload)
	envelope := RecordEnvelope{
		SchemaVersion: MemorySchemaVersion,
		RecordType:    recordType,
		PayloadSHA256: hex.EncodeToString(digest[:]),
		Payload:       payload,
	}
	return marshalEnvelope(envelope), nil
}
