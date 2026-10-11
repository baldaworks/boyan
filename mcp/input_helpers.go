package mcp

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func parseScopeInput(in ScopeInput) (memory.Scope, error) {
	scope := memory.Scope{
		Key:  strings.TrimSpace(in.ScopeKey),
		Kind: in.ScopeKind,
	}
	if err := scope.Validate(); err != nil {
		return memory.Scope{}, err
	}

	return scope, nil
}

func normalizeSearchInput(in SearchInput) (string, int, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" || len(query) > memory.MaxSearchQueryBytes {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}

	limit := in.Limit
	if limit == 0 {
		limit = memory.DefaultSearchLimit
	}

	if limit < 1 || limit > memory.MaxSearchLimit {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}

	if err := validateSearchFilters(in); err != nil {
		return "", 0, err
	}

	return query, limit, nil
}

func validateSearchFilters(in SearchInput) error {
	if in.MemoryKind != nil && *in.MemoryKind != memory.MemoryKindState &&
		*in.MemoryKind != memory.MemoryKindEvent {
		return memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}

	if in.Category != nil {
		if err := in.Category.Validate(); err != nil {
			return memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
		}
	}

	if err := validateSearchFilterIdentities(in.SourceID, in.ConversationID, in.MemoryKey); err != nil {
		return err
	}

	if in.AsOf != nil && in.AsOf.IsZero() {
		return memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}

	return nil
}

func validateSearchFilterIdentities(values ...string) error {
	for _, value := range values {
		if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
			return memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
		}
	}

	return nil
}
