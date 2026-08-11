package canonicalops

import (
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func boundaryParentEvidence(parents []memory.RevisionRef, loaded map[string]memory.MemoryRevision) ([]memory.EvidenceRef, error) {
	seen := make(map[string]struct{})
	evidence := make([]memory.EvidenceRef, 0, len(parents))
	for _, parent := range parents {
		revision, ok := loaded[parent.RevisionID]
		if !ok {
			return nil, fmt.Errorf("canonical parent revision %s is unavailable", parent.RevisionID)
		}
		for _, ref := range revision.Evidence {
			key := evidenceKey(ref)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			evidence = append(evidence, ref)
		}
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("boundary commit parent evidence is empty")
	}
	if len(evidence) > memory.MaxSourcesPerRevision {
		return nil, fmt.Errorf("boundary commit evidence exceeds %d references", memory.MaxSourcesPerRevision)
	}
	return evidence, nil
}
