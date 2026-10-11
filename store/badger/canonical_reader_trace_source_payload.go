package badger

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func decodeCanonicalTraceTurnPayload(payload []byte) (canonicalTurnSourcePayload, error) {
	var turnPayload canonicalTurnSourcePayload
	if err := json.Unmarshal(payload, &turnPayload); err != nil ||
		turnPayload.ExportID == "" ||
		turnPayload.ConversationID == "" ||
		turnPayload.SourceTurnID == "" {
		return canonicalTurnSourcePayload{}, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical trace source payload is not representable",
			nil,
		)
	}

	return turnPayload, nil
}
