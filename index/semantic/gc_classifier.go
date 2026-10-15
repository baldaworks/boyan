package semantic

import (
	"bytes"
	"encoding/binary"

	"github.com/dgraph-io/badger/v4"
)

type garbageRecordKind byte

const (
	garbageState garbageRecordKind = iota + 1
	garbageLegacyClaim
	garbageChunk
)

type garbageActiveGeneration struct {
	exists bool
	state  State
}

type garbageBatchClassifier struct {
	transaction   *badger.Txn
	activeByScope map[string]garbageActiveGeneration
}

func (classifier *garbageBatchClassifier) keep(key []byte, kind garbageRecordKind) (bool, error) {
	switch kind {
	case garbageLegacyClaim:
		return false, nil
	case garbageState:
		return classifier.keepState(key)
	case garbageChunk:
		return classifier.keepChunk(key)
	default:
		return false, ErrCorrupt
	}
}

func (classifier *garbageBatchClassifier) keepState(key []byte) (bool, error) {
	scopeHash, _, valid := decodeProjectionKey(key, keyState, false)
	if !valid {
		return false, nil
	}

	active, err := classifier.active(scopeHash)
	if err != nil || !active.exists {
		return false, err
	}

	return bytes.Equal(key, stateKey(active.state.Scope, active.state.BuildID)), nil
}

func (classifier *garbageBatchClassifier) keepChunk(key []byte) (bool, error) {
	scopeHash, index, valid := decodeProjectionKey(key, keyChunk, true)
	if !valid {
		return false, nil
	}

	active, err := classifier.active(scopeHash)
	if err != nil || !active.exists {
		return false, err
	}

	if active.state.Status != StatusReady || index >= active.state.ChunkCount {
		return false, nil
	}

	prefix := chunkPrefix(active.state.Scope, active.state.BuildID)

	return bytes.Equal(key[:len(prefix)], prefix), nil
}

func decodeProjectionKey(key []byte, keyType byte, indexed bool) ([]byte, uint32, bool) {
	length := len(allChunkPrefix()) + sha256DigestBytes*hashesPerGeneration
	if indexed {
		length += keyIndexSize
	}

	if len(key) != length || key[0] != keyNamespace || key[1] != keyVersion || key[2] != keyType {
		return nil, 0, false
	}

	scopeStart := len(allChunkPrefix())
	buildStart := scopeStart + sha256DigestBytes
	buildEnd := buildStart + sha256DigestBytes

	index := uint32(0)
	if indexed {
		index = binary.BigEndian.Uint32(key[buildEnd:])
	}

	return key[scopeStart:buildStart], index, true
}

func (classifier *garbageBatchClassifier) active(scopeHash []byte) (garbageActiveGeneration, error) {
	cacheKey := string(scopeHash)
	if active, found := classifier.activeByScope[cacheKey]; found {
		return active, nil
	}

	key := make([]byte, 0, len(allChunkPrefix())+sha256DigestBytes)
	key = append(key, keyNamespace, keyVersion, keyActive)
	key = append(key, scopeHash...)

	item, err := classifier.transaction.Get(key)
	if err == badger.ErrKeyNotFound {
		active := garbageActiveGeneration{}
		classifier.activeByScope[cacheKey] = active

		return active, nil
	}

	if err != nil {
		return garbageActiveGeneration{}, err
	}

	pointer, err := decodeActiveItem(item)
	if err != nil {
		return garbageActiveGeneration{}, err
	}

	scope := scopeFromPointer(pointer)

	record, err := loadStoredState(classifier.transaction, scope, pointer.BuildID)
	if err != nil {
		return garbageActiveGeneration{}, err
	}

	if err := validateGarbageActiveState(pointer, record); err != nil {
		return garbageActiveGeneration{}, err
	}

	active := garbageActiveGeneration{exists: true, state: record.State}
	classifier.activeByScope[cacheKey] = active

	return active, nil
}

func validateGarbageActiveState(pointer activePointer, record storedState) error {
	state := record.State
	if !record.Complete || state.PublicationEpoch != pointer.Epoch {
		return ErrCorrupt
	}

	switch state.Status {
	case StatusReady:
		return validateGarbageReadyState(record)
	case StatusDegraded:
		if err := validateDegradedState(record); err != nil {
			return ErrCorrupt
		}

		return nil
	default:
		return ErrCorrupt
	}
}

func validateGarbageReadyState(record storedState) error {
	state := record.State
	if validateReadyState(record) != nil || !validHash(state.CoverageHash) ||
		state.RecordCount > state.ChunkCount {
		return ErrCorrupt
	}

	if _, err := generationRowBudget(state); err != nil {
		return ErrCorrupt
	}

	return nil
}
