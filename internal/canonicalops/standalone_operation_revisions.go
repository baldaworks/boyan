package canonicalops

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func collectOperationRevisions(request memory.CommitRequest) []memory.RevisionRef {
	revisions := make(
		[]memory.RevisionRef,
		0,
		len(request.Atoms)+len(request.Scenarios)+len(request.Profiles),
	)
	for _, atom := range request.Atoms {
		revisions = append(
			revisions,
			memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID},
		)
	}

	for _, scenario := range request.Scenarios {
		revisions = append(
			revisions,
			memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID},
		)
	}

	for _, profile := range request.Profiles {
		revisions = append(
			revisions,
			memory.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID},
		)
	}

	return revisions
}

func operationCommittedAt(request memory.CommitRequest) time.Time {
	return request.ProcessingAt
}
