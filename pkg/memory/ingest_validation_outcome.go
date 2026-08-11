package memory

func (o IngestOutcome) Validate() error {
	if o.SchemaVersion != IngestOutcomeSchemaVersionV1 {
		return invalidDerived("ingest outcome schema version is invalid")
	}
	if err := o.Kind.Validate(); err != nil {
		return err
	}
	if err := o.Disposition.Validate(); err != nil {
		return err
	}
	if err := o.Projection.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(o.ProducerID) ||
		!isCanonicalID(o.Fingerprint) ||
		!isCanonicalID(o.ExportID) ||
		o.ScopeSequence == 0 ||
		o.ScopeVersion == 0 ||
		o.ScopeChangeSeq == 0 {
		return invalidDerived("ingest outcome identity is invalid")
	}
	if err := o.Scope.Validate(); err != nil {
		return err
	}
	return validateUniqueCanonicalIDs(o.OperationIDs, "ingest operation")
}
