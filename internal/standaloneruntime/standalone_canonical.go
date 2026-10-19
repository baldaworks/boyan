package standaloneruntime

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/pkg/memory"
	canonicalpostgres "github.com/baldaworks/boyan/store/postgres"
	canonicalsqlite "github.com/baldaworks/boyan/store/sqlite"
)

// Composition retains the selected public Store pointer across every consumer.
// Algorithms depend on their existing narrower ports, not this runtime aggregate.
//
//nolint:interfacebloat // Runtime composition embeds existing consumer ports without widening those consumers.
type standaloneCanonicalStore interface {
	memory.CanonicalStore
	memory.CanonicalTurnIdentityStore
	canonicalops.BoundaryCanonicalStore
	forgetops.CanonicalForgetStore
	forgetops.CanonicalEraseRecoveryStore
	memory.CanonicalScopeStatePager
	memory.IngestSequenceStore
	memory.ScopeCheckpointStore
	memory.ProjectionCheckpointStore
	memory.ProjectionRetentionFloorReader
	PrepareSourcePrivacy(ctx context.Context) error
	RestorePreflight(ctx context.Context) error
	ResumeRestoreBeforeEraseRecovery(ctx context.Context) error
	IsSourceDenied(ctx context.Context, scope memory.Scope, sourceID string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, revisionID string) (bool, error)
	CheckReadiness(ctx context.Context) error
	Close() error
}

type standaloneCanonicalReader interface {
	memory.RecallCanonicalReader
	memory.CanonicalDerivedReader
	canonicalops.BoundaryCanonicalReader
}

func openStandaloneCanonical(ctx context.Context, layout stateLayout, cfg CanonicalConfig) (
	standaloneCanonicalStore, standaloneCanonicalReader, error,
) {
	switch cfg.Backend {
	case "", "sqlite":
		if cfg.SQLite.Path == "" {
			cfg.SQLite.Path = filepath.Join(layout.CanonicalDir, "canonical.sqlite")
		}

		store, err := canonicalsqlite.Open(ctx, cfg.SQLite)
		if err != nil {
			return nil, nil, err
		}

		return store, canonicalsqlite.NewCanonicalReader(store), nil
	case "postgres":
		store, err := canonicalpostgres.Open(ctx, cfg.Postgres)
		if err != nil {
			return nil, nil, err
		}

		return store, canonicalpostgres.NewCanonicalReader(store), nil
	default:
		return nil, nil, fmt.Errorf("unsupported canonical backend")
	}
}
