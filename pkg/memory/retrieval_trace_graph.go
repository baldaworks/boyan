package memory

func validateTraceGraph(request TraceRequest, graph TraceGraph) error {
	if err := validateTraceGraphShape(request, graph); err != nil {
		return err
	}
	index, err := indexTraceGraph(request, graph)
	if err != nil {
		return err
	}
	return validateTraceGraphClosure(request.Root, index)
}

func validateTraceGraphShape(request TraceRequest, graph TraceGraph) error {
	if graph.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported trace graph schema version")
	}
	if graph.Scope != request.Scope || graph.Root != request.Root {
		return PermanentError(CodeScopeViolation, "trace graph does not match the request", nil)
	}
	if len(graph.Revisions)+len(graph.Sources) > request.MaxNodes {
		return limitExceeded("trace graph exceeds the node limit")
	}
	return nil
}
