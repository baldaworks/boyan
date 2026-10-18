package canonicalsql

import (
	"context"
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyRepresentationsAndIncompleteRetry(t *testing.T) {
	for _, kind := range []string{"certified", "embedded", "embedded-only", "opaque-fields", "incomplete-retry"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runPrivacyRepresentation(t, owner, kind)
			})
		})
	}
}

func runPrivacyRepresentation(t *testing.T, owner *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	want := "standard"

	configureEmbeddedPrivacy(t, &h, kind)

	if kind == "incomplete-retry" {
		h.payloads = h.payloads[:1]
		want = "unknown"
	}

	seedPrivacyHistory(t, owner, h)

	writePrivacyRepresentation(t, owner, h, kind, &want)

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := privacyRecordState(t, owner, h); got.State != want {
		t.Fatalf("state=%s want %s", got.State, want)
	}

	if kind == "incomplete-retry" {
		if err := owner.Update(t.Context(),
			func(ctx context.Context,
				tx *Tx) error {
				return tx.Delete(ctx,
					Key{Family: SourcePrivacyReady})
			}); err != nil {
			t.Fatal(err)
		}

		if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatalf("valid incomplete state cannot restart: %v", err)
		}
	}
}
func configureEmbeddedPrivacy(t *testing.T, h *privacyHistory, kind string) {
	t.Helper()

	if kind == "embedded" || kind == "embedded-only" {
		embedded := memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1,
			Ref: memory.SourceRef{Scope: h.scope,
				ExportID:       h.turn.ExportID,
				ConversationID: h.turn.Conversation.ConversationID,
				SourceTurnID:   h.turn.SourceTurnID},
			State: memory.SourceStateActive,
			Turn:  &h.turn}

		data, err := json.Marshal(embedded)
		if err != nil {
			t.Fatal(err)
		}

		h.source.Payload = privacyRef("embedded", data)

		h.payloads[0] = memory.CanonicalPayload{Ref: h.source.Payload, Data: data}
		if kind == "embedded-only" {
			h.messages = nil
		}
	}
}
func writePrivacyRepresentation(t *testing.T, owner *Owner, h privacyHistory, kind string, want *string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		switch kind {
		case "certified":
			key, _ := privacyKey(h.scope, TurnCertificate, h.source.SourceID)

			return putPrivacyRecord(ctx, tx, key, TurnCertificate, privacyCertificate(t, h))
		case "opaque-fields":
			data, err := json.Marshal(h.revision)
			if err != nil {
				return err
			}

			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				return err
			}

			value["schema_version"] = "unsupported"
			value["opaque_legacy_field"] = "retained"
			key, _ := privacyKey(h.scope, Revision, h.revision.RevisionID)
			*want = "unknown"

			return putPrivacyRecord(ctx, tx, key, Revision, value)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
