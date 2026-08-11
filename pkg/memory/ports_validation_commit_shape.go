package memory

func validateCommitRequestShape(r CommitRequest) error {
	if r.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported commit request schema version")
	}
	if !isCanonicalID(r.OperationID) {
		return invalidDerived("commit operation id is required")
	}
	if err := r.Stage.Validate(); err != nil {
		return err
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if len(r.Sources) > 1 {
		return limitExceeded("commit contains too many source writes")
	}
	if len(r.Atoms)+len(r.Scenarios)+len(r.Profiles) > MaxCandidateCount {
		return limitExceeded("commit contains too many derived revisions")
	}
	return validateCommitStageWrites(r)
}

func validateCommitStageWrites(r CommitRequest) error {
	switch r.Stage {
	case OperationStageAtoms:
		if len(r.Scenarios) != 0 || len(r.Profiles) != 0 {
			return invalidDerived("atom-stage commit cannot contain aggregate revisions")
		}
	case OperationStageScenarios:
		if len(r.Sources) != 0 || len(r.Atoms) != 0 || len(r.Profiles) != 0 {
			return invalidDerived("scenario-stage commit contains unsupported writes")
		}
	case OperationStageProfile:
		if len(r.Sources) != 0 || len(r.Atoms) != 0 || len(r.Scenarios) != 0 || len(r.Profiles) > 1 {
			return invalidDerived("profile-stage commit contains unsupported writes")
		}
	}
	return nil
}
