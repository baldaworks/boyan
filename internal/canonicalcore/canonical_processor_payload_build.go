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

// canonicalScopedPayloadID isolates disposable raw blobs even when legacy turn
// exports share their identity between scope kinds. Stable operation IDs stay unchanged.
func canonicalScopedPayloadID(scope Scope, kind, identity string) string {
	return reconciliationID("payload", scope.Key, string(scope.Kind), kind, identity)
}
