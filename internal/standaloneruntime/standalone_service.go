package standaloneruntime

import (
	"context"
	stdhttp "net/http"
	"sync"

	"github.com/gofrs/flock"
	"github.com/rs/zerolog"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/derivation"
	memoryops "github.com/baldaworks/boyan/internal/memoryops"
	memoryread "github.com/baldaworks/boyan/internal/memoryread"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	projection "github.com/baldaworks/boyan/internal/projection"
)

type standaloneService struct {
	stateLock  *flock.Flock
	cfg        Config
	logger     zerolog.Logger
	state      stateLayout
	store      standaloneCanonicalStore
	reader     standaloneCanonicalReader
	index      *sqlite.Store
	applier    *sqlite.SQLiteCanonicalApplier
	projection *projection.ProjectionRuntime
	semantic   *semanticRuntime
	sequenced  *orderedingest.SequencedIngestRuntime
	invoker    *derivation.Invoker
	recall     *memoryread.RecallService
	trace      *memoryread.TraceService
	forget     *memoryops.ForgetService
	server     *stdhttp.Server

	lifecycle        sync.Mutex
	operations       sync.WaitGroup
	shutdownContext  context.Context
	cancelOperations context.CancelFunc
	mu               sync.Mutex
	starting         bool
	started          bool
	closing          bool
	serving          error
	done             chan struct{}
	terminal         error
}
