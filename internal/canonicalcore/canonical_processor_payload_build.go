package canonicalcore

import (
	"crypto/sha256"
	"encoding/hex"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) payload(payloadID string, plaintext []byte) (CanonicalPayload, PayloadRef, error) {
	if len(plaintext) == 0 {
		return CanonicalPayload{}, PayloadRef{}, invalidDerived("canonical payload is empty")
	}

	digest := sha256.Sum256(plaintext)
	ref := PayloadRef{
		ID:       payloadID,
		Digest:   hex.EncodeToString(digest[:]),
		ByteSize: uint32(len(plaintext)),
	}

	payload := CanonicalPayload{Ref: ref, Data: append([]byte(nil), plaintext...)}
	if err := payload.Validate(); err != nil {
		return CanonicalPayload{}, PayloadRef{}, err
	}

	return payload, ref, nil
}

func canonicalPayloadID(kind, identity string) string {
	return reconciliationID("payload", kind, identity)
}
