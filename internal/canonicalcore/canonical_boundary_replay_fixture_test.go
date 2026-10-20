package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	ordered "github.com/baldaworks/boyan/internal/orderedingest"
	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type boundaryReplayFixture struct {
	factory   coreFactory
	path      string
	store     coreOwner
	processor *boundary.BoundaryProcessor
	runtime   *ordered.SequencedIngestRuntime
	model     *profileRetryInvoker
	export    m.Boundary
	at        time.Time
}

func newBoundaryReplayFixture(t *testing.T, partial bool, factory coreFactory) *boundaryReplayFixture {
	t.Helper()
	turn := modelRetryTurn(t)
	f := &boundaryReplayFixture{factory: factory, path: t.TempDir(), model: &profileRetryInvoker{invalidProfile: partial},
		at: turn.CompletedAt.Add(24 * time.Hour)}
	export, err := m.NewBoundary(turn.Scope, turn.Conversation, "replay", m.BoundaryReasonClose, f.at)
	requireNoError(t, err, "boundary: %v")

	f.export = export
	f.open(t)
	native, err := core.NewCanonicalTurnProcessor(f.store, canonicalExtractorFunc(extractTestStateCandidate),
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "native: %v")
	_, err = native.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "native turn: %v")
	t.Cleanup(func() { f.close(t) })

	return f
}

func (f *boundaryReplayFixture) open(t *testing.T) {
	t.Helper()

	var store coreOwner

	var reader coreReader

	var err error

	store, reader = f.factory(t, f.path)

	f.store = store
	adapter, err := ops.NewCanonicalBoundaryStore(store, reader)
	requireNoError(t, err, "adapter: %v")
	deriver, err := derive.NewDeriver(f.model)
	requireNoError(t, err, "deriver: %v")
	f.processor, err = boundary.NewBoundaryProcessorWithClock(adapter, deriver, deriver,
		testProcessorDerivation(), func() time.Time { return f.at })
	requireNoError(t, err, "processor: %v")
	f.runtime, err = ordered.NewSequencedIngestRuntime(store, store, nil, m.DerivationRef{}, nil,
		f.processor, nil, ordered.SequencedIngestConfig{})
	requireNoError(t, err, "ordered runtime: %v")
	requireNoError(t, f.runtime.Start(context.Background()), "start: %v")
}

func (f *boundaryReplayFixture) close(t *testing.T) {
	t.Helper()
	requireNoError(t, f.runtime.Close(context.Background()), "close runtime: %v")
	requireNoError(t, f.store.Close(), "close store: %v")
}

func (f *boundaryReplayFixture) restart(t *testing.T) {
	t.Helper()
	f.close(t)
	f.open(t)
}

func (f *boundaryReplayFixture) request(sequence uint64, fingerprint string) m.SequencedBoundary {
	return m.SequencedBoundary{ProducerID: "replay-producer", ScopeSequence: sequence,
		Fingerprint: fingerprint, Boundary: f.export}
}
