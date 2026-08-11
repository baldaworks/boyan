package portableapp

import "sync"

// Runtime is the concrete portable Boyan application.  Consumers
// should depend on one or more of its capability interfaces, not on a type
// assertion for optional methods.
type Runtime struct {
	turn     *TurnProcessor
	boundary *BoundaryService
	recall   RecallReader
	trace    TraceReader
	forget   Forgetter

	mu        sync.Mutex
	lifecycle []Lifecycle
	started   int
	starting  bool
	closed    bool
}

var _ TurnIngestor = (*Runtime)(nil)
var _ BoundaryIngestor = (*Runtime)(nil)
var _ RecallReader = (*Runtime)(nil)
var _ TraceReader = (*Runtime)(nil)
var _ Forgetter = (*Runtime)(nil)
var _ Lifecycle = (*Runtime)(nil)
