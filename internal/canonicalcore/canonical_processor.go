package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

// CanonicalTurnProcessor owns the canonical turn-processing dependencies. Its
// implementation is split so orchestration and mutation assembly remain easy
// to read independently.
type CanonicalTurnProcessor struct {
	store     CanonicalStore
	extractor CanonicalSemanticExtractor
	policy    PolicyRegistry
}
