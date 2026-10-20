package boyan_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	hostapi "github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type hostCanonicalOwner interface {
	m.CanonicalStore
	m.CanonicalTurnIdentityStore
	m.ProjectionCheckpointStore
	m.ScopeEraseStore
	PrepareSourcePrivacy(ctx context.Context) error
	LoadPayload(ctx context.Context, ref m.PayloadRef) ([]byte, error)
	IsSourceDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope m.Scope, id string) (bool, error)
	Close() error
}

var hostDSNSequence atomic.Uint64

func forEachHostBackend(t *testing.T, run func(*testing.T, string)) {
	t.Helper()

	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) { run(t, backend) })
	}
}

func selectedHostConfig(t *testing.T, backend string) hostapi.Config {
	t.Helper()
	cfg := testHostConfig(t)
	requireNoError(t, os.MkdirAll(cfg.StateDir, 0o700), "create private fixture state: %v")

	if backend == "postgres" {
		db := canonicalsqltest.PostgresDatabase(t)
		// Each fixture owns a distinct name, including parallel Host tests.
		// No shared environment variable is replaced or restored.
		name := fmt.Sprintf("BOYAN_TASK19_HOST_DSN_%d", hostDSNSequence.Add(1))
		requireNoError(t, os.Setenv(name, db.DSN), "set fixture DSN: %v")
		t.Cleanup(func() { requireNoError(t, os.Unsetenv(name), "remove fixture DSN: %v") })

		cfg.Canonical.Backend = backend
		cfg.Canonical.Postgres.DSNEnv = name
		cfg.Canonical.Postgres.AllowLocalInsecure = true
	}

	return cfg
}

// Opening an existing fixture deliberately does not prepare or repair it.
// The original test body controls preparation, interrupted erase, and restart.
func openHostCanonical(t *testing.T, cfg hostapi.Config) (hostCanonicalOwner, m.RecallCanonicalReader, error) {
	t.Helper()

	if cfg.Canonical.Backend == "postgres" {
		store, err := postgres.Open(
			t.Context(),
			postgres.Config{DSN: os.Getenv(cfg.Canonical.Postgres.DSNEnv), AllowLocalInsecure: true},
		)
		if err != nil {
			return nil, nil, err
		}

		return store, postgres.NewCanonicalReader(store), nil
	}

	store, err := sqlite.Open(
		t.Context(),
		sqlite.Config{Path: filepath.Join(cfg.StateDir, "canonical", "canonical.sqlite")},
	)
	if err != nil {
		return nil, nil, err
	}

	return store, sqlite.NewCanonicalReader(store), nil
}
