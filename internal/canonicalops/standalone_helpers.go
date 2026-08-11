package canonicalops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func stableCanonicalFingerprint(kind string, value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return stableCanonicalID(kind, "marshal-error")
	}
	return stableCanonicalID(kind, string(data))
}

func stableCanonicalID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(part))
	}
	return "boyan-canonicalops:" + hex.EncodeToString(hash.Sum(nil))
}

func derivedMemoryKey(itemID string) memory.MemoryKey {
	return memory.MemoryKey("derived:" + itemID)
}
