package canonicalsql

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// PutPayload persists immutable bytes and their stamp in the same transaction.
func (o *Owner) PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error {
	if ctx == nil {
		return invalid("invalid canonical SQL context")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := (memory.CanonicalPayload{Ref: ref, Data: data}).Validate(); err != nil {
		return err
	}

	if len(data) > MaxEnvelopeBytes {
		return limitError()
	}

	key, err := payloadRecordKey(ref)
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
			return err
		}

		if err := checkEraseBlobWrite(ctx, tx, ref.ID); err != nil {
			return err
		}

		return putImmutablePrivacyRecord(ctx, tx, key, data)
	})
}

// LoadPayload reads and validates the exact referenced immutable content.
func (o *Owner) LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error) {
	key, err := payloadRecordKey(ref)
	if err != nil {
		return nil, err
	}

	var data []byte

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return getPrivacyRecord(ctx, tx, key, Payload, &data)
	})
	if err != nil {
		return nil, err
	}

	if (memory.CanonicalPayload{Ref: ref, Data: data}).Validate() != nil {
		return nil, invalidSourcePrivacy()
	}

	return data, nil
}

// DeletePayload removes raw content after logical forgetting, with an archive stamp.
func (o *Owner) DeletePayload(ctx context.Context, ref memory.PayloadRef) error {
	key, err := payloadRecordKey(ref)
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
			return err
		}

		if err := checkEraseBlobLock(ctx, tx, ref.ID); err != nil {
			return err
		}

		return tx.Delete(ctx, key)
	})
}

func checkScopeErasePayloadGuard(ctx context.Context, tx *Tx) error {
	var guard struct {
		Pending uint64 `json:"pending"`
	}

	err := getPrivacyRecord(ctx, tx, Key{Family: EraseGuard}, EraseGuard, &guard)
	if privacyMissing(err) {
		return nil
	}

	if err != nil || guard.Pending == 0 {
		return invalidSourcePrivacy()
	}

	return eraseConflict()
}

func eraseConflict() error {
	return memory.RetryableError(memory.CodeConflict, "scope erase is pending or its baseline changed", nil)
}

func checkEraseBlobLock(ctx context.Context, tx *Tx, id string) error {
	var lock struct {
		Scope       memory.Scope `json:"scope"`
		OperationID string       `json:"operation_id"`
	}

	err := getPrivacyRecord(ctx, tx, Key{Family: EraseBlobLock, Parts: [][]byte{[]byte(id)}}, EraseBlobLock, &lock)
	if privacyMissing(err) {
		return nil
	}

	if err != nil || lock.OperationID == "" || lock.Scope.Validate() != nil {
		return invalidSourcePrivacy()
	}

	return eraseConflict()
}

func checkEraseBlobWrite(ctx context.Context, tx *Tx, id string) error {
	var erased bool

	err := getPrivacyRecord(ctx, tx, Key{Family: ErasedBlob, Parts: [][]byte{[]byte(id)}}, ErasedBlob, &erased)
	if !privacyMissing(err) {
		if err != nil || !erased {
			return invalidSourcePrivacy()
		}

		return memory.PermanentError(memory.CodeConflict, "canonical payload was forgotten", nil)
	}

	return checkEraseBlobLock(ctx, tx, id)
}

type deniedSource struct {
	SourceID string    `json:"source_id"`
	DeniedAt time.Time `json:"denied_at"`
}
type deniedRevision struct {
	RevisionID string    `json:"revision_id"`
	DeniedAt   time.Time `json:"denied_at"`
}

// IsSourceDenied reports the permanent exact-scope source denial.
func (o *Owner) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	return o.isDenied(ctx, scope, DeniedSource, id)
}

// IsRevisionDenied reports the permanent exact-scope revision denial.
func (o *Owner) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	return o.isDenied(ctx, scope, DeniedRevision, id)
}

func (o *Owner) isDenied(ctx context.Context, scope memory.Scope, family Family, id string) (bool, error) {
	var found bool

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		found, err = privacyDenied(ctx, tx, scope, family, id)

		return err
	})

	return found, err
}

func privacyDenied(ctx context.Context, tx *Tx, scope memory.Scope, family Family, id string) (bool, error) {
	key, err := privacyKey(scope, family, id)
	if err != nil {
		return false, err
	}

	if family == DeniedSource {
		var denied deniedSource

		err = getPrivacyRecord(ctx, tx, key, family, &denied)
		if err == nil && (denied.SourceID != id || denied.DeniedAt.IsZero()) {
			err = invalidSourcePrivacy()
		}
	} else {
		var denied deniedRevision

		err = getPrivacyRecord(ctx, tx, key, family, &denied)
		if err == nil && (denied.RevisionID != id || denied.DeniedAt.IsZero()) {
			err = invalidSourcePrivacy()
		}
	}

	if privacyMissing(err) {
		return false, nil
	}

	return err == nil, err
}
