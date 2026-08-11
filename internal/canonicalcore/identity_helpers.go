package canonicalcore

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func reconciliationID(kind string, parts ...string) string {
	hash := sha256.New()
	writeHashPart(hash, memory.MemorySchemaVersion)
	writeHashPart(hash, kind)
	for _, part := range parts {
		writeHashPart(hash, part)
	}
	return memory.StableID(kind, hash.Sum(nil))
}

func writeHashPart(dst hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}
