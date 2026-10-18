package canonicalsql

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const canonicalRecordComponents = 4

func privacySourceDenied(ctx context.Context, tx *Tx, scope memory.Scope, id string) bool {
	denied, err := privacyDenied(ctx, tx, scope, DeniedSource, id)

	return err == nil && denied
}

func privacyComponents(key Key) []string {
	if key.Family == SourcePrivacyReady {
		return []string{string(key.Family)}
	}

	return []string{string(key.Family), key.Scope.Key, string(key.Scope.Kind), string(key.Parts[0])}
}

func validatePrivacyRow(ctx context.Context, tx *Tx, key Key, _ memory.Scope, _ string) error {
	data, err := tx.Get(ctx, key)
	if err != nil {
		return err
	}

	envelope, err := memory.UnmarshalRecordEnvelope(data)
	if err != nil {
		return invalidSourcePrivacy()
	}

	components := privacyComponents(key)
	if err := validateCanonicalPrivacyRecord(components, envelope); err != nil {
		return err
	}

	return validateStoredPrivacyBindings(ctx, tx, components, envelope)
}

func privacyKey(scope memory.Scope, family Family, id string) (Key, error) {
	key := Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(id)}}
	_, _, err := key.values()

	return key, err
}

func payloadRecordKey(ref memory.PayloadRef) (Key, error) {
	if err := ref.Validate(); err != nil {
		return Key{}, err
	}

	key := Key{Family: Payload, Parts: [][]byte{[]byte(ref.ID)}}
	_, _, err := key.values()

	return key, err
}

func privacyMissing(err error) bool {
	code, _, ok := memory.ClassifyError(err)

	return ok && code == memory.CodeNotFound
}

func getPrivacyRecord(ctx context.Context, tx *Tx, key Key, family Family, target any) error {
	data, err := tx.Get(ctx, key)
	if err != nil {
		return err
	}

	if key.Family != family {
		return invalidSourcePrivacy()
	}

	payload, err := decodePayload(family, data)
	if err != nil {
		return err
	}

	if isCanonicalProofRecord(string(family)) {
		if err := validateCanonicalProofRecord(privacyComponents(key),
			memory.RecordEnvelope{RecordType: string(family),
				Payload: payload}); err != nil {
			return err
		}
	}

	decode := json.Unmarshal
	if isCanonicalPrivacyRecord(string(family)) {
		decode = decodeCanonicalProofJSON
	}

	if err := decode(payload, target); err != nil {
		return invalidSourcePrivacy()
	}

	return nil
}

func putPrivacyRecord(ctx context.Context, tx *Tx, key Key, family Family, value any) error {
	if key.Family != family {
		return tx.fail(invalidSourcePrivacy())
	}

	data, err := Encode(family, value)
	if err != nil {
		return tx.fail(err)
	}

	return tx.Put(ctx, key, data)
}

func decodeCanonicalProofJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalidSourcePrivacy()
	}

	return nil
}

type canonicalTurnSourcePayload struct {
	ExportID            string                    `json:"export_id"`
	ConversationID      string                    `json:"conversation_id"`
	AgentConversationID string                    `json:"agent_conversation_id"`
	SourceTurnID        string                    `json:"source_turn_id"`
	CompletedAt         time.Time                 `json:"completed_at"`
	TerminalStatus      memory.TurnTerminalStatus `json:"terminal_status"`
}

func loadTurnCertificate(ctx context.Context,
	tx *Tx,
	scope memory.Scope,
	id string) (memory.CanonicalTurnCertificate,
	bool,
	error) {
	var certificate memory.CanonicalTurnCertificate

	key, err := privacyKey(scope, TurnCertificate, id)
	if err != nil {
		return certificate, false, err
	}

	err = getPrivacyRecord(ctx, tx, key, TurnCertificate, &certificate)
	if privacyMissing(err) {
		return certificate, false, nil
	}

	if err != nil || certificate.Validate() != nil || certificate.SourceID != id {
		return certificate, false, invalidSourcePrivacy()
	}

	return certificate, true, nil
}

func putCanonicalMutationEvidenceEdges(ctx context.Context,
	tx *Tx,
	scope memory.Scope,
	revision memory.MemoryRevision) error {
	for _, evidence := range revision.Evidence {
		key := Key{Family: SourceRevision,
			Scope: scope,
			Parts: [][]byte{[]byte(evidence.SourceID),
				[]byte(revision.RevisionID)}}

		edge := struct {
			ChildRevisionID  string `json:"child_revision_id"`
			ParentRevisionID string `json:"parent_revision_id"`
		}{revision.RevisionID, evidence.SourceID}
		if err := putImmutablePrivacyRecord(ctx, tx, key, edge); err != nil {
			return err
		}
	}

	return nil
}

func putImmutablePrivacyRecord(ctx context.Context, tx *Tx, key Key, value any) error {
	encoded, err := Encode(key.Family, value)
	if err != nil {
		return tx.fail(err)
	}

	previous, err := tx.Get(ctx, key)
	if err == nil {
		if !bytes.Equal(previous, encoded) {
			return tx.fail(memory.PermanentError(memory.CodeConflict, "canonical immutable content differs", nil))
		}

		return nil
	}

	if !privacyMissing(err) {
		return err
	}

	return tx.Put(ctx, key, encoded)
}
