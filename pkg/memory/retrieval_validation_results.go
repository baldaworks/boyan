package memory

import (
	"math"
)

// Validate verifies one Store search hit and returns its full revision metadata.
func (h SearchHit) Validate() (RevisionMeta, error) {
	if h.Canonical != nil {
		return RevisionMeta{}, invalidDerived("canonical revisions are trace-only")
	}

	populated := 0

	var meta RevisionMeta

	if h.Atom != nil {
		populated++

		if err := h.Atom.Validate(); err != nil {
			return RevisionMeta{}, err
		}

		meta = h.Atom.Meta
	}

	if h.Scenario != nil {
		populated++

		if err := h.Scenario.Validate(); err != nil {
			return RevisionMeta{}, err
		}

		meta = h.Scenario.Meta
	}

	if h.Profile != nil {
		populated++

		if err := h.Profile.Validate(); err != nil {
			return RevisionMeta{}, err
		}

		meta = h.Profile.Meta
	}

	if populated != 1 {
		return RevisionMeta{}, invalidDerived("search hit must contain exactly one derived revision")
	}

	if err := validateSearchHitScore(h.Score); err != nil {
		return RevisionMeta{}, err
	}

	return meta, nil
}

func validateSearchHitScore(score *float64) error {
	if score != nil && (math.IsNaN(*score) || math.IsInf(*score, 0)) {
		return invalidDerived("search hit score must be finite")
	}

	return nil
}
