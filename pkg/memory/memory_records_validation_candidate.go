package memory

import "strings"

func (c MemoryCandidate) Validate() error {
	if c.Kind != MemoryKindState && c.Kind != MemoryKindEvent {
		return invalidDerived("unsupported memory candidate kind")
	}
	if strings.TrimSpace(c.Statement) != c.Statement || c.Statement == "" || len(c.Statement) > maxMemoryTextBytes {
		return invalidDerived("memory candidate statement is invalid")
	}
	if err := c.Temporal.Validate(); err != nil {
		return err
	}
	if len(c.Evidence) == 0 || len(c.Evidence) > MaxSourcesPerRevision {
		return invalidDerived("memory candidate evidence is invalid")
	}
	if err := validateEvidenceRefs(c.Evidence); err != nil {
		return err
	}
	return validateSensitivityRetention(c.Sensitivity, c.Retention)
}
