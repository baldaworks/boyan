package memory

import (
	"strings"
)

// Validate verifies a portable item does not accept model-owned identity.
func (i MemoryItem) Validate() error {
	if !isCanonicalID(i.ItemID) {
		return invalidDerived("memory item id is required")
	}
	if err := i.Scope.Validate(); err != nil {
		return err
	}
	switch i.Kind {
	case MemoryKindState:
		if !isCanonicalID(string(i.MemoryKey)) {
			return invalidDerived("state memory key is required")
		}
	case MemoryKindEvent:
		if i.MemoryKey != "" {
			return invalidDerived("event memory cannot have a stable memory key")
		}
	default:
		return invalidDerived("unsupported memory kind")
	}
	return nil
}

// Validate verifies temporal ranges and their source representation.
func (t Temporal) Validate() error {
	if t.ObservedAt.IsZero() {
		return invalidDerived("observed time is required")
	}
	if t.ValidFrom != nil && t.ValidUntil != nil && t.ValidUntil.Before(*t.ValidFrom) {
		return invalidDerived("valid interval is inverted")
	}
	if len(t.TemporalText) > maxMemoryTextBytes || strings.TrimSpace(t.TemporalText) != t.TemporalText {
		return invalidDerived("temporal text is invalid")
	}
	return nil
}
