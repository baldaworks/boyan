package memoryread

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// RecallService owns canonical hydration and fail-closed validation around a
// disposable lexical projection.  Projection text is never returned without
// a canonical read for the same exact scope.
type RecallService struct {
	canonical  memory.RecallCanonicalReader
	projection memory.RecallProjection
	now        func() time.Time
}

// NewRecallService constructs bounded canonical recall.  A nil projection is
// valid and selects the bounded canonical-tail fallback.
func NewRecallService(
	canonical memory.RecallCanonicalReader,
	projection memory.RecallProjection,
) (
	*RecallService,
	error,
) {
	if canonical == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "recall canonical reader is required", nil)
	}

	return &RecallService{canonical: canonical, projection: projection, now: time.Now}, nil
}

var _ RecallReader = (*RecallService)(nil)
