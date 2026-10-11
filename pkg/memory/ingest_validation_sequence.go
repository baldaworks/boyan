package memory

func (r SequencedTurn) Validate() error {
	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.ScopeSequence == 0 {
		return invalidDerived("sequenced turn identity is invalid")
	}

	return r.Turn.Validate()
}

func (r SequencedBoundary) Validate() error {
	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.ScopeSequence == 0 {
		return invalidDerived("sequenced boundary identity is invalid")
	}

	return r.Boundary.Validate()
}
