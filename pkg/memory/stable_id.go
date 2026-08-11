package memory

import "encoding/hex"

const stableIDNamespace = "boyan-memory"

// StableID formats a deterministic Boyan identifier from a hashed payload.
func StableID(kind string, digest []byte) string {
	return stableIDNamespace + ":" + kind + ":" + hex.EncodeToString(digest)
}
