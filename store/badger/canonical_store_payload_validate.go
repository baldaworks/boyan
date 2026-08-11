package badger

import (
	"crypto/sha256"
	"encoding/hex"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func payloadRecordKey(ref memory.PayloadRef) ([]byte, error) {
	return canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
}

func isBadgerPayloadValid(data []byte, ref memory.PayloadRef) bool {
	if ref.Validate() != nil || len(data) == 0 || len(data) != int(ref.ByteSize) {
		return false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == ref.Digest
}
