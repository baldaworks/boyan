package memory

func (c IngestSequenceCursor) Validate() error {
	if c.SchemaVersion != IngestSequenceSchemaVersionV1 {
		return invalidDerived("ingest sequence cursor schema version is invalid")
	}
	if !isCanonicalID(c.ProducerID) || c.Latest == 0 {
		return invalidDerived("ingest sequence cursor identity is invalid")
	}
	return c.Scope.Validate()
}
