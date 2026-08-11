package memory

// NormalizeTraceRequest applies the configured graph bound and validates the request.
func NormalizeTraceRequest(request TraceRequest, defaultMaxNodes int) (TraceRequest, error) {
	if request.SchemaVersion == "" {
		request.SchemaVersion = DerivedSchemaVersionV1
	}
	if request.MaxNodes == 0 {
		request.MaxNodes = defaultMaxNodes
	}
	if err := request.Validate(); err != nil {
		return TraceRequest{}, err
	}
	return request, nil
}

// Validate verifies one exact-scope bounded trace request.
func (r TraceRequest) Validate() error {
	if r.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported trace request schema version")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := r.Root.Validate(); err != nil {
		return err
	}
	if r.MaxNodes < 1 || r.MaxNodes > MaxTraceNodes {
		return limitExceeded("trace node limit is outside the allowed range")
	}
	return nil
}

// Validate verifies a closed, bounded trace response and its untrusted marker.
func (r TraceResponse) Validate(maxNodes int) error {
	if r.SchemaVersion != DerivedSchemaVersionV1 || r.Trust != ReferenceTrustUntrusted {
		return invalidDerived("trace response schema or trust marker is invalid")
	}
	request := TraceRequest{
		SchemaVersion: DerivedSchemaVersionV1,
		Scope:         r.Scope,
		Root:          r.Root,
		MaxNodes:      maxNodes,
	}
	if err := request.Validate(); err != nil {
		return err
	}
	return validateTraceGraph(request, TraceGraph{
		SchemaVersion: DerivedSchemaVersionV1,
		Scope:         r.Scope,
		Root:          r.Root,
		Revisions:     r.Revisions,
		Sources:       r.Sources,
	})
}
