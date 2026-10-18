package canonicalsql

import (
	"context"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestReaderFinalBarrierOmittedMembership(t *testing.T) {
	for _, kind := range []string{"uncertified", "certified", "embedded", "evidence-role",
		"unchanged-certified", "unchanged-embedded-only"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runReaderMembershipBarrier(t, owner, kind)
			})
		})
	}
}

func runReaderMembershipBarrier(t *testing.T, owner *Owner, kind string) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	if strings.Contains(kind, "embedded") {
		configureEmbeddedPrivacy(t, &h, "embedded")
	}

	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	var captured readerSnapshot

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		records, err := loadReaderRecords(ctx, tx, h.scope, []string{h.revision.RevisionID}, time.Now(), &captured)
		if err == nil && len(records) != 1 {
			t.Fatal("hydration did not capture real content")
		}

		return err
	}); err != nil {
		t.Fatal(err)
	}

	alterReaderBarrierMembership(t, owner, h, kind)
	err := owner.finalReaderBarrier(t.Context(), captured)
	t.Logf("case=%s barrier=%v", kind, err)

	if strings.HasPrefix(kind, "unchanged-") {
		if err != nil {
			t.Fatal(err)
		}
	} else if err == nil {
		t.Fatal("fresh barrier authorized changed evidence or incomplete membership")
	}
}

func alterReaderBarrierMembership(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		record, _, err := loadSourcePrivacy(ctx, tx, h.source)
		if err != nil {
			return err
		}

		record.MessageMembershipComplete = false
		record.MessageIDs = nil

		key, _ := privacyKey(h.scope, SourcePrivacy, h.source.SourceID)
		if err := putPrivacyRecord(ctx, tx, key, SourcePrivacy, record); err != nil {
			return err
		}

		if kind == "certified" || kind == "unchanged-certified" || kind == "evidence-role" {
			key, _ := privacyKey(h.scope, TurnCertificate, h.source.SourceID)
			if err := putPrivacyRecord(ctx, tx, key, TurnCertificate, privacyCertificate(t, h)); err != nil {
				return err
			}
		}

		return alterReaderBarrierMember(ctx, tx, h, kind)
	}); err != nil {
		t.Fatal(err)
	}
}

func alterReaderBarrierMember(ctx context.Context, tx *Tx, h privacyHistory, kind string) error {
	if kind == "unchanged-certified" {
		return nil
	}

	if kind == "unchanged-embedded-only" {
		for _, message := range h.messages {
			key, _ := privacyKey(h.scope, Message, message.MessageID)
			if err := tx.Delete(ctx, key); err != nil {
				return err
			}
		}

		return nil
	}

	message := h.messages[0]

	message.SourceID = "foreign-owner"
	if kind == "evidence-role" {
		message.SourceID = h.source.SourceID
		message.Role = memory.MessageRoleAssistant
	}

	key, _ := privacyKey(h.scope, Message, message.MessageID)

	return putPrivacyRecord(ctx, tx, key, Message, message)
}
