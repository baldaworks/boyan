package memory

import "encoding/binary"

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
