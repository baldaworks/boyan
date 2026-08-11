package structuredderive

import (
	"crypto/sha256"
	"encoding/hex"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func operationID(stage memory.OperationStage, exportID string, derivation memory.DerivationRef) (string, error) {
	return memory.ProcessingOperationID(stage, exportID, normalizeDerivation(derivation))
}

func normalizeDerivation(derivation memory.DerivationRef) memory.DerivationRef {
	if derivation == (memory.DerivationRef{}) {
		return memory.LegacyDerivationRef()
	}
	return derivation
}

// ProcessorRunID derives a deterministic short-lived model run ID from an
// operation and stage without retaining conversation state.
func ProcessorRunID(operationID, stage string) string {
	hash := sha256.Sum256([]byte(operationID + "\x00" + stage))
	return "boyan-processor-" + hex.EncodeToString(hash[:])
}
