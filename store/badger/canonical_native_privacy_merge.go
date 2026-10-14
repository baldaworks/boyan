package badger

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/pb"
)

func nativePrivacyKV(kv *pb.KV) ([]string, memory.RecordEnvelope, bool, error) {
	components, _, err := decodeCanonicalLogicalKey(kv.Key)
	if err != nil {
		return nil, memory.RecordEnvelope{}, false, err
	}

	if !isCanonicalPrivacyRecord(components[0]) {
		return components, memory.RecordEnvelope{}, false, nil
	}

	if len(kv.Meta) > 0 && kv.Meta[0]&1 != 0 {
		return components, memory.RecordEnvelope{}, true, nil
	}

	if len(kv.Value) > maxCanonicalLogicalLineBytes || kv.ExpiresAt != 0 {
		return nil, memory.RecordEnvelope{}, false, invalidSourcePrivacy()
	}

	envelope, err := memory.UnmarshalRecordEnvelope(kv.Value)
	if err != nil {
		return nil, envelope, false, invalidSourcePrivacy()
	}

	if err := validateCanonicalPrivacyRecord(components, envelope); err != nil {
		return nil, envelope, false, err
	}

	return components, envelope, false, nil
}

func (s *CanonicalStore) stageNativePrivacyKV(kv *pb.KV) (bool, error) {
	components, envelope, deleted, err := nativePrivacyKV(kv)
	if err != nil {
		return false, err
	}

	if deleted {
		return false, nil
	}

	if !isCanonicalPrivacyRecord(components[0]) || components[0] == badgerRecordSourcePrivacyReady {
		return true, nil
	}

	err = s.db.View(func(txn *badger.Txn) error {
		merged, err := mergeNativePrivacyRecord(txn, kv.Key, components, envelope, false)
		if err != nil {
			return err
		}

		kv.Value = merged

		return nil
	})

	return err == nil, err
}

func mergeNativePrivacyRecord(
	txn *badger.Txn, key []byte, components []string, envelope memory.RecordEnvelope, replay bool,
) ([]byte, error) {
	var payload any

	switch components[0] {
	case badgerRecordSourcePrivacy:
		record, err := mergeNativePrivacyAggregate(txn, key, decodePrivacyAggregate(envelope), replay)
		if err != nil {
			return nil, err
		}

		payload = record
	case badgerRecordSourcePrivacyScope:
		record, err := mergeNativePrivacyScope(txn, key, envelope.Payload)
		if err != nil {
			return nil, err
		}

		payload = record
	default:
		return memory.MarshalRecordEnvelope(envelope.RecordType, envelope.Payload)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return memory.MarshalRecordEnvelope(components[0], data)
}

func mergeRestrictivePrivacy(record, basis sourcePrivacy) (sourcePrivacy, error) {
	if record.Scope != basis.Scope || record.SourceID != basis.SourceID || record.Payload != basis.Payload {
		return record, invalidSourcePrivacy()
	}

	unknown := basis.StickyUnknown || basis.State == sourcePrivacyUnknown && !basis.Preparing
	if basis.State == sourcePrivacySensitive {
		record.State = sourcePrivacySensitive
	}

	if unknown {
		record.StickyUnknown = true
		if record.State != sourcePrivacySensitive {
			record.State = sourcePrivacyUnknown
		}
	}

	return record, nil
}

func (s *CanonicalStore) replayNativePrivacyBackup(ctx context.Context, reader io.Reader) error {
	for {
		list, err := readNativePrivacyFrame(ctx, reader)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		for _, kv := range list.Kv {
			if err := s.replayNativePrivacyKV(ctx, kv); err != nil {
				return err
			}

			if s.afterNativeRestoreReplay != nil {
				if err := s.afterNativeRestoreReplay(); err != nil {
					return err
				}
			}
		}
	}
}

func (s *CanonicalStore) replayNativePrivacyKV(ctx context.Context, kv *pb.KV) error {
	components, envelope, _, err := nativePrivacyKV(kv)
	if err != nil {
		return err
	}

	if !isCanonicalPrivacyRecord(components[0]) || components[0] == badgerRecordSourcePrivacyReady {
		return nil
	}

	return s.db.Update(func(txn *badger.Txn) error {
		if err := canonicalContextError(ctx); err != nil {
			return err
		}

		if components[0] == badgerRecordSourcePrivacy {
			if err := validatePrivacyAggregateBindings(txn, decodePrivacyAggregate(envelope)); err != nil {
				return err
			}
		}

		merged, err := mergeNativePrivacyRecord(txn, kv.Key, components, envelope, true)
		if err != nil {
			return err
		}

		return txn.Set(kv.Key, merged)
	})
}

func mergeNativePrivacyAggregate(
	txn *badger.Txn, key []byte, incoming sourcePrivacy, replay bool,
) (sourcePrivacy, error) {
	var current sourcePrivacy

	err := getPrivacyPreparationRecord(txn, key, badgerRecordSourcePrivacy, &current)
	if privacyMissing(err) {
		return incoming, nil
	}

	if err != nil {
		return incoming, err
	}

	if replay {
		return mergeRestrictivePrivacy(current, incoming)
	}

	return mergeRestrictivePrivacy(incoming, current)
}

func mergeNativePrivacyScope(txn *badger.Txn, key, payload []byte) (sourcePrivacyScope, error) {
	var incoming, current sourcePrivacyScope
	if json.Unmarshal(payload, &incoming) != nil {
		return incoming, invalidSourcePrivacy()
	}

	err := getPrivacyPreparationRecord(txn, key, badgerRecordSourcePrivacyScope, &current)
	if err != nil && !privacyMissing(err) {
		return incoming, err
	}

	if err == nil {
		incoming.Incomplete = incoming.Incomplete || current.Incomplete
	}

	return incoming, nil
}
