package canonicalcore

import memory "github.com/baldaworks/boyan/pkg/memory"

func validateEvidenceRefs(evidence []memory.EvidenceRef) error {
	seen := make(map[memory.EvidenceRef]struct{}, len(evidence))
	for _, ref := range evidence {
		if err := ref.Validate(); err != nil {
			return err
		}

		if _, exists := seen[ref]; exists {
			return invalidDerived("memory evidence contains a duplicate reference")
		}

		seen[ref] = struct{}{}
	}

	return nil
}
