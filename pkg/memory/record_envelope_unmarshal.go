package memory

import (
	"crypto/sha256"
	"encoding/hex"
)

// UnmarshalRecordEnvelope validates and decodes the portable protobuf-wire envelope.
func UnmarshalRecordEnvelope(encoded []byte) (RecordEnvelope, error) {
	fields, err := unmarshalEnvelopeFields(encoded)
	if err != nil {
		return RecordEnvelope{}, err
	}

	envelope := RecordEnvelope{
		SchemaVersion: string(fields[envelopeSchemaField]),
		RecordType:    string(fields[envelopeTypeField]),
		PayloadSHA256: string(fields[envelopeChecksumField]),
		Payload:       fields[envelopePayloadField],
	}
	if envelope.SchemaVersion != MemorySchemaVersion || !isCanonicalID(envelope.RecordType) ||
		len(envelope.Payload) == 0 {
		return RecordEnvelope{}, invalidDerived("record envelope is invalid")
	}

	want := sha256.Sum256(envelope.Payload)
	if envelope.PayloadSHA256 != hex.EncodeToString(want[:]) {
		return RecordEnvelope{}, invalidDerived("record envelope checksum does not match payload")
	}

	return envelope, nil
}

func unmarshalEnvelopeFields(encoded []byte) (map[uint64][]byte, error) {
	fields := make(map[uint64][]byte, envelopeFieldCount)

	for offset := 0; offset < len(encoded); {
		tag, next, ok := readProtoVarint(encoded, offset)
		if !ok || !validEnvelopeTag(tag) {
			return nil, invalidDerived("record envelope is malformed")
		}

		length, valueOffset, ok := readProtoVarint(encoded, next)
		if !ok || length > uint64(len(encoded)-valueOffset) {
			return nil, invalidDerived("record envelope payload is malformed")
		}

		field := tag >> protoTagShift
		if _, exists := fields[field]; exists {
			return nil, invalidDerived("record envelope contains duplicate field")
		}

		end := valueOffset + int(length)
		fields[field] = append([]byte(nil), encoded[valueOffset:end]...)
		offset = end
	}

	return fields, nil
}

func validEnvelopeTag(tag uint64) bool {
	return tag>>protoTagShift >= envelopeSchemaField &&
		tag>>protoTagShift <= envelopePayloadField &&
		tag&protoWireTypeMask == protoBytesWireType
}
