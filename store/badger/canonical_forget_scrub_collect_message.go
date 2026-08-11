package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func collectForgetMessagePayloads(
	txn *badger.Txn,
	scope memory.Scope,
	deniedSources map[string]struct{},
	collector *scrubPayloadCollector,
) error {
	// Message payloads are independent immutable blobs. Removing only
	// source/revision payloads would leave forgotten turn text reachable by a
	// trace hydrator, so scan the bounded exact scope for denied sources.
	if len(deniedSources) == 0 {
		return nil
	}
	prefix, err := canonicalPrefix(scope, badgerRecordMessage)
	if err != nil {
		return err
	}
	iterator := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		var message memory.MessageRecord
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordMessage, &message); err != nil {
			return err
		}
		if _, ok := deniedSources[message.SourceID]; ok {
			collector.add(message.Payload)
		}
	}
	return nil
}
