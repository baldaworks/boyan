package standaloneruntime

import (
	"context"
	stdhttp "net/http"
	"sync"

	"github.com/rs/zerolog"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/derivation"
	memoryops "github.com/baldaworks/boyan/internal/memoryops"
	memoryread "github.com/baldaworks/boyan/internal/memoryread"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	projection "github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/store/badger"
)

type standaloneService struct {
	cfg         Config
	logger      zerolog.Logger
	state       stateLayout
	store       *badger.CanonicalStore
	maintenance *badger.CanonicalMaintenance
	reader      *badger.CanonicalReader
	index       *sqlite.Store
	applier     *sqlite.SQLiteCanonicalApplier
	projection  *projection.ProjectionRuntime
	semantic    *semanticRuntime
	sequenced   *orderedingest.SequencedIngestRuntime
	invoker     *derivation.Invoker
	recall      *memoryread.RecallService
	trace       *memoryread.TraceService
	forget      *memoryops.ForgetService
	server      *stdhttp.Server

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
