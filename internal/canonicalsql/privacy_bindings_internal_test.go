package canonicalsql

import (
	"context"
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func privacyCertificate(t *testing.T, h privacyHistory) memory.CanonicalTurnCertificate {
	t.Helper()

	fingerprint, err := memory.CanonicalTurnFingerprint(h.turn)
	if err != nil {
		t.Fatal(err)
	}

	certificate := memory.CanonicalTurnCertificate{Version: memory.CanonicalTurnFingerprintVersion,
		SourceID:    h.source.SourceID,
		Fingerprint: fingerprint}
	for _, message := range h.messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	return certificate
}

func TestPrivacyPreparationRejectsBindings(t *testing.T) {
	for _, kind := range []string{"native-header",
		"message-payload",
		"certificate-subset",
		"aggregate-subset",
		"source-scope",
		"scope-metadata",
		"privacy-combination",
		"orphan-aggregate"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runPrivacyBindings(t, owner, kind)
			})
		})
	}
}

func runPrivacyBindings(t *testing.T, owner *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	certificate := privacyCertificate(t, h)

	if kind == "native-header" {
		var header map[string]any
		if err := json.Unmarshal(h.payloads[0].Data, &header); err != nil {
			t.Fatal(err)
		}

		header["export_id"] = "foreign-native-export"

		data, err := json.Marshal(header)
		if err != nil {
			t.Fatal(err)
		}

		h.source.Payload = privacyRef("changed-header", data)
		h.payloads[0] = memory.CanonicalPayload{Ref: h.source.Payload, Data: data}
	}

	seedPrivacyHistory(t, owner, h)

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		return putCorruptPrivacyBinding(t, ctx, tx, h, certificate, kind)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := owner.PrepareSourcePrivacy(t.Context()); err == nil {
		t.Fatal("corrupt binding certified")
	}

	if err := owner.SourcePrivacyReady(t.Context()); err == nil {
		t.Fatal("failed pass published readiness")
	}
}

func putCorruptPrivacyBinding(t *testing.T, ctx context.Context, tx *Tx,
	h privacyHistory, certificate memory.CanonicalTurnCertificate, kind string,
) error {
	t.Helper()

	if kind == "certificate-subset" {
		certificate.MessageIDs = certificate.MessageIDs[:1]
	}

	if err := tx.Put(ctx,
		Key{Family: TurnCertificate,
			Scope: h.scope,
			Parts: [][]byte{[]byte(h.source.SourceID)}},
		envelope(t,
			TurnCertificate,
			certificate)); err != nil {
		return err
	}

	aggregate := sourcePrivacy{Version: 1,
		CoverageVersion:           1,
		Scope:                     h.scope,
		SourceID:                  h.source.SourceID,
		Payload:                   h.source.Payload,
		State:                     "standard",
		MessageMembershipComplete: true}
	for _, message := range h.messages {
		aggregate.MessageIDs = append(aggregate.MessageIDs, message.MessageID)
	}

	return putCorruptPrivacyAggregate(t, ctx, tx, h, aggregate, kind)
}

func putCorruptPrivacyAggregate(t *testing.T, ctx context.Context, tx *Tx,
	h privacyHistory, aggregate sourcePrivacy, kind string,
) error {
	t.Helper()

	switch kind {
	case "aggregate-subset":
		aggregate.MessageIDs = aggregate.MessageIDs[:1]
	case "privacy-combination":
		aggregate.StickyUnknown = true
	case "message-payload":
		return tx.Put(ctx,
			Key{Family: Payload,
				Parts: [][]byte{[]byte(h.messages[0].Payload.ID)}},
			envelope(t,
				Payload,
				[]byte("different raw message")))
	case "source-scope":
		h.source.Scope.Kind = memory.ScopeKindGroup

		return tx.Put(ctx,
			Key{Family: Source,
				Scope: h.scope,
				Parts: [][]byte{[]byte(h.source.SourceID)}},
			envelope(t,
				Source,
				h.source))
	case "scope-metadata":
		scope := h.scope
		scope.Kind = memory.ScopeKindGroup

		return tx.Put(ctx,
			Key{Family: SourcePrivacyScope,
				Scope: h.scope,
				Parts: [][]byte{[]byte("coverage")}},
			envelope(t,
				SourcePrivacyScope,
				sourcePrivacyScope{Version: 1,
					Scope: scope}))
	case "orphan-aggregate":
		aggregate.SourceID = "missing-source"
	}

	return tx.Put(ctx,
		Key{Family: SourcePrivacy,
			Scope: h.scope,
			Parts: [][]byte{[]byte(aggregate.SourceID)}},
		envelope(t,
			SourcePrivacy,
			aggregate))
}
