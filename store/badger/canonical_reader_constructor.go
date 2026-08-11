package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

// NewCanonicalReader constructs the canonical read adapter for one Badger
// owner. The reader does not open a second database handle.
func NewCanonicalReader(store *CanonicalStore) (*CanonicalReader, error) {
	if store == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical reader store is required", nil)
	}
	return &CanonicalReader{store: store}, nil
}
