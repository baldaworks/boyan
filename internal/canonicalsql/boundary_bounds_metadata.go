package canonicalsql

import (
	"encoding/json"
	"time"

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
func cloneBoundaryTemporal(temporal memory.Temporal) memory.Temporal {
	clone := func(value *time.Time) *time.Time {
		if value == nil {
			return nil
		}

		at := *value

		return &at
	}
	temporal.ValidFrom = clone(temporal.ValidFrom)
	temporal.ValidUntil = clone(temporal.ValidUntil)
	temporal.ExpiresAt = clone(temporal.ExpiresAt)
	temporal.EventAt = clone(temporal.EventAt)

	return temporal
}
