package memory

import (
	"math"
)

// Validate verifies one Store search hit and returns its full revision metadata.
func (h SearchHit) Validate() (RevisionMeta, error) {
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
	if h.Score != nil && (math.IsNaN(*h.Score) || math.IsInf(*h.Score, 0)) {
		return RevisionMeta{}, invalidDerived("search hit score must be finite")
	}
	return meta, nil
}
