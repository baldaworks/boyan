package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// CanonicalReader is the bounded application adapter over the canonical
// Badger records. It deliberately implements the storage-neutral recall port
// and keeps Badger keys, envelopes, and payload layout inside state.
type CanonicalReader struct {
	store                        *CanonicalStore
	beforeTracePayloadRead       func()
	afterTracePrivacyBarrier     func()
	beforeTraceMessageRecordRead func() error // Test-only source-local read boundary.
}

var _ memory.RecallCanonicalReader = (*CanonicalReader)(nil)
var _ memory.CanonicalDerivedReader = (*CanonicalReader)(nil)

type canonicalRecallView struct {
	item          memory.MemoryItem
	revision      memory.MemoryRevision
	state         memory.RevisionState
	sourceRefs    []memory.SourceRef
	sourceIDs     []string
	conversations []string
	compat        *memory.CanonicalCompatibilityPayload
}

var (
	errCanonicalInactive = errors.New("canonical revision is inactive")
	errCanonicalDenied   = errors.New("canonical revision is denied")
)
