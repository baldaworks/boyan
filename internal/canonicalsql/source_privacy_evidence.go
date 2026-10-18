package canonicalsql

import memory "github.com/baldaworks/boyan/pkg/memory"

func validatePrivacyMessage(message memory.MessageRecord, ref memory.EvidenceRef) (bool, error) {
	if message.Validate() != nil || message.MessageID != ref.MessageID {
		return false, invalidSourcePrivacy()
	}

	if message.SourceID != ref.SourceID || message.Role != ref.Role || ref.EndByte > message.Payload.ByteSize {
		return false, memory.PermanentError(memory.CodeInvalidDerived,
			"canonical privacy evidence ownership is invalid", nil)
	}

	return ref.TextDigest != "" && ref.TextDigest == message.Payload.Digest, nil
}
