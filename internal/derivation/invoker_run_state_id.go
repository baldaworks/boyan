package derivation

import (
	"crypto/sha256"
	"encoding/hex"
)

func processorRunStateID(operationID, stage string) string {
	hash := sha256.Sum256([]byte(operationID + "\x00" + stage))

	return "boyan-derivation-" + hex.EncodeToString(hash[:])
}
