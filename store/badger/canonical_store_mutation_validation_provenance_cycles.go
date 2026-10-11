package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func validateBadgerMutationProvenanceCycles(revisions map[string]memory.MemoryRevision) error {
	colors := make(map[string]uint8, len(revisions))

	var visit func(string) error

	visit = func(revisionID string) error {
		switch colors[revisionID] {
		case provenanceVisiting:
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical provenance cycle", nil)
		case provenanceVisited:
			return nil
		}

		revision, exists := revisions[revisionID]
		if !exists {
			return nil
		}

		colors[revisionID] = provenanceVisiting

		for _, parentID := range revision.Parents {
			if err := visit(parentID); err != nil {
				return err
			}
		}

		colors[revisionID] = provenanceVisited

		return nil
	}
	for revisionID := range revisions {
		if err := visit(revisionID); err != nil {
			return err
		}
	}

	return nil
}
