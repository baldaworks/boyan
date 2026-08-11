package canonicalops

import (
	"fmt"
	"github.com/baldaworks/boyan/store/badger"
)

type CanonicalBoundaryStore struct {
	store  *badger.CanonicalStore
	reader *badger.CanonicalReader
}

func NewCanonicalBoundaryStore(store *badger.CanonicalStore, reader *badger.CanonicalReader) (*CanonicalBoundaryStore, error) {
	if store == nil || reader == nil {
		return nil, fmt.Errorf("canonical boundary store dependencies are required")
	}
	return &CanonicalBoundaryStore{store: store, reader: reader}, nil
}
