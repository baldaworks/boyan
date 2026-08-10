package badger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalRecord(txn *badger.Txn, key []byte, recordType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s record: %w", recordType, err)
	}
	envelope, err := memory.MarshalRecordEnvelope(recordType, payload)
	if err != nil {
		return fmt.Errorf("encode %s record: %w", recordType, err)
	}
	if err := txn.Set(key, envelope); err != nil {
		return fmt.Errorf("write %s record: %w", recordType, err)
	}
	return nil
}

func getCanonicalRecord(txn *badger.Txn, key []byte, recordType string, value any) error {
	item, err := txn.Get(key)
	if err != nil {
		return err
	}
	encoded, err := item.ValueCopy(nil)
	if err != nil {
		return fmt.Errorf("read %s record: %w", recordType, err)
	}
	envelope, err := memory.UnmarshalRecordEnvelope(encoded)
	if err != nil {
		return fmt.Errorf("decode %s record: %w", recordType, err)
	}
	if envelope.RecordType != recordType {
		return fmt.Errorf("unexpected Badger record type %q", envelope.RecordType)
	}
	if err := json.Unmarshal(envelope.Payload, value); err != nil {
		return fmt.Errorf("unmarshal %s record: %w", recordType, err)
	}
	return nil
}

func putCanonicalImmutableRecord(txn *badger.Txn, key []byte, recordType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal immutable %s record: %w", recordType, err)
	}
	item, err := txn.Get(key)
	if err == nil {
		encoded, err := item.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("read immutable %s record: %w", recordType, err)
		}
		envelope, err := memory.UnmarshalRecordEnvelope(encoded)
		if err != nil || envelope.RecordType != recordType || !bytes.Equal(envelope.Payload, payload) {
			return memory.PermanentError(memory.CodeConflict, "canonical immutable record identity was reused", err)
		}
		return nil
	}
	if !errors.Is(err, badger.ErrKeyNotFound) {
		return fmt.Errorf("lookup immutable %s record: %w", recordType, err)
	}
	return putCanonicalRecord(txn, key, recordType, value)
}

func putBadgerCanonicalRecord(txn *badger.Txn, scope memory.Scope, recordType, id string, value any) error {
	key, err := canonicalKey(scope, recordType, id)
	if err != nil {
		return err
	}
	return putCanonicalImmutableRecord(txn, key, recordType, value)
}
