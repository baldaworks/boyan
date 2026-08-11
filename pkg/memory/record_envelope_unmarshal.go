package memory

import (
	"crypto/sha256"
	"encoding/hex"
)

// UnmarshalRecordEnvelope validates and decodes the portable protobuf-wire envelope.
func UnmarshalRecordEnvelope(encoded []byte) (RecordEnvelope, error) {
	fields := make(map[uint64][]byte, 4)
	for offset := 0; offset < len(encoded); {
		tag, next, ok := readProtoVarint(encoded, offset)
		if !ok || tag>>3 < 1 || tag>>3 > 4 || tag&7 != 2 {
			return RecordEnvelope{}, invalidDerived("record envelope is malformed")
		}
		length, valueOffset, ok := readProtoVarint(encoded, next)
		if !ok || length > uint64(len(encoded)-valueOffset) {
			return RecordEnvelope{}, invalidDerived("record envelope payload is malformed")
		}
		field := tag >> 3
		if _, exists := fields[field]; exists {
			return RecordEnvelope{}, invalidDerived("record envelope contains duplicate field")
		}
		end := valueOffset + int(length)
		fields[field] = append([]byte(nil), encoded[valueOffset:end]...)
		offset = end
	}
	envelope := RecordEnvelope{
		SchemaVersion: string(fields[1]),
		RecordType:    string(fields[2]),
		PayloadSHA256: string(fields[3]),
		Payload:       fields[4],
	}
	if envelope.SchemaVersion != MemorySchemaVersion || !isCanonicalID(envelope.RecordType) || len(envelope.Payload) == 0 {
		return RecordEnvelope{}, invalidDerived("record envelope is invalid")
	}
	want := sha256.Sum256(envelope.Payload)
	if envelope.PayloadSHA256 != hex.EncodeToString(want[:]) {
		return RecordEnvelope{}, invalidDerived("record envelope checksum does not match payload")
	}
	return envelope, nil
}
