package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) validateProfileCandidateGrounding(
	state profileGroundingState,
	candidate ProfileCandidate,
) error {
	if len(candidate.Atoms)+len(candidate.Scenarios) > e.config.MaxSourcesPerRevision {
		return limitExceeded("profile candidate exceeds the configured provenance limit")
	}

	for _, atom := range candidate.Atoms {
		if _, ok := state.activeAtoms[atom]; !ok {
			return invalidDerived("profile candidate references a non-active same-scope atom")
		}
	}

	for _, scenario := range candidate.Scenarios {
		if _, ok := state.activeScenarios[scenario]; !ok {
			return invalidDerived("profile candidate references a non-active same-scope scenario")
		}
	}

	return nil
}
