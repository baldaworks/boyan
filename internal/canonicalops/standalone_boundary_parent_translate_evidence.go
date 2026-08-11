package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func buildBoundaryParentEvidenceSets(
	parentSets map[string][]memory.RevisionRef,
	loaded map[string]memory.MemoryRevision,
) (map[string][]memory.EvidenceRef, error) {
	evidenceSets := make(map[string][]memory.EvidenceRef, len(parentSets))
	for revisionID, parents := range parentSets {
		evidence, err := boundaryParentEvidence(parents, loaded)
		if err != nil {
			return nil, err
		}
		evidenceSets[revisionID] = evidence
	}
	return evidenceSets, nil
}
