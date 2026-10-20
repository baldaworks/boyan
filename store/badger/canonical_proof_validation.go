package badger

import (
	"bytes"
	"encoding/json"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// validateCanonicalProofRecord validates bounded structural metadata only.
// Legacy revision bodies remain opaque when no inherited trust is asserted.
func decodeCanonicalProofJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalidCanonicalProof()
	}

	return nil
}

func invalidCanonicalProof() error {
	return memory.PermanentError(memory.CodeInvalidDerived, "canonical structural proof metadata is invalid", nil)
}
