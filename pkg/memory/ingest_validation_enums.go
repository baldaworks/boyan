package memory

func (k IngestKind) Validate() error {
	switch k {
	case IngestKindTurn, IngestKindBoundary:
		return nil
	default:
		return invalidDerived("ingest kind is invalid")
	}
}

func (d IngestDisposition) Validate() error {
	switch d {
	case IngestDispositionAccepted, IngestDispositionDuplicate:
		return nil
	default:
		return invalidDerived("ingest disposition is invalid")
	}
}

func (s ProjectionStatus) Validate() error {
	switch s {
	case ProjectionStatusCurrent, ProjectionStatusLagging:
		return nil
	default:
		return invalidDerived("projection status is invalid")
	}
}
