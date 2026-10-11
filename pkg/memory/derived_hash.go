package memory

import (
	"crypto/sha256"
	"encoding/hex"
)

const derivedIDPrefix = "boyan-derived:v1:"

func derivedStableID(kind string, parts ...string) string {
	hash := sha256.New()
	writeHashPart(hash, DerivedSchemaVersionV1)
	writeHashPart(hash, kind)

	for _, part := range parts {
		writeHashPart(hash, part)
	}

	return derivedIDPrefix + kind + ":" + hex.EncodeToString(hash.Sum(nil))
}
