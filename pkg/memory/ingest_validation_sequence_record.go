package memory

func (r IngestSequenceRecord) Validate() error {
	if r.SchemaVersion != IngestSequenceSchemaVersionV1 {
		return invalidDerived("ingest sequence record schema version is invalid")
	}

	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.Sequence == 0 {
		return invalidDerived("ingest sequence record identity is invalid")
	}

	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if err := r.Outcome.Validate(); err != nil {
		return err
	}

	return r.validateOutcomeKey()
}

func (r IngestSequenceRecord) validateOutcomeKey() error {
	if r.Outcome.ProducerID != r.ProducerID ||
		r.Outcome.Scope != r.Scope ||
		r.Outcome.ScopeSequence != r.Sequence ||
		r.Outcome.Fingerprint != r.Fingerprint {
		return invalidDerived("ingest sequence record outcome does not match its key")
	}

	if r.Outcome.Disposition != IngestDispositionAccepted {
		return invalidDerived("ingest sequence record must store an accepted outcome")
	}

	switch r.Outcome.Kind {
	case IngestKindTurn, IngestKindBoundary:
		return nil
	default:
		return invalidDerived("ingest sequence record kind is invalid")
	}
}
