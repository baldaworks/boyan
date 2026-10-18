package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type eraseRawRecord struct {
	Key      Key
	Scope    memory.Scope
	ID       string
	Payload  memory.PayloadRef
	Source   *memory.CanonicalSourceRecord
	Message  *memory.MessageRecord
	Revision *memory.MemoryRevision
}

func eraseKey(scope memory.Scope, family Family, id string) (Key, error) {
	def, err := familyDefinition(family)
	if err != nil {
		return Key{}, err
	}

	key := Key{Family: family}
	if def.scoped {
		key.Scope = scope
	}

	if len(def.parts) > 0 {
		key.Parts = [][]byte{[]byte(id)}
	}

	_, _, err = key.values()

	return key, err
}

func eraseRange(scope memory.Scope, family Family) (Range, error) {
	if err := scope.Validate(); err != nil {
		return Range{}, err
	}

	return Range{Family: family, Scope: &scope, Limit: scopeEraseBatchSize}, nil
}
func eraseGlobalPrefix(family Family) (Range, error) {
	return Range{Family: family, Limit: scopeEraseBatchSize}, nil
}
func eraseBlobLockKey(id string) (Key, error) { return eraseKey(memory.Scope{}, EraseBlobLock, id) }
func eraseGuardKey() (Key, error)             { return Key{Family: EraseGuard}, nil }
func eraseOwnedKey(j memory.ScopeEraseJournal, family Family, id string) (Key, error) {
	return mutationEdgeKey(j.Scope, family, j.OperationID, id)
}
func eraseOwnedPrefix(j memory.ScopeEraseJournal, family Family) (Range, error) {
	return Range{Family: family, Scope: &j.Scope, Prefix: [][]byte{[]byte(j.OperationID)}, Limit: scopeEraseBatchSize}, nil
}

// The frame transports the exclusive raw tuple; SQL always orders its components.
func eraseWalkPage(ctx context.Context, tx *Tx, scan Range, cursor []byte, visit func(Key) error) ([]byte,
	bool, error) {
	if len(cursor) > 0 {
		key, err := keyFromFrame(scan.Family, cursor)
		if err != nil {
			return nil, false, err
		}

		scan.After = &key
	}

	rows, err := tx.Scan(ctx, scan)
	if err != nil {
		return nil, false, err
	}

	var last []byte

	for _, row := range rows {
		if err := visit(row.Key); err != nil {
			return nil, false, err
		}

		_, values, _ := row.Key.values()
		last = stampKey(values)
	}

	if len(rows) < scan.Limit {
		return last, true, nil
	}

	key := rows[len(rows)-1].Key
	scan.After = &key
	scan.Limit = 1
	more, err := tx.hasRangeRows(ctx, scan)

	return last, !more, err
}

func eraseRecord(ctx context.Context, tx *Tx, key Key, kind Family) (eraseRawRecord, error) {
	r := eraseRawRecord{Key: key, Scope: key.Scope}
	if key.Family != kind || len(key.Parts) != 1 || key.Scope.Validate() != nil {
		return r, corruptRow()
	}

	r.ID = string(key.Parts[0])

	var err error

	switch kind {
	case Source:
		err = decodeEraseSource(ctx, tx, &r)
	case Message:
		err = decodeEraseMessage(ctx, tx, &r)
	case Revision:
		err = decodeEraseRevision(ctx, tx, &r)
	default:
		err = corruptRow()
	}

	return r, err
}

func verifyEraseLocksReleased(ctx context.Context, tx *Tx, j memory.ScopeEraseJournal) error {
	scan, err := eraseOwnedPrefix(j, EraseLockRef)
	if err != nil {
		return err
	}

	scan.Limit = 1

	found, err := tx.hasRangeRows(ctx, scan)
	if err != nil {
		return err
	}

	if found {
		return eraseConflict()
	}

	return nil
}

func validErasePayload(data []byte, ref memory.PayloadRef) bool {
	return (memory.CanonicalPayload{Ref: ref, Data: data}).Validate() == nil
}
func listedCanonicalSourcePayload(ctx context.Context, tx *Tx, id string) ([]byte, error) {
	key, err := eraseKey(memory.Scope{}, Payload, id)
	if err != nil {
		return nil, err
	}

	var data []byte

	err = getPrivacyRecord(ctx, tx, key, Payload, &data)

	return data, err
}

func decodeEraseSource(ctx context.Context, tx *Tx, r *eraseRawRecord) error {
	r.Source = &memory.CanonicalSourceRecord{}
	if err := getPrivacyRecord(ctx, tx, r.Key, Source, r.Source); err != nil {
		return err
	}

	if r.Source.Validate() != nil || r.Source.Scope != r.Scope || r.Source.SourceID != r.ID {
		return corruptRow()
	}

	r.Payload = r.Source.Payload

	return nil
}
func decodeEraseMessage(ctx context.Context, tx *Tx, r *eraseRawRecord) error {
	r.Message = &memory.MessageRecord{}
	if err := getPrivacyRecord(ctx, tx, r.Key, Message, r.Message); err != nil {
		return err
	}

	if r.Message.Validate() != nil || r.Message.MessageID != r.ID {
		return corruptRow()
	}

	r.Payload = r.Message.Payload

	return nil
}
func decodeEraseRevision(ctx context.Context, tx *Tx, r *eraseRawRecord) error {
	r.Revision = &memory.MemoryRevision{}
	if err := getPrivacyRecord(ctx, tx, r.Key, Revision, r.Revision); err != nil {
		return err
	}

	if r.Revision.Validate() != nil || r.Revision.RevisionID != r.ID {
		return corruptRow()
	}

	r.Payload = r.Revision.Payload

	return nil
}
