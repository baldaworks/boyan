package canonicalcore

import (
	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) bindCanonicalTurnIdentity(
	turn Turn, mutation *CanonicalMutation, proof *CanonicalTurnIdentityResult,
) error {
	if proof == nil {
		return nil
	}

	fingerprint, err := CanonicalTurnFingerprint(turn)
	if err != nil {
		return err
	}

	ids := make([]string, len(turn.Messages))
	for index, message := range turn.Messages {
		ids[index] = message.MessageID
	}

	mutation.TurnIdentity = &CanonicalTurnIdentityCommit{
		Certificate: CanonicalTurnCertificate{Version: CanonicalTurnFingerprintVersion,
			SourceID: turn.SourceID, Fingerprint: fingerprint, MessageIDs: ids}, Turn: turn,
	}
	if proof.Found {
		if err := preserveCertifiedTurnRecords(turn, mutation, *proof); err != nil {
			return err
		}
	}

	return mutation.Validate()
}

func preserveCertifiedTurnRecords(turn Turn, mutation *CanonicalMutation, proof CanonicalTurnIdentityResult) error {
	if err := validateCertifiedTurnRecords(turn, proof); err != nil {
		return err
	}

	omit := make(map[string]struct{}, len(mutation.Sources)+len(mutation.Messages))
	for _, source := range mutation.Sources {
		omit[source.Payload.ID] = struct{}{}
	}

	for _, message := range mutation.Messages {
		omit[message.Payload.ID] = struct{}{}
	}

	payloads := mutation.Payloads[:0]
	for _, payload := range mutation.Payloads {
		if _, existing := omit[payload.Ref.ID]; !existing {
			payloads = append(payloads, payload)
		}
	}

	mutation.Payloads = payloads
	mutation.Sources = []CanonicalSourceRecord{proof.Source}
	mutation.Messages = append([]MessageRecord(nil), proof.Messages...)

	return nil
}

func validateCertifiedTurnRecords(turn Turn, proof CanonicalTurnIdentityResult) error {
	if proof.Source.Scope != turn.Scope || proof.Source.SourceID != turn.SourceID {
		return PermanentError(CodeScopeViolation, "verified canonical source identity differs", nil)
	}

	if len(proof.Messages) != len(turn.Messages) {
		return PermanentError(CodeStoreFailure, "verified canonical message membership differs", nil)
	}

	for index, message := range proof.Messages {
		expected := turn.Messages[index]
		if message.MessageID != expected.MessageID || message.Role != expected.Role || message.SourceID != turn.SourceID {
			return PermanentError(CodeStoreFailure, "verified canonical message identity differs", nil)
		}
	}

	return nil
}
