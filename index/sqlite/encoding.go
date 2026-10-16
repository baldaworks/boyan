package sqlite

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	unsignedBytes  = 8
	timestampBytes = 17
	signedOrderBit = uint64(1) << 63
)

func encodeUint64(value uint64) []byte {
	result := make([]byte, unsignedBytes)
	binary.BigEndian.PutUint64(result, value)

	return result
}

func decodeUint64(value []byte) (uint64, error) {
	if len(value) != unsignedBytes {
		return 0, fmt.Errorf("%w: unsigned integer", ErrCorrupt)
	}

	return binary.BigEndian.Uint64(value), nil
}

// encodeTime orders UTC calendar components, including the signed year. It
// preserves nanoseconds without UnixNano's restricted 1678..2262 range or
// RFC3339's four-digit year restriction. Monotonic clock data is not persisted.
func encodeTime(value time.Time) []byte {
	value = value.UTC()
	result := make([]byte, timestampBytes)
	binary.BigEndian.PutUint64(result, uint64(value.Year())^signedOrderBit)
	result[8], result[9] = byte(value.Month()), byte(value.Day())
	result[10], result[11], result[12] = byte(value.Hour()), byte(value.Minute()), byte(value.Second())
	binary.BigEndian.PutUint32(result[13:], uint32(value.Nanosecond()))

	return result
}

func decodeTime(value []byte) (time.Time, error) {
	if len(value) != timestampBytes {
		return time.Time{}, fmt.Errorf("%w: timestamp length", ErrCorrupt)
	}

	year := int64(binary.BigEndian.Uint64(value) ^ signedOrderBit)

	result := time.Date(int(year), time.Month(value[8]), int(value[9]),
		int(value[10]), int(value[11]), int(value[12]), int(binary.BigEndian.Uint32(value[13:])), time.UTC)
	if !bytes.Equal(value, encodeTime(result)) {
		return time.Time{}, fmt.Errorf("%w: timestamp", ErrCorrupt)
	}

	return result, nil
}
