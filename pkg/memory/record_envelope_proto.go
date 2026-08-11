package memory

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
