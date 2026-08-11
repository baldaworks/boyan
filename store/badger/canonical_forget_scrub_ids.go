package badger

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func normalizeDeniedIDs(ids []string, kind string) (map[string]struct{}, error) {
	denied := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != id || id == "" {
			return nil, memory.PermanentError(memory.CodeInvalidDerived, "canonical forget "+kind+" identity is invalid", nil)
		}
		denied[id] = struct{}{}
	}
	return denied, nil
}
