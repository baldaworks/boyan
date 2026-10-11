package badger

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func canonicalCompatibilityPayload(payload []byte, scope memory.Scope) (memory.CanonicalCompatibilityPayload, bool) {
	var compatibility memory.CanonicalCompatibilityPayload
	if err := json.Unmarshal(payload, &compatibility); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false
	}

	if err := compatibility.Validate(scope); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false
	}

	return compatibility, true
}
