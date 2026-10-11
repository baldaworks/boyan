package badger

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateCanonicalRevisionRefPage(scope memory.Scope, afterRevisionID string, limit uint32) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if limit == 0 || limit > 512 ||
		(afterRevisionID != "" && strings.TrimSpace(afterRevisionID) != afterRevisionID) {
		return memory.PermanentError(memory.CodeLimitExceeded, "canonical revision enumeration limit is invalid", nil)
	}

	return nil
}
