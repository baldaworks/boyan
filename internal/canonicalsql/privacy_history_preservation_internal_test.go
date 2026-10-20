//go:build integration

package canonicalsql

import (
	"bytes"
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyHistoricalMixedSiblings(t *testing.T) {
	for _, separate := range []bool{false, true} {
		name := "mixed-same"
		if separate {
			name = "mixed-separate"
		}

		t.Run(name, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
				h := newPrivacyHistory(t)
				sensitive := h.revision
				sensitive.RevisionID = "sensitive-history"
				sensitive.Sensitivity = memory.SensitivitySensitive

				sensitive.Evidence = append([]memory.EvidenceRef(nil), h.revision.Evidence...)
				if separate {
					h.messages[1].Role = memory.MessageRoleUser
					sensitive.Evidence[0] = memory.EvidenceRef{SourceID: h.source.SourceID,
						MessageID: h.messages[1].MessageID, Role: memory.MessageRoleUser,
						EndByte: h.messages[1].Payload.ByteSize, TextDigest: h.messages[1].Payload.Digest,
						AssertionMode: memory.AssertionModeUser}
				}

				seedPrivacyHistory(t, owner, h)
				err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					key, _ := privacyKey(h.scope, Revision, sensitive.RevisionID)

					return putPrivacyRecord(ctx, tx, key, Revision, sensitive)
				})
				requireEraseTestNoError(t, err)
				requireEraseTestNoError(t, owner.PrepareSourcePrivacy(t.Context()))
				assertPrivacyHistory(t, owner, h, "sensitive")
				requireEraseTestNoError(t, owner.Close())
				assertPrivacyHistory(t, reopen(), h, "sensitive")
			})
		})
	}
}

func TestPrivacyPreparationRebuildsDeletedEvidenceEdge(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		h := prepareTraceHistory(t, owner)
		edge := Key{Family: SourceRevision, Scope: h.scope,
			Parts: [][]byte{[]byte(h.source.SourceID), []byte(h.revision.RevisionID)}}
		err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if _, err := tx.Get(ctx, edge); err != nil {
				return err
			}

			if err := tx.Delete(ctx, edge); err != nil {
				return err
			}

			return tx.Delete(ctx, Key{Family: SourcePrivacyReady})
		})
		requireEraseTestNoError(t, err)
		requireEraseTestNoError(t, owner.PrepareSourcePrivacy(t.Context()))
		requireEraseTestNoError(t, owner.Close())
		owner = reopen()
		requireEraseTestNoError(t, owner.SourcePrivacyReady(t.Context()))
		assertPrivacyHistory(t, owner, h, "standard")

		err = owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			_, err := tx.Get(ctx, edge)

			return err
		})
		requireEraseTestNoError(t, err)
	})
}

func TestPrivacyPreparationRevisionCorruption(t *testing.T) {
	for _, kind := range []string{"revision-id", "oversized-envelope"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := newPrivacyHistory(t)
				seedPrivacyHistory(t, owner, h)
				corruptPrivacyRevision(t, owner, h, kind)

				if err := owner.PrepareSourcePrivacy(t.Context()); err == nil {
					t.Fatal("corrupt revision history prepared")
				}

				assertCode(t, owner.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)
			})
		})
	}
}

func corruptPrivacyRevision(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if kind == "oversized-envelope" {
		data := bytes.Repeat([]byte("x"), MaxEnvelopeBytes+1)
		statement := "UPDATE " + owner.connection.table("revision") + " SET envelope=$1 WHERE id=$2"

		_, err := owner.connection.q.exec(t.Context(), statement, data, []byte(h.revision.RevisionID))
		if err == nil {
			t.Fatal("SQL constraint accepted oversized authoritative envelope")
		}

		// Unlike Badger, SQL rejects the oversized row at its constraint. Retain
		// the hostile-read assertion with a maximal legal but invalid envelope.
		_, err = owner.connection.q.exec(t.Context(), statement, data[:MaxEnvelopeBytes], []byte(h.revision.RevisionID))
		requireEraseTestNoError(t, err)

		return
	}

	id := h.revision.RevisionID
	h.revision.RevisionID = ""
	data := envelope(t, Revision, h.revision)
	_, err := owner.connection.q.exec(t.Context(),
		"UPDATE "+owner.connection.table("revision")+" SET envelope=$1 WHERE id=$2", data, []byte(id))
	requireEraseTestNoError(t, err)
}
