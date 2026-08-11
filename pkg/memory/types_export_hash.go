package memory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

func stableExportID(kind string, parts ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(SchemaVersionV1))
	writeHashPart(hash, kind)
	for _, part := range parts {
		writeHashPart(hash, part)
	}
	return fmt.Sprintf("boyan:v1:%s:%s", kind, hex.EncodeToString(hash.Sum(nil)))
}

type hashWriter interface {
	Write(data []byte) (int, error)
}

func writeHashPart(dst hashWriter, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}
