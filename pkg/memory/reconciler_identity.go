package memory

import (
	"crypto/sha256"
	"strings"
)

func sameEventEvidence(left, right []EvidenceRef) bool {
	for _, a := range left {
		for _, b := range right {
			if a.SourceID == b.SourceID && a.MessageID == b.MessageID {
				return true
			}
		}
	}
	return false
}

func canonicalSemanticPart(value string) (string, error) {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	if value == "" || len(value) > maxMemoryTextBytes {
		return "", invalidDerived("semantic candidate part is invalid")
	}
	return value, nil
}

func reconciliationID(kind string, parts ...string) string {
	hash := sha256.New()
	writeHashPart(hash, MemorySchemaVersion)
	writeHashPart(hash, kind)
	for _, part := range parts {
		writeHashPart(hash, part)
	}
	return StableID(kind, hash.Sum(nil))
}
