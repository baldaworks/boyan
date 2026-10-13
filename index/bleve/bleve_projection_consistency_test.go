package bleve_test

import (
	"context"
	"testing"
	"time"

	b "github.com/baldaworks/boyan/index/bleve"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type recallCheckpoints struct {
	manifest     m.ProjectionManifest
	found        bool
	err          error
	second       *m.ProjectionManifest
	calls        int
	beforeSecond func()
}

func (c *recallCheckpoints) LoadActiveProjectionManifest(context.Context,
	m.Scope,
	string) (m.ProjectionManifest,
	bool,
	error) {
	c.calls++
	if c.calls%2 == 0 && c.beforeSecond != nil {
		c.beforeSecond()
	}

	if c.calls%2 == 0 && c.second != nil {
		return *c.second, c.found, c.err
	}

	return c.manifest, c.found, c.err
}

func TestBleveConsistentRecallCheckpoint(t *testing.T) {
	t.Parallel()

	projection, checkpoints, base, request := consistentProjectionFixture(t)

	hits, err := projection.SearchRecallConsistent(context.Background(), request)
	if err != nil || len(hits) != 1 {
		t.Fatalf("sufficient checkpoint: %+v %v", hits, err)
	}

	assertCheckpointFailures(t, projection, checkpoints, base, request)
	checkpoints.manifest = base
	checkpoints.found = true
	request.Query = "absent"

	hits, err = projection.SearchRecallConsistent(context.Background(), request)
	if err != nil || len(hits) != 0 {
		t.Fatalf("certified empty: %+v %v", hits, err)
	}
}

func assertCheckpointFailures(t *testing.T,
	projection *b.BleveRecallProjection,
	checkpoints *recallCheckpoints,
	base m.ProjectionManifest,
	request m.RecallRequest) {
	t.Helper()

	cases := []struct {
		name   string
		change func(*recallCheckpoints)
	}{
		{"stale", func(c *recallCheckpoints) { c.manifest.Watermark = 1 }},
		{"dirty", func(c *recallCheckpoints) { c.manifest.Status = m.ProjectionGenerationDirty }},
		{"building", func(c *recallCheckpoints) { c.manifest.Status = m.ProjectionGenerationBuilding }},
		{"superseded", func(c *recallCheckpoints) { c.manifest.Status = m.ProjectionGenerationSuperseded }},
		{"foreign scope", func(c *recallCheckpoints) { c.manifest.Scope.Key = "test:foreign" }},
		{"wrong projection", func(c *recallCheckpoints) { c.manifest.ProjectionID = "other" }},
		{"unknown", func(c *recallCheckpoints) { c.found = false }},
		{"generation mismatch", func(c *recallCheckpoints) { c.manifest.GenerationID = "other" }},
		{"changed during query",
			func(c *recallCheckpoints) {
				after := base
				after.UpdatedAt = base.UpdatedAt.Add(time.Nanosecond)
				c.second = &after
			}},

		{"dirty during query",
			func(c *recallCheckpoints) {
				after := base
				after.Status = m.ProjectionGenerationDirty
				c.second = &after
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name,
			func(t *testing.T) {
				checkpoints.manifest = base
				checkpoints.found = true
				checkpoints.second = nil
				checkpoints.calls = 0
				tc.change(checkpoints)

				_, err := projection.SearchRecallConsistent(context.Background(), request)

				code, class, ok := m.ClassifyError(err)
				if !ok || code != m.CodeConflict || class != m.ErrorClassRetryable {
					t.Fatalf("unsafe checkpoint accepted: %v", err)
				}
			})
	}

	checkpoints.second = nil
}

func consistentProjectionFixture(t *testing.T) (*b.BleveRecallProjection,
	*recallCheckpoints,
	m.ProjectionManifest,
	m.RecallRequest) {
	t.Helper()

	scope := m.Scope{Key: "test:consistency", Kind: m.ScopeKindPersonal}
	base := m.ProjectionManifest{Scope: scope,
		ProjectionID: "bleve",
		GenerationID: "consistent",
		Status:       m.ProjectionGenerationActive,
		Watermark:    2,
		UpdatedAt:    time.Now().UTC()}
	checkpoints := &recallCheckpoints{manifest: base, found: true}
	projection, err := b.Open(t.TempDir(), b.Config{Checkpoints: checkpoints, ProjectionID: "bleve"})
	requireNoError(t, err, "open: %v")
	t.Cleanup(func() { requireNoError(t, projection.Close(), "close: %v") })

	generation, err := projection.NewGeneration(base.GenerationID)
	requireNoError(t, err, "generation: %v")
	requireNoError(t,
		generation.Index(context.Background(),
			recallProjectionDocument(scope,
				"item",
				"revision",
				"memory",
				base.UpdatedAt,
				1)),
		"index: %v")
	requireNoError(t, generation.Commit(context.Background()), "commit: %v")
	requireNoError(t, generation.Close(), "close: %v")
	requireNoError(t,
		projection.ActivateGenerationForScope(context.Background(),
			scope,
			base.GenerationID),
		"activate: %v")
	request := m.RecallRequest{Scope: scope, Query: "memory", Limit: 10, MinScopeChangeSeq: 2}

	return projection, checkpoints, base, request
}

func TestBleveConsistentRecallCheckpointFailures(t *testing.T) {
	t.Parallel()
	projection, checkpoints, _, request := consistentProjectionFixture(t)
	checkpoints.err = m.RetryableError(m.CodeStoreFailure, "checkpoint read failed", nil)
	_, err := projection.SearchRecallConsistent(context.Background(), request)

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeStoreFailure {
		t.Fatalf("storage failure replaced: %v", err)
	}

	checkpoints.err = nil
	checkpoints.manifest.UpdatedAt = time.Time{}

	if _,
		err = projection.SearchRecallConsistent(context.Background(),
		request); err == nil {
		t.Fatal("malformed checkpoint accepted")
	}

	for _, config := range []b.Config{{}, {Checkpoints: checkpoints, ProjectionID: "bleve"}} {
		empty, openErr := b.Open(t.TempDir(), config)
		requireNoError(t, openErr, "open: %v")

		_, err = empty.SearchRecallConsistent(context.Background(), request)
		requireNoError(t, empty.Close(), "close: %v")

		code, _, ok = m.ClassifyError(err)
		if !ok || code != m.CodeConflict {
			t.Fatalf("missing physical/checkpoint dependency accepted: %v", err)
		}
	}
}

func TestBleveConsistentRecallPinsGenerationDuringActivation(t *testing.T) {
	t.Parallel()
	projection, checkpoints, base, request := consistentProjectionFixture(t)
	other, err := projection.NewGeneration("other")
	requireNoError(t, err, "other: %v")
	requireNoError(t, other.Commit(context.Background()), "commit: %v")
	requireNoError(t, other.Close(), "close: %v")

	entered := make(chan struct{})
	release := make(chan struct{})
	checkpoints.beforeSecond = func() { close(entered); <-release }
	checkpoints.calls = 0
	searched := make(chan error, 1)

	go func() {
		_, queryErr := projection.SearchRecallConsistent(context.Background(), request)
		searched <- queryErr
	}()

	<-entered

	activated := make(chan error, 1)
	go func() {
		activated <- projection.ActivateGenerationForScope(context.Background(), request.Scope, "other")
	}()

	select {
	case err = <-activated:
		t.Fatalf("physical generation changed during pinned query: %v", err)
	default:
	}

	changed := base
	changed.UpdatedAt = base.UpdatedAt.Add(time.Nanosecond)
	checkpoints.second = &changed

	close(release)

	err = <-searched

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeConflict {
		t.Fatalf("concurrent checkpoint mutation accepted: %v", err)
	}

	requireNoError(t, <-activated, "activate after query: %v")
}
