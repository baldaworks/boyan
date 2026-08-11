package memoryops

import memory "github.com/baldaworks/boyan/pkg/memory"

// NewForgetService constructs a forget capability over a canonical operation
// port.  A host may use this adapter while it migrates an existing canonical
// implementation into the public storage package.
func NewForgetService(operations Forgetter) (*ForgetService, error) {
	return NewForgetServiceWithProjection(operations, nil)
}

// NewForgetServiceWithProjection composes canonical logical denial with the
// rebuildable projection side effect. Canonical denial remains authoritative
// when a later projection retry is required.
func NewForgetServiceWithProjection(operations Forgetter, projection ProjectionSyncer) (*ForgetService, error) {
	if operations == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "forget operations are required", nil)
	}
	return &ForgetService{operations: operations, projection: projection}, nil
}
