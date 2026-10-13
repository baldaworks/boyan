package badger

import (
	"bytes"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type eraseRawRecord struct {
	Key      []byte
	Scope    memory.Scope
	ID       string
	Payload  memory.PayloadRef
	Source   *memory.CanonicalSourceRecord
	Message  *memory.MessageRecord
	Revision *memory.MemoryRevision
}

// The exclusive cursor follows encoded tuple order, not decoded identifier order.
func eraseWalkPage(txn *badger.Txn, prefix, cursor []byte, visit func([]byte) error) ([]byte, bool, error) {
	if len(cursor) > 0 && !bytes.HasPrefix(cursor, prefix) {
		return nil, false, eraseStoreFailure("scope erase cursor is outside its phase", nil)
	}

	options := badger.DefaultIteratorOptions
	options.PrefetchValues = false

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	seek := prefix
	if len(cursor) > 0 {
		seek = cursor
	}

	iterator.Seek(seek)

	if iterator.ValidForPrefix(prefix) && bytes.Equal(iterator.Item().Key(), cursor) {
		iterator.Next()
	}

	var last []byte

	for count := 0; iterator.ValidForPrefix(prefix) && count < scopeEraseBatchSize; count++ {
		key := iterator.Item().KeyCopy(nil)
		if err := visit(key); err != nil {
			return nil, false, err
		}

		last = key

		iterator.Next()
	}

	return last, !iterator.ValidForPrefix(prefix), nil
}

func eraseRecord(txn *badger.Txn, key []byte, kind string) (eraseRawRecord, error) {
	parts, _, err := decodeCanonicalLogicalKey(key)
	if err != nil {
		return eraseRawRecord{}, err
	}

	if len(parts) != canonicalRecordComponents || parts[0] != kind {
		return eraseRawRecord{}, eraseStoreFailure("scope erase tuple type differs", nil)
	}

	result := eraseRawRecord{Key: key, Scope: memory.Scope{Key: parts[1], Kind: memory.ScopeKind(parts[2])}, ID: parts[3]}
	if err := result.Scope.Validate(); err != nil {
		return result, eraseStoreFailure("scope erase tuple scope is invalid", err)
	}

	err = decodeEraseRecord(txn, kind, &result)
	if err != nil {
		return result, eraseStoreFailure("invalid scope erase structural record", err)
	}

	return result, nil
}

func eraseGlobalPrefix(kind string) ([]byte, error) {
	return memory.TupleKey(canonicalNamespace, canonicalVersion, kind)
}

func decodeEraseRecord(txn *badger.Txn, kind string, result *eraseRawRecord) error {
	switch kind {
	case badgerRecordSource:
		return decodeEraseSource(txn, result)
	case badgerRecordMessage:
		return decodeEraseMessage(txn, result)
	case badgerRecordRevision:
		return decodeEraseRevision(txn, result)
	default:
		return eraseStoreFailure("scope erase record kind is invalid", nil)
	}
}

func decodeEraseSource(txn *badger.Txn, result *eraseRawRecord) error {
	source := &memory.CanonicalSourceRecord{}
	if err := getCanonicalRecord(txn, result.Key, badgerRecordSource, source); err != nil {
		return err
	}

	if err := source.Validate(); err != nil {
		return err
	}

	if source.Scope != result.Scope || source.SourceID != result.ID {
		return eraseStoreFailure("scope erase source identity differs", nil)
	}

	result.Source = source
	result.Payload = source.Payload

	return nil
}

func decodeEraseMessage(txn *badger.Txn, result *eraseRawRecord) error {
	message := &memory.MessageRecord{}
	if err := getCanonicalRecord(txn, result.Key, badgerRecordMessage, message); err != nil {
		return err
	}

	if err := message.Validate(); err != nil {
		return err
	}

	if message.MessageID != result.ID {
		return eraseStoreFailure("scope erase message identity differs", nil)
	}

	result.Message = message
	result.Payload = message.Payload

	return nil
}

func decodeEraseRevision(txn *badger.Txn, result *eraseRawRecord) error {
	revision := &memory.MemoryRevision{}
	if err := getCanonicalRecord(txn, result.Key, badgerRecordRevision, revision); err != nil {
		return err
	}

	if err := revision.Validate(); err != nil {
		return err
	}

	if revision.RevisionID != result.ID {
		return eraseStoreFailure("scope erase revision identity differs", nil)
	}

	result.Revision = revision
	result.Payload = revision.Payload

	return nil
}
