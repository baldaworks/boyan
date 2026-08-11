package ingestapp

import (
	"github.com/baldaworks/boyan/internal/canonicalcore"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TurnProcessor owns canonical semantic processing after a host has supplied
// a validated terminal turn.  The canonical store remains the source of truth;
// projection work is a post-commit side effect.
type TurnProcessor struct {
	processor  *canonicalcore.CanonicalTurnProcessor
	projection ProjectionSyncer
	derivation memory.DerivationRef
}
