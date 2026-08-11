package badger

import (
	"encoding/json"
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
