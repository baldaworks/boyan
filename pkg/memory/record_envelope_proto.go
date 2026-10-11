package memory

const (
	envelopeMetadataCapacity = 128
	envelopeSchemaField      = 1
	envelopeTypeField        = 2
	envelopeChecksumField    = 3
	envelopePayloadField     = 4
	envelopeFieldCount       = 4
	protoTagShift            = 3
	protoWireTypeMask        = (1 << protoTagShift) - 1
	protoBytesWireType       = 2
	protoVarintBits          = 7
	protoVarintMask          = 0x7f
	protoVarintContinuation  = 0x80
)

func marshalEnvelope(envelope RecordEnvelope) []byte {
	encoded := make([]byte, 0, len(envelope.Payload)+envelopeMetadataCapacity)
	encoded = appendProtoBytes(encoded, envelopeSchemaField, []byte(envelope.SchemaVersion))
	encoded = appendProtoBytes(encoded, envelopeTypeField, []byte(envelope.RecordType))
	encoded = appendProtoBytes(encoded, envelopeChecksumField, []byte(envelope.PayloadSHA256))
	encoded = appendProtoBytes(encoded, envelopePayloadField, envelope.Payload)

	return encoded
}

func appendProtoBytes(dst []byte, field byte, value []byte) []byte {
	dst = append(dst, field<<protoTagShift|protoBytesWireType)

	for length := uint64(len(value)); ; length >>= protoVarintBits {
		part := byte(length & protoVarintMask)
		if length < protoVarintContinuation {
			dst = append(dst, part)

			break
		}

		dst = append(dst, part|protoVarintContinuation)
	}

	return append(dst, value...)
}

func readProtoVarint(encoded []byte, offset int) (uint64, int, bool) {
	var value uint64

	for shift := uint(0); offset < len(encoded) && shift < 64; shift += protoVarintBits {
		part := encoded[offset]
		offset++

		value |= uint64(part&protoVarintMask) << shift
		if part < protoVarintContinuation {
			return value, offset, true
		}
	}

	return 0, offset, false
}
