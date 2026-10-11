package projectionsync

import (
	"fmt"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// ScopeViewBuilder reads canonical state in bounded pages. It never loads a
// full scope and is suitable for Scenario/Profile or retrieval projection
// rebuilds.
type ScopeViewBuilder struct {
	canonical CanonicalStore
}

// NewScopeViewBuilder constructs a storage-neutral bounded-view builder.
func NewScopeViewBuilder(canonical CanonicalStore) (*ScopeViewBuilder, error) {
	if canonical == nil {
		return nil, fmt.Errorf("canonical store is required")
	}

	return &ScopeViewBuilder{canonical: canonical}, nil
}
