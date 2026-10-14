package derivation

import (
	"encoding/hex"
	"io"
)

func processorRunStateID(entropy io.Reader) (string, error) {
	var random [32]byte
	if _, err := io.ReadFull(entropy, random[:]); err != nil {
		return "", err
	}

	return "boyan-derivation-" + hex.EncodeToString(random[:]), nil
}
