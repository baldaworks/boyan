package memory

func (e IngestEnvelope[T]) Validate(validatePayload func(T) error) error {
	if e.SchemaVersion != IngestHTTPSchemaVersionV1 {
		return invalidDerived("ingest envelope schema version is invalid")
	}

	if !isCanonicalID(e.ProducerID) || e.ScopeSequence == 0 {
		return invalidDerived("ingest envelope identity is invalid")
	}

	if validatePayload == nil {
		return invalidDerived("ingest payload validator is required")
	}

	return validatePayload(e.Payload)
}

func (r IngestReceipt) Validate() error {
	if r.SchemaVersion != IngestReceiptSchemaVersionV1 {
		return invalidDerived("ingest receipt schema version is invalid")
	}

	if !isCanonicalID(r.ExportID) || r.ScopeVersion == 0 || r.ScopeChangeSeq == 0 {
		return invalidDerived("ingest receipt identity is invalid")
	}

	if err := r.Disposition.Validate(); err != nil {
		return err
	}

	if err := r.Projection.Validate(); err != nil {
		return err
	}

	if r.OperationID != "" && !isCanonicalID(r.OperationID) {
		return invalidDerived("ingest receipt operation id is invalid")
	}

	return validateUniqueCanonicalIDs(r.OperationIDs, "ingest receipt operation")
}

func (r IngestErrorResponse) Validate() error {
	if r.Code == "" || r.Class == "" || r.Message == "" {
		return invalidDerived("ingest error response is invalid")
	}

	if r.RetryAfterMS != nil && *r.RetryAfterMS < 0 {
		return invalidDerived("ingest retry delay is invalid")
	}

	return nil
}
