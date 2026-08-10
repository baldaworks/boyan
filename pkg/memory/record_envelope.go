package memory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// RecordEnvelope is the deterministic protobuf-wire envelope for a portable
// memory record. Payload encoding remains owned by the corresponding contract.
type RecordEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	RecordType    string `json:"record_type"`
	PayloadSHA256 string `json:"payload_sha256"`
	Payload       []byte `json:"payload"`
}

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

func marshalEnvelope(envelope RecordEnvelope) []byte {
	encoded := make([]byte, 0, len(envelope.Payload)+128)
	encoded = appendProtoBytes(encoded, 1, []byte(envelope.SchemaVersion))
	encoded = appendProtoBytes(encoded, 2, []byte(envelope.RecordType))
	encoded = appendProtoBytes(encoded, 3, []byte(envelope.PayloadSHA256))
	encoded = appendProtoBytes(encoded, 4, envelope.Payload)
	return encoded
}

func appendProtoBytes(dst []byte, field byte, value []byte) []byte {
	dst = append(dst, field<<3|2)
	for length := uint64(len(value)); ; length >>= 7 {
		part := byte(length & 0x7f)
		if length < 0x80 {
			dst = append(dst, part)
			break
		}
		dst = append(dst, part|0x80)
	}
	return append(dst, value...)
}

func readProtoVarint(encoded []byte, offset int) (uint64, int, bool) {
	var value uint64
	for shift := uint(0); offset < len(encoded) && shift < 64; shift += 7 {
		part := encoded[offset]
		offset++
		value |= uint64(part&0x7f) << shift
		if part < 0x80 {
			return value, offset, true
		}
	}
	return 0, offset, false
}

// TupleKey encodes application-owned components without backend-native keys.
func TupleKey(namespace byte, version byte, components ...string) ([]byte, error) {
	key := []byte{namespace, version}
	for _, component := range components {
		if !isCanonicalID(component) {
			return nil, invalidDerived("tuple key component is invalid")
		}
		if len(component) > 65535 {
			return nil, limitExceeded("tuple key component is too large")
		}
		var length [2]byte
		binary.BigEndian.PutUint16(length[:], uint16(len(component)))
		key = append(key, length[:]...)
		key = append(key, component...)
	}
	return key, nil
}
