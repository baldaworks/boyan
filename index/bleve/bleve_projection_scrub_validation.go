package bleve

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateForgetScrub(scope memory.Scope, sourceIDs, revisionIDs []string) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if len(sourceIDs) > memory.MaxSnapshotItems ||
		len(revisionIDs) > memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "bleve forget scrub bound exceeded", nil)
	}

	if len(sourceIDs) == 0 && len(revisionIDs) == 0 {
		return nil
	}

	for _, id := range append(append([]string(nil), sourceIDs...), revisionIDs...) {
		if strings.TrimSpace(id) != id || id == "" {
			return memory.PermanentError(memory.CodeInvalidDerived, "bleve forget scrub identity is invalid", nil)
		}
	}

	return nil
}
