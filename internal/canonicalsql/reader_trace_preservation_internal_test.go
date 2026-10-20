//go:build integration

package canonicalsql

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func prepareTracePreflightHistory(t *testing.T, owner *Owner, kind string) privacyHistory {
	t.Helper()

	if kind != "opaque" {
		return prepareTraceHistory(t, owner)
	}

	h := newPrivacyHistory(t)
	configurePrivacyCase(&h, kind)
	seedPrivacyHistory(t, owner, h)

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		key, _ := privacyKey(h.scope, Head, h.item.ItemID)

		return putPrivacyRecord(ctx, tx, key, Head,
			memory.ItemHead{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID})
	})
	requireEraseTestNoError(t, err)
	requireEraseTestNoError(t, owner.PrepareSourcePrivacy(t.Context()))
	assertPrivacyHistory(t, owner, h, "unknown")

	return h
}

func TestRecognizedMalformedMigratedTraceSourceNative(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := prepareTraceHistory(t, owner)
		err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			record, err := loadTraceSourceRecord(ctx, tx, h.source)
			if err == nil && (record.Turn == nil || len(record.Turn.Messages) != 2) {
				t.Fatal("valid native source control lost its messages")
			}

			return err
		})
		requireEraseTestNoError(t, err)

		malformed := []byte(`{"ref":{"scope":{"key":"test:privacy","kind":"personal"}},` +
			`"export_id":"native","conversation_id":"conversation","source_turn_id":"turn"}`)

		_, recognized, err := migratedTraceSourceRecord(malformed, h.scope)
		if !recognized || err == nil {
			t.Fatalf("recognized migrated payload fell through: %v %v", recognized, err)
		}

		h.source.Payload = privacyRef("malformed-migrated", malformed)
		requireEraseTestNoError(t, owner.PutPayload(t.Context(), malformed, h.source.Payload))

		err = owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			_, err := loadTraceSourceRecord(ctx, tx, h.source)

			return err
		})
		// The recognized legacy DTO's validation error must survive the native
		// payload read; it must not fall through to the native header parser.
		assertCode(t, err, memory.CodeInvalidDerived)
	})
}
