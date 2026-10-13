package projectionsync

import (
	"errors"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// ErrProjectionRebuildRequired identifies a durable change that cannot be
// represented by bounded replay deltas. Callers must rebuild or remain dirty.
var ErrProjectionRebuildRequired = errors.New("projection requires canonical rebuild")

func checkProjectionRebuildRequired(changes []ScopeChange) error {
	for _, change := range changes {
		if change.RequiresRebuild {
			return RetryableError(
				CodeConflict,
				"projection delta requires a canonical rebuild",
				ErrProjectionRebuildRequired,
			)
		}
	}

	return nil
}
