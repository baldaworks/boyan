package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func collectForgetSourcePayloads(
	txn *badger.Txn,
	scope memory.Scope,
	deniedSources map[string]struct{},
	collector *scrubPayloadCollector,
) error {
	if len(deniedSources) == 0 {
		return nil
	}
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return err
	}
	iterator := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		var source memory.CanonicalSourceRecord
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSource, &source); err != nil {
			return err
		}
		if _, ok := deniedSources[source.SourceID]; ok {
			collector.add(source.Payload)
		}
	}
	return nil
}

func collectForgetRevisionPayloads(
	txn *badger.Txn,
	scope memory.Scope,
	deniedRevisions map[string]struct{},
	collector *scrubPayloadCollector,
) error {
	if len(deniedRevisions) == 0 {
		return nil
	}
	prefix, err := canonicalPrefix(scope, badgerRecordRevision)
	if err != nil {
		return err
	}
	iterator := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		var revision memory.MemoryRevision
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordRevision, &revision); err != nil {
			return err
		}
		if _, ok := deniedRevisions[revision.RevisionID]; ok {
			collector.add(revision.Payload)
		}
	}
	return nil
}
