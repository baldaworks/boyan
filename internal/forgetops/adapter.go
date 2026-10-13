package forgetops

import (
	"fmt"

	"github.com/baldaworks/boyan/store/badger"
)

type CanonicalForgetAdapter struct {
	store *badger.CanonicalStore
}

func NewCanonicalForgetAdapter(store *badger.CanonicalStore) (*CanonicalForgetAdapter, error) {
	if store == nil {
		return nil, fmt.Errorf("canonical forget store is required")
	}

	return &CanonicalForgetAdapter{store: store}, nil
}
