package canonicalsql

import (
	"encoding/binary"
	"math"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Key holds separate binary tuple components. Parts follow the fixed family
// schema; ordered uint64 parts must be encoded with Uint64.
type Key struct {
	Family Family
	Scope  memory.Scope
	Parts  [][]byte
}

const unsignedBytes = 8

// Uint64 preserves the entire unsigned domain under SQL binary comparison.
func Uint64(value uint64) []byte {
	encoded := make([]byte, unsignedBytes)
	binary.BigEndian.PutUint64(encoded, value)

	return encoded
}

// DecodeUint64 rejects malformed SQL values rather than padding or truncating.
func DecodeUint64(encoded []byte) (uint64, error) {
	if len(encoded) != unsignedBytes {
		return 0, invalid("invalid canonical unsigned column")
	}

	return binary.BigEndian.Uint64(encoded), nil
}

// NextUint64 refuses to wrap a durable sequence.
func NextUint64(value uint64) (uint64, error) {
	if value == math.MaxUint64 {
		return 0, memory.PermanentError(memory.CodeLimitExceeded, "canonical sequence exhausted", nil)
	}

	return value + 1, nil
}

func (k Key) values() (familyDef, [][]byte, error) {
	d, err := familyDefinition(k.Family)
	if err != nil {
		return d, nil, err
	}

	if len(k.Parts) != len(d.parts) {
		return d, nil, invalid("invalid canonical tuple arity")
	}

	var values [][]byte

	if d.scoped {
		if err := k.Scope.Validate(); err != nil {
			return d, nil, err
		}

		if len(k.Scope.Key) > math.MaxUint16 {
			return d, nil, limitError()
		}

		values = append(values, []byte(k.Scope.Key), []byte(k.Scope.Kind))
	} else if k.Scope != (memory.Scope{}) {
		return d, nil, invalid("global canonical key cannot carry scope")
	}

	for i, part := range k.Parts {
		if err := validatePart(d.parts[i], part); err != nil {
			return d, nil, err
		}

		values = append(values, part)
	}

	if len(values) == 0 {
		values = append(values, []byte{})
	}

	return d, values, nil
}

func validatePart(column string, part []byte) error {
	if strings.HasSuffix(column, "_u64") {
		_, err := DecodeUint64(part)

		return err
	}

	if len(part) == 0 {
		return invalid("empty canonical tuple component")
	}

	if len(part) > math.MaxUint16 {
		return limitError()
	}

	return nil
}

// stampKey is tuple identity framing only. It is never used for ordered row scans.
func stampKey(values [][]byte) []byte {
	result := []byte{1}
	for _, value := range values {
		result = binary.BigEndian.AppendUint32(result, uint32(len(value)))
		result = append(result, value...)
	}

	return result
}

const frameLengthBytes = 4

func keyFromFrame(f Family, frame []byte) (Key, error) {
	def, err := familyDefinition(f)
	if err != nil || len(frame) == 0 || frame[0] != 1 {
		return Key{}, corruptRow()
	}

	remaining := frame[1:]

	values := make([][]byte, 0, len(def.columns()))
	for range def.columns() {
		if len(remaining) < frameLengthBytes {
			return Key{}, corruptRow()
		}

		size := uint64(binary.BigEndian.Uint32(remaining))

		remaining = remaining[frameLengthBytes:]
		if size > uint64(len(remaining)) {
			return Key{}, corruptRow()
		}

		values = append(values, remaining[:size])
		remaining = remaining[size:]
	}

	if len(remaining) != 0 {
		return Key{}, corruptRow()
	}

	return keyFromValues(f, def, values)
}
