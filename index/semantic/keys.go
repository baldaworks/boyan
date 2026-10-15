package semantic

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/baldaworks/boyan/pkg/memory"
)

const sha256DigestBytes = sha256.Size

const (
	keyNamespace       byte = 0x56
	keyVersion         byte = 1
	keyActive          byte = 1
	keyState           byte = 2
	keyChunk           byte = 3
	keyLegacyUsedBuild byte = 4
	keyIndexSize            = 4
)

func scopeDigest(scope memory.Scope) [32]byte {
	return sha256.Sum256([]byte(scope.Key + "\x00" + string(scope.Kind)))
}

func buildDigest(buildID string) [32]byte {
	return sha256.Sum256([]byte(buildID))
}

func activeKey(scope memory.Scope) []byte {
	scopeHash := scopeDigest(scope)
	key := []byte{keyNamespace, keyVersion, keyActive}

	return append(key, scopeHash[:]...)
}

func activePrefix() []byte {
	return []byte{keyNamespace, keyVersion, keyActive}
}

func stateKey(scope memory.Scope, buildID string) []byte {
	scopeHash := scopeDigest(scope)
	buildHash := buildDigest(buildID)
	key := []byte{keyNamespace, keyVersion, keyState}
	key = append(key, scopeHash[:]...)

	return append(key, buildHash[:]...)
}

func statePrefix() []byte {
	return []byte{keyNamespace, keyVersion, keyState}
}

func legacyUsedBuildKey(scope memory.Scope, buildID string) []byte {
	scopeHash := scopeDigest(scope)
	buildHash := buildDigest(buildID)
	key := []byte{keyNamespace, keyVersion, keyLegacyUsedBuild}
	key = append(key, scopeHash[:]...)

	return append(key, buildHash[:]...)
}

func legacyUsedBuildPrefix() []byte {
	return []byte{keyNamespace, keyVersion, keyLegacyUsedBuild}
}

func chunkPrefix(scope memory.Scope, buildID string) []byte {
	scopeHash := scopeDigest(scope)
	buildHash := buildDigest(buildID)
	key := []byte{keyNamespace, keyVersion, keyChunk}
	key = append(key, scopeHash[:]...)

	return append(key, buildHash[:]...)
}

func allChunkPrefix() []byte {
	return []byte{keyNamespace, keyVersion, keyChunk}
}

func chunkKey(scope memory.Scope, buildID string, index uint32) []byte {
	key := append([]byte(nil), chunkPrefix(scope, buildID)...)
	suffix := make([]byte, keyIndexSize)
	binary.BigEndian.PutUint32(suffix, index)

	return append(key, suffix...)
}
