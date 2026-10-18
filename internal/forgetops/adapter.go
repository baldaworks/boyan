package forgetops

import (
	"context"
	"fmt"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// CanonicalForgetStore is the authority actually consumed by the forget adapter.
type CanonicalForgetStore interface {
	memory.CanonicalSourceIdentityResolver
	RunScopeErase(ctx context.Context, request memory.ScopeEraseRequest) (memory.ScopeEraseJournal, error)
	LoadScopeErase(ctx context.Context, scope memory.Scope, operationID string) (memory.ScopeEraseJournal, bool, error)
	LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error)
}

type CanonicalForgetAdapter struct {
	store CanonicalForgetStore
}

func NewCanonicalForgetAdapter(store CanonicalForgetStore) (*CanonicalForgetAdapter, error) {
	if store == nil || reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil() {
		return nil, fmt.Errorf("canonical forget store is required")
	}

	return &CanonicalForgetAdapter{store: store}, nil
}
