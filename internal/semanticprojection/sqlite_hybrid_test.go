//nolint:cyclop,gocognit,lll,testpackage // Real SQL boundary matrices keep fault outcomes adjacent.
package semanticprojection

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/memoryread"
	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// A legacy generation reader cannot serve the SQL build or execute both SQL channels.
func TestSQLiteHybridReadsMatchingCompletePair(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	hybrid, err := NewHybrid(HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints,
		Reader: f.store, Lexical: f.store, Provider: &fakeProvider{vector: []float32{1, 0}},
		ModelSpace: testModelSpace("model-a")})
	if err != nil {
		t.Fatal(err)
	}

	request := memory.RecallRequest{Scope: f.scope, Query: "eligible", Limit: 10}

	hits, report, err := hybrid.SearchRecallWithReport(t.Context(), request)
	if err != nil || len(hits) != 1 || report.Effective != "hybrid" ||
		hits[0].Lexical != 1.0/61 || hits[0].Semantic != 1.0/61 || hits[0].Score != 2.0/61 {
		t.Fatalf("matching SQL pair: hits=%#v report=%#v err=%v", hits, report, err)
	}
}

func newSQLHybrid(t *testing.T, changes ...func(*memory.RecallRecord)) (*sqliteManagerFixture, *Hybrid, memory.RecallRequest) {
	t.Helper()

	evidence, err := memory.NewEvidenceRef("source-a", "message", memory.MessageRoleUser,
		"eligible text", 0, 13, memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	changes = append([]func(*memory.RecallRecord){func(record *memory.RecallRecord) {
		record.Evidence = []memory.EvidenceRef{evidence}
	}}, changes...)
	f := newSQLiteManagerFixture(t, changes...)
	// Controlled build time admits the historical temporal fixtures without
	// depending on the host clock, which may move during the test run.
	f.manager.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	h, err := NewHybrid(HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints,
		Reader: f.store, Lexical: f.store, Provider: &fakeProvider{vector: []float32{1, 0}},
		ModelSpace: testModelSpace("model-a")})
	if err != nil {
		t.Fatal(err)
	}

	return f, h, memory.RecallRequest{Scope: f.scope, Query: "eligible", Limit: 10}
}

type hybridBoundaryReader struct {
	SearchReader

	open func(context.Context, memory.Scope) (semantic.SearchSnapshot, error)
}

// A canceled dependency can wrap the context sentinel in private diagnostics.
// Preserve cancellation identity and any existing classification without prose.
func TestSQLiteHybridSanitizesWrappedContextErrors(t *testing.T) {
	for _, boundary := range []string{"provider", "canonical", "classified-canonical"} {
		for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(boundary+sentinel.Error(), func(t *testing.T) {
				_, h, request := newSQLHybrid(t)

				failure := fmt.Errorf("private dependency prose: %w", sentinel)
				if boundary == "classified-canonical" {
					failure = memory.RetryableError(memory.CodeConflict, "private canonical data", failure)
				}

				if boundary == "provider" {
					h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) { return nil, failure })
				} else {
					h.canonical = hybridScopeReaderFunc(func(context.Context, memory.Scope) (memory.ScopeState, error) { return memory.ScopeState{}, failure })
				}

				_, report, err := h.SearchRecallWithReport(t.Context(), request)
				if !errors.Is(err, sentinel) || !errors.Is(err, failure) || report != nil || strings.Contains(err.Error(), "private") {
					t.Fatalf("wrapped context leaked or lost: %#v %v", report, err)
				}

				code, class, classified := memory.ClassifyError(err)
				if boundary == "classified-canonical" {
					if code != memory.CodeConflict || class != memory.ErrorClassRetryable {
						t.Fatalf("lost classification: %v", err)
					}
				} else if classified {
					t.Fatalf("context misclassified as storage failure: %v", err)
				}
			})
		}
	}
}

// Wrapped nil dependencies must fail at construction instead of panicking on
// the first authority/provider/SQL operation.
func TestSQLiteHybridRejectsTypedNilDependencies(t *testing.T) {
	f, _, _ := newSQLHybrid(t)
	for _, dependency := range []string{"canonical", "manifests", "reader", "lexical", "provider"} {
		t.Run(dependency, func(t *testing.T) {
			deps := HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints, Reader: f.store, Lexical: f.store,
				Provider: &fakeProvider{vector: []float32{1, 0}}, ModelSpace: testModelSpace("model-a")}

			switch dependency {
			case "canonical":
				deps.Canonical = (*fakeCanonical)(nil)
			case "manifests":
				deps.Manifests = (*fakeManifestSource)(nil)
			case "reader":
				deps.Reader = (*sqlitestore.Store)(nil)
			case "lexical":
				deps.Lexical = (*sqlitestore.Store)(nil)
			case "provider":
				deps.Provider = (*fakeProvider)(nil)
			}

			if _, err := NewHybrid(deps); !errors.Is(err, semantic.ErrInvalid) {
				t.Fatalf("typed nil accepted: %v", err)
			}
		})
	}
}

// Incompatible state and malformed query vectors permit ordinary lexical
// recall, while every enabled nonzero minimum retains retryable conflict.
func TestSQLiteHybridIncompatibleAndInvalidProviderFallback(t *testing.T) {
	for _, change := range []string{"model", "dimensions", "generation", "watermark", "incomplete", "bad-count", "bad-dimension", "bad-norm", "nan", "infinity"} {
		t.Run(change, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)

			switch change {
			case "model":
				h.spaceID = strings.Repeat("f", 64)
			case "dimensions":
				h.space.Dimensions = 3
			case "generation":
				f.exec(t, "UPDATE semantic_build SET generation_id='other-generation'")
			case "watermark":
				f.exec(t, "UPDATE semantic_build SET watermark=x'0000000000000008'")
			case "incomplete":
				f.exec(t, "UPDATE semantic_build SET complete=0")
			default:
				vectors := [][]float32{{0, 0}}

				switch change {
				case "bad-count":
					vectors = nil
				case "bad-dimension":
					vectors = [][]float32{{1}}
				case "nan":
					vectors = [][]float32{{float32(math.NaN()), 0}}
				case "infinity":
					vectors = [][]float32{{float32(math.Inf(1)), 0}}
				}

				h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) { return vectors, nil })
			}

			hits, report, err := h.SearchRecallWithReport(t.Context(), request)
			if err != nil || len(hits) != 1 || report.Effective != "degraded" || report.Validate() != nil {
				t.Fatalf("incompatible fallback=%#v %#v %v", hits, report, err)
			}

			request.MinScopeChangeSeq = 7
			_, _, err = h.SearchRecallWithReport(t.Context(), request)

			code, class, _ := memory.ClassifyError(err)
			if code != memory.CodeConflict || class != memory.ErrorClassRetryable {
				t.Fatalf("minimum=%v", err)
			}
		})
	}
}

func (r hybridBoundaryReader) OpenSearchSnapshot(ctx context.Context,
	scope memory.Scope) (semantic.SearchSnapshot, error) {
	return r.open(ctx, scope)
}

type hybridBoundarySnapshot struct {
	semantic.SearchSnapshot

	afterClose func()
	once       sync.Once
	lexical    func(context.Context, memory.RecallRequest, int) ([]memory.RecallProjectionHit, error)
	dense      func(context.Context, memory.RecallRequest, [][]float32, int) ([]semantic.DenseMatch, semantic.ScanCounts, error)
}

func (s *hybridBoundarySnapshot) Close() error {
	err := s.SearchSnapshot.Close()
	s.once.Do(func() {
		if s.afterClose != nil {
			s.afterClose()
		}
	})

	return err
}

func (s *hybridBoundarySnapshot) SearchLexical(ctx context.Context, request memory.RecallRequest,
	limit int) ([]memory.RecallProjectionHit, error) {
	if s.lexical != nil {
		return s.lexical(ctx, request, limit)
	}

	return s.SearchSnapshot.SearchLexical(ctx, request, limit)
}

func (s *hybridBoundarySnapshot) SearchSemantic(ctx context.Context, request memory.RecallRequest,
	queries [][]float32, limit int) ([]semantic.DenseMatch, semantic.ScanCounts, error) {
	if s.dense != nil {
		return s.dense(ctx, request, queries, limit)
	}

	return s.SearchSnapshot.SearchSemantic(ctx, request, queries, limit)
}

func wrapHybridFirstRead(h *Hybrid, change func(*hybridBoundarySnapshot)) {
	reader := h.reader
	first := true
	h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context,
		scope memory.Scope) (semantic.SearchSnapshot, error) {
		snapshot, err := reader.OpenSearchSnapshot(ctx, scope)
		if err != nil || !first {
			return snapshot, err
		}

		first = false
		wrapped := &hybridBoundarySnapshot{SearchSnapshot: snapshot}
		change(wrapped)

		return wrapped, nil
	}}
}

// Opening a transaction before inference blocks the provider behind the full reader pool.
func TestSQLiteHybridProviderPrecedesPoolWait(t *testing.T) {
	f, h, request := newSQLHybrid(t)

	one, err := f.store.OpenSearchSnapshot(t.Context(), f.scope)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()

	two, err := f.store.OpenSearchSnapshot(t.Context(), f.scope)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	// Provider can itself release both real readers; this deadlocks if any SQL
	// acquisition happens before inference. The caller deadline bounds failure.
	h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
		_ = one.Close()
		_ = two.Close()

		return [][]float32{{1, 0}}, nil
	})

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	hits, report, err := h.SearchRecallWithReport(ctx, request)
	if err != nil || len(hits) != 1 || report.Effective != "hybrid" {
		t.Fatalf("provider behind reader pool: %#v %#v %v", hits, report, err)
	}
}

// Skipping a postcheck, checking only nonempty results, or checking only the
// logical manifest releases stale dense identities for one of these transitions.
func TestSQLiteHybridRejectsChangedCertificatesIncludingEmptyMinimum(t *testing.T) {
	for _, transition := range []string{"canonical", "canonical-manifest", "physical", "same-tuple-new-physical", "semantic-epoch", "semantic-model", "semantic-incomplete"} {
		for _, empty := range []bool{false, true} {
			t.Run(transition+map[bool]string{true: "-empty", false: "-hits"}[empty], func(t *testing.T) {
				f, h, request := newSQLHybrid(t)

				request.MinScopeChangeSeq = 7
				if empty {
					request.SourceID = "absent"
				}

				wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
					s.afterClose = func() {
						switch transition {
						case "canonical":
							f.canonical.setSequence(8)
						case "canonical-manifest":
							manifest := f.checkpoints.manifest
							manifest.Status = memory.ProjectionGenerationDirty
							f.checkpoints.set(manifest)
						case "physical":
							f.exec(t, "UPDATE lexical_build SET revoked=1")
						case "same-tuple-new-physical":
							f.exec(t, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state)
 SELECT 'replacement',scope_kind,scope_key,generation_id,watermark,'committed' FROM lexical_build LIMIT 1`)
							f.exec(t, "UPDATE lexical_active SET physical_build_id='replacement'")
						case "semantic-epoch":
							state, _, err := f.store.ActiveState(t.Context(), f.scope)
							if err != nil {
								t.Fatal(err)
							}

							if err := f.store.InvalidatePublication(t.Context(), f.scope, state.BuildID, state.PublicationEpoch); err != nil {
								t.Fatal(err)
							}

							if err := f.manager.Repair(t.Context(), f.scope); err != nil {
								t.Fatal(err)
							}
						case "semantic-model":
							f.exec(t, "UPDATE semantic_build SET model_space='changed-model'")
						case "semantic-incomplete":
							f.exec(t, "UPDATE semantic_build SET complete=0")
						}
					}
				})
				hits, report, err := h.SearchRecallWithReport(t.Context(), request)

				code, class, ok := memory.ClassifyError(err)
				if !ok || code != memory.CodeConflict || class != memory.ErrorClassRetryable || hits != nil || report != nil {
					t.Fatalf("changed certificate released: %#v %#v %v", hits, report, err)
				}
			})
		}
	}
}

// Recomputing time after inference or ignoring an explicit historical AsOf
// drops the only eligible revision at the expiry/validity boundaries.
func TestSQLiteHybridPinsEffectiveTimeAndExplicitAsOf(t *testing.T) {
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := start.Add(time.Second)
	observed := start.Add(-time.Hour)
	validFrom := start

	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured", true: "explicit"}[explicit], func(t *testing.T) {
			_, h, request := newSQLHybrid(t, func(record *memory.RecallRecord) {
				record.Temporal.ObservedAt = observed
				record.Temporal.ValidFrom = &validFrom
				record.Temporal.ExpiresAt = &expires
			})
			clock := start
			h.now = func() time.Time { return clock }

			if explicit {
				request.AsOf = &start
				clock = expires.Add(time.Hour)
			}

			h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				clock = expires.Add(time.Hour)

				return [][]float32{{1, 0}}, nil
			})

			hits, report, err := h.SearchRecallWithReport(t.Context(), request)
			if err != nil || len(hits) != 1 || report.Effective != "hybrid" || hits[0].Semantic != 1.0/61 {
				t.Fatalf("captured time lost: %#v %#v %v", hits, report, err)
			}

			if !explicit && request.AsOf != nil {
				t.Fatal("caller request mutated")
			}

			request.AsOf = &expires
			request.MinScopeChangeSeq = 7

			hits, report, err = h.SearchRecallWithReport(t.Context(), request)
			if err != nil || len(hits) != 0 || report.Effective != "hybrid" {
				t.Fatalf("expiry equality: %#v %#v %v", hits, report, err)
			}
		})
	}
}

// A chunk top-k, caller limit applied before fusion, or an uncapped channel
// loses one independent population or exceeds the 100+100 candidate budget.
//
//nolint:funlen // Fixture publication and independently derived bounds form one SQL contract.
func TestSQLiteHybridBoundsIndependentChannelsAndRRF(t *testing.T) {
	f := newSQLiteManagerFixture(t)

	var (
		records   []memory.RecallRecord
		revisions []memory.MemoryRevision
	)

	for _, prefix := range []string{"dense", "lexical"} {
		for i := range 110 {
			id := fmt.Sprintf("%s-%03d", prefix, i)

			text := "journey"
			if prefix == "lexical" {
				text = "eligible"
			}

			records = append(records, testRecord(f.scope, id, id, text, memory.SensitivityStandard, memory.RetentionClassStandard))
			revisions = append(revisions, memory.MemoryRevision{RevisionID: id})
		}
	}

	f.canonical.replace(7, records...)
	manifest := f.checkpoints.manifest
	manifest.GenerationID = "sql-hybrid-bounds"
	f.checkpoints.set(manifest)

	applier, err := sqlitestore.NewSQLiteCanonicalApplier(f.store.Store, f.canonical)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(f.checkpoints, "sqlite")

	if err = applier.BeginProjectionRebuild(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), f.scope, manifest.GenerationID, nil, revisions); err != nil {
		t.Fatal(err)
	}

	if err = applier.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err = applier.Activate(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err = f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	h, err := NewHybrid(HybridDependencies{Canonical: f.canonical, Manifests: f.checkpoints, Reader: f.store, Lexical: f.store,
		Provider: &fakeProvider{vector: []float32{1, 0}}, ModelSpace: testModelSpace("model-a")})
	if err != nil {
		t.Fatal(err)
	}

	hits, report, err := h.SearchRecallWithReport(t.Context(), memory.RecallRequest{Scope: f.scope, Query: "eligible", Limit: 100})
	if err != nil || len(hits) != 200 || report.LexicalCandidates != 100 || report.SemanticCandidates != 100 || report.ScannedChunks != 220 {
		t.Fatalf("channel bounds: hits=%d report=%#v err=%v", len(hits), report, err)
	}

	if hits[0].RevisionID != "dense-000" || hits[0].Semantic != 1.0/61 || hits[0].Lexical != 0 ||
		hits[1].RevisionID != "lexical-000" || hits[1].Lexical != 1.0/61 || hits[1].Semantic != 0 {
		t.Fatalf("RRF60 ordering/contributions: %#v", hits[:2])
	}
}

// The SQL prefilters, including exact scope kind, must reach both channels.
//
//nolint:funlen // All independent filters share one real SQL fixture and literal outcomes.
func TestSQLiteHybridFiltersAndTemporalBoundaries(t *testing.T) {
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	_, h, request := newSQLHybrid(t, func(record *memory.RecallRecord) {
		category := memory.AtomCategory("fact")
		record.Category = &category
		record.Temporal.ObservedAt = at
		record.Temporal.ValidFrom = &at
		record.Temporal.ValidUntil = &end
		record.Temporal.ExpiresAt = &end
	})
	request.AsOf = &at

	for _, filter := range []string{"matching", "kind", "category", "key", "source", "conversation", "before-observed", "valid-until", "expiry"} {
		t.Run(filter, func(t *testing.T) {
			r := request

			switch filter {
			case "kind":
				kind := memory.MemoryKindEvent
				r.Kind = &kind
			case "category":
				category := memory.AtomCategory("preference")
				r.Category = &category
			case "key":
				r.MemoryKey = "other"
			case "source":
				r.SourceID = "other"
			case "conversation":
				r.ConversationID = "other"
			case "before-observed":
				before := at.Add(-time.Nanosecond)
				r.AsOf = &before
			case "valid-until", "expiry":
				r.AsOf = &end
			case "matching":
				kind := memory.MemoryKindState
				category := memory.AtomCategory("fact")
				r.Kind = &kind
				r.Category = &category
				r.SourceID = "source-a"
				r.ConversationID = "conversation-a"
				r.MemoryKey = "memory-item-a"
			}

			hits, report, err := h.SearchRecallWithReport(t.Context(), r)

			want := 0
			if filter == "matching" {
				want = 1
			}

			if err != nil || len(hits) != want || report.Effective != "hybrid" {
				t.Fatalf("filter=%s hits=%#v report=%#v err=%v", filter, hits, report, err)
			}
		})
	}
	// The same key in another kind cannot use this scope's certificate.
	r := request
	r.Scope.Kind = memory.ScopeKindGroup

	_, _, err := h.SearchRecallWithReport(t.Context(), r)
	if err == nil {
		t.Fatal("foreign scope kind unexpectedly certified")
	}
}

type hybridScopeReaderFunc func(context.Context, memory.Scope) (memory.ScopeState, error)

func (read hybridScopeReaderFunc) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	return read(ctx, scope)
}

type hybridLexicalFunc func(context.Context, memory.RecallRequest) ([]memory.RecallProjectionHit, error)

func (search hybridLexicalFunc) SearchRecall(ctx context.Context, request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	return search(ctx, request)
}

// Semantic-only failure never masks canonical/lexical I/O. Raw diagnostics are
// wrapped without losing either their classified retryability or errors.Is.
func TestSQLiteHybridOutagePropagatesSanitizedAuthorityFailures(t *testing.T) {
	for _, boundary := range []string{"canonical", "lexical", "raw-canonical"} {
		t.Run(boundary, func(t *testing.T) {
			_, h, request := newSQLHybrid(t)
			h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				return nil, errors.New("private provider secret")
			})
			failure := errors.New("private canonical data")

			var want error = failure
			if boundary != "raw-canonical" {
				want = memory.RetryableError(memory.CodeStoreFailure, "private SQL data", failure)
			}

			if boundary == "lexical" {
				h.lexical = hybridLexicalFunc(func(context.Context, memory.RecallRequest) ([]memory.RecallProjectionHit, error) { return nil, want })
			} else {
				h.canonical = hybridScopeReaderFunc(func(context.Context, memory.Scope) (memory.ScopeState, error) { return memory.ScopeState{}, want })
			}

			_, report, err := h.SearchRecallWithReport(t.Context(), request)

			code, class, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeStoreFailure || !errors.Is(err, want) || report != nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("masked/leaked authority error: %#v %v", report, err)
			}

			if boundary != "raw-canonical" && class != memory.ErrorClassRetryable {
				t.Fatalf("retry class lost: %v", err)
			}
		})
	}
}

// A failed channel must close the real transaction before returning. Acquiring
// both bounded readers afterwards catches even a single leaked snapshot.
func TestSQLiteHybridClosesTransactionsOnFailure(t *testing.T) {
	for _, boundary := range []string{"lexical", "semantic", "certificate", "post-certificate"} {
		t.Run(boundary, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)
			closed := false

			wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
				s.afterClose = func() { closed = true }

				switch boundary {
				case "lexical":
					s.lexical = func(context.Context, memory.RecallRequest, int) ([]memory.RecallProjectionHit, error) {
						return nil, errors.New("private SQL")
					}
				case "semantic":
					s.dense = func(context.Context, memory.RecallRequest, [][]float32, int) ([]semantic.DenseMatch, semantic.ScanCounts, error) {
						return nil, semantic.ScanCounts{}, semantic.ErrCorrupt
					}
				case "certificate":
					// Revoke only the snapshot's selected semantic metadata through a
					// wrapper so the real transaction still needs explicit closure.
					s.SearchSnapshot = hybridStateSnapshot{s.SearchSnapshot}
				case "post-certificate":
					s.afterClose = func() { closed = true; f.canonical.setSequence(8) }
				}
			})

			_, _, _ = h.SearchRecallWithReport(t.Context(), request)
			if !closed {
				t.Fatal("initial transaction was not closed")
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			one, err := f.store.OpenSearchSnapshot(ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			defer one.Close()

			two, err := f.store.OpenSearchSnapshot(ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			defer two.Close()
		})
	}
}

type hybridStateSnapshot struct{ semantic.SearchSnapshot }

func (hybridStateSnapshot) SemanticState() (semantic.State, bool, error) {
	return semantic.State{}, false, nil
}

// Invalid input and caller deadlines propagate instead of falling back to a
// successful lexical result, including when inference returns after cancellation.
func TestSQLiteHybridRejectsInvalidAndCanceledRequests(t *testing.T) {
	_, h, request := newSQLHybrid(t)
	for _, query := range []string{"", strings.Repeat("x", 4097), string([]byte{0xff})} {
		r := request
		r.Query = query

		_, report, err := h.SearchRecallWithReport(t.Context(), r)
		if err == nil || report != nil {
			t.Fatalf("invalid request degraded successfully: %#v %v", report, err)
		}
	}

	if _, _, err := h.SearchRecallWithReport(nil, request); err == nil {
		t.Fatal("nil context accepted")
	}

	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		want := context.Canceled

		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(t.Context(), time.Unix(1, 0))
			want = context.DeadlineExceeded
		}

		h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
			cancel()

			return [][]float32{{1, 0}}, nil
		})
		_, report, err := h.SearchRecallWithReport(ctx, request)

		cancel()

		if !errors.Is(err, want) || report != nil {
			t.Fatalf("caller cancellation hidden: %#v %v", report, err)
		}
	}
}

// SQL channel results must originate in the same pinned view even if a writer
// replaces the semantic generation between the two channel queries.
func TestSQLiteHybridSharesTransactionAcrossConcurrentPublication(t *testing.T) {
	f, h, request := newSQLHybrid(t)
	request.MinScopeChangeSeq = 7

	wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
		s.lexical = func(ctx context.Context, r memory.RecallRequest, depth int) ([]memory.RecallProjectionHit, error) {
			hits, err := s.SearchSnapshot.SearchLexical(ctx, r, depth)
			if err != nil {
				return nil, err
			}

			state, _, err := f.store.ActiveState(ctx, f.scope)
			if err != nil {
				return nil, err
			}

			if err = f.store.InvalidatePublication(ctx, f.scope, state.BuildID, state.PublicationEpoch); err != nil {
				return nil, err
			}

			if err = f.manager.Repair(ctx, f.scope); err != nil {
				return nil, err
			}

			return hits, nil
		}
		s.dense = func(ctx context.Context, r memory.RecallRequest, q [][]float32, depth int) ([]semantic.DenseMatch, semantic.ScanCounts, error) {
			hits, counts, err := s.SearchSnapshot.SearchSemantic(ctx, r, q, depth)
			if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-a" {
				t.Fatalf("snapshot changed under reader: %#v %v", hits, err)
			}

			return hits, counts, err
		}
	})
	_, _, err := h.SearchRecallWithReport(t.Context(), request)

	code, class, _ := memory.ClassifyError(err)
	if code != memory.CodeConflict || class != memory.ErrorClassRetryable {
		t.Fatalf("changed epoch released: %v", err)
	}
}

// Inference failure must not hide canonical I/O, and semantic corruption must
// not turn a lexical failure or caller cancellation into successful degradation.
//
//nolint:funlen // Failure setup and exact classified outcomes remain adjacent.
func TestSQLiteHybridErrorPolicy(t *testing.T) {
	for _, boundary := range []string{"provider", "corrupt", "missing", "lexical", "semantic-storage", "canonical", "post-canonical", "cancel"} {
		t.Run(boundary, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			failure := memory.RetryableError(memory.CodeStoreFailure, "private storage failure", nil)

			switch boundary {
			case "provider":
				h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
					return nil, errors.New("private provider secret")
				})
			case "corrupt":
				f.exec(t, "UPDATE semantic_chunk SET embedding=x'0000004000000000'")
			case "missing":
				f.exec(t, "DELETE FROM semantic_active")
			case "canonical":
				f.checkpoints.err = failure
			case "post-canonical":
				wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) { s.afterClose = func() { f.checkpoints.err = failure } })
			case "cancel":
				wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) { s.afterClose = cancel })
			case "lexical":
				wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
					s.lexical = func(context.Context, memory.RecallRequest, int) ([]memory.RecallProjectionHit, error) {
						return nil, failure
					}
				})
			case "semantic-storage":
				wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
					s.dense = func(context.Context, memory.RecallRequest, [][]float32, int) ([]semantic.DenseMatch, semantic.ScanCounts, error) {
						return nil, semantic.ScanCounts{}, failure
					}
				})
			}

			hits, report, err := h.SearchRecallWithReport(ctx, request)

			switch boundary {
			case "provider", "corrupt", "missing":
				if err != nil || len(hits) != 1 || report.Effective != "degraded" || report.Validate() != nil {
					t.Fatalf("fallback: %#v %#v %v", hits, report, err)
				}

				request.MinScopeChangeSeq = 7
				_, _, err = h.SearchRecallWithReport(ctx, request)

				code, class, _ := memory.ClassifyError(err)
				if code != memory.CodeConflict || class != memory.ErrorClassRetryable {
					t.Fatalf("minimum=%v", err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel=%v", err)
				}
			default:
				if !errors.Is(err, failure) || hits != nil || report != nil {
					t.Fatalf("failure hidden: %#v %#v %v", hits, report, err)
				}
			}

			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatalf("leaked failure: %v", err)
			}
		})
	}
}

// Hydration is still authoritative after a coherent SQL read; stale copies may
// never release forgotten, foreign-scope, superseded or expired canonical text.
func TestSQLiteHybridCanonicalHydrationBarrier(t *testing.T) {
	for _, change := range []string{"control", "forgotten", "foreign", "superseded", "expired", "sensitivity"} {
		t.Run(change, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)
			at := time.Now().UTC()
			request.AsOf = &at

			wrapHybridFirstRead(h, func(s *hybridBoundarySnapshot) {
				s.afterClose = func() {
					f.canonical.mu.Lock()
					defer f.canonical.mu.Unlock()

					record := f.canonical.records["revision-a"]

					switch change {
					case "forgotten":
						delete(f.canonical.records, "revision-a")

						return
					case "foreign":
						record.Scope.Key = "foreign"
					case "superseded":
						record.State = memory.RevisionStateSuperseded
					case "expired":
						record.Temporal.ExpiresAt = &at
					case "sensitivity":
						record.Sensitivity = memory.SensitivitySensitive
					}

					f.canonical.records["revision-a"] = record
				}
			})

			service, err := memoryread.NewRecallService(f.canonical, h)
			if err != nil {
				t.Fatal(err)
			}

			response, err := service.Search(t.Context(), request)

			want := 0
			if change == "control" {
				want = 1
			}

			if change == "foreign" {
				code, _, _ := memory.ClassifyError(err)
				if code != memory.CodeScopeViolation {
					t.Fatalf("foreign hydration error=%v", err)
				}

				return
			}

			if err != nil || len(response.Results) != want {
				t.Fatalf("canonical privacy barrier leaked: %#v", response)
			}
		})
	}
}
