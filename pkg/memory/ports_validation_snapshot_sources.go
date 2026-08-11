package memory

func validateScopeSnapshotSources(scope Scope, sources []SourceRecord) error {
	sourceSeen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Ref.Scope != scope {
			return PermanentError(CodeScopeViolation, "source record scope does not match snapshot scope", nil)
		}
		if _, ok := sourceSeen[source.Ref.ExportID]; ok {
			return invalidDerived("scope snapshot contains duplicate source identity")
		}
		sourceSeen[source.Ref.ExportID] = struct{}{}
	}
	return nil
}
