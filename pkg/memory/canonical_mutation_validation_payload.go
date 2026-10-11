package memory

import (
	"crypto/sha256"
	"encoding/hex"
)

// Validate verifies a payload blob can safely be persisted atomically with a
// canonical mutation. Digest checking is an integrity/identity check; payload
// protection is deliberately outside this contract.
func (p CanonicalPayload) Validate() error {
	if err := p.Ref.Validate(); err != nil {
		return err
	}

	if len(p.Data) == 0 || len(p.Data) != int(p.Ref.ByteSize) {
		return invalidDerived("canonical payload bytes are invalid")
	}

	digest := sha256.Sum256(p.Data)
	if hex.EncodeToString(digest[:]) != p.Ref.Digest {
		return invalidDerived("canonical payload digest does not match bytes")
	}

	return nil
}
