package sqlite

import (
	"bytes"
	"math"
	"testing"
	"time"
)

func TestUnsignedEncodingPreservesEntireDomain(t *testing.T) {
	t.Parallel()

	values := []uint64{0, 1, math.MaxInt64, uint64(math.MaxInt64) + 1, math.MaxUint64}

	var previous []byte

	for _, value := range values {
		encoded := encodeUint64(value)

		decoded, err := decodeUint64(encoded)
		if err != nil || decoded != value || len(encoded) != 8 {
			t.Fatalf("roundtrip %d: %x %d %v", value, encoded, decoded, err)
		}

		if previous != nil && bytes.Compare(previous, encoded) >= 0 {
			t.Fatal("unsigned order lost")
		}

		previous = encoded
	}

	if _, err := decodeUint64([]byte{1}); err == nil {
		t.Fatal("malformed unsigned value accepted")
	}
}

func TestTimestampEncodingPreservesCalendarRangeAndNanoseconds(t *testing.T) {
	t.Parallel()

	values := []time.Time{
		time.Date(-12000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(-1, 12, 31, 23, 59, 59, 999999999, time.UTC),
		time.Time{}, time.Date(1677, 1, 1, 0, 0, 0, 1, time.UTC),
		time.Date(2263, 1, 1, 0, 0, 0, 1, time.UTC),
		time.Date(2263, 1, 1, 0, 0, 0, 2, time.UTC),
		time.Date(12000, 1, 1, 0, 0, 0, 1, time.UTC),
	}

	var previous []byte

	for _, value := range values {
		encoded := encodeTime(value)

		decoded, err := decodeTime(encoded)
		if err != nil || !decoded.Equal(value) {
			t.Fatalf("roundtrip %v: %v %v", value, decoded, err)
		}

		if previous != nil && bytes.Compare(previous, encoded) >= 0 {
			t.Fatal("timestamp order lost")
		}

		previous = encoded
	}

	local := values[4].In(time.FixedZone("offset", 3600))
	if !bytes.Equal(encodeTime(local), encodeTime(values[4])) {
		t.Fatal("encoding depends on timezone")
	}

	bad := encodeTime(values[4])

	bad[8] = 13
	if _, err := decodeTime(bad); err == nil {
		t.Fatal("malformed date accepted")
	}
}
