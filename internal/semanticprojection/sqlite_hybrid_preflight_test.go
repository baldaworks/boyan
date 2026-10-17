//nolint:cyclop,funlen,gocognit,gocyclo,lll,nestif,testpackage // Real SQL admission matrices keep boundary setup and observable outcomes together.
package semanticprojection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/memoryread"
	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Inference before a negative metadata observation enters the forbidden provider.
func TestSQLiteHybridPreflightSkipsProviderForUnavailableMetadata(t *testing.T) {
	for _, change := range []string{"missing", "degraded", "model", "dimensions", "generation", "watermark", "scope", "physical", "incomplete", "malformed", "ready"} {
		t.Run(change, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)

			switch change {
			case "missing":
				f.exec(t, "DELETE FROM semantic_active")
			case "degraded":
				state, found, err := f.store.ActiveState(t.Context(), f.scope)
				if err != nil || !found {
					t.Fatalf("ready setup: %v", err)
				}

				generation := semantic.StateFromGeneration(state)

				generation.BuildID = "degraded-preflight"
				if _, err := f.store.PublishDegraded(t.Context(), semantic.Degraded{Generation: generation,
					SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: state.PublicationEpoch}, state.BuildID); err != nil {
					t.Fatal(err)
				}
			case "model":
				f.exec(t, "UPDATE semantic_build SET model_space='other-model'")
			case "dimensions":
				h.space.Dimensions = 3
			case "generation":
				f.exec(t, "UPDATE semantic_build SET generation_id='other-generation'")
			case "watermark":
				f.exec(t, "UPDATE semantic_build SET watermark=x'0000000000000008'")
			case "scope":
				// Deliberately corrupt only the external fixture connection. The
				// Store retains normal FK enforcement and all normal pool settings.
				connection, err := f.db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() {
					_, _ = connection.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
					_ = connection.Close()
				})

				for _, statement := range []string{"PRAGMA foreign_keys=OFF", "UPDATE semantic_build SET scope_key='foreign'", "PRAGMA foreign_keys=ON"} {
					if _, err := connection.ExecContext(t.Context(), statement); err != nil {
						t.Fatal(err)
					}
				}
			case "physical":
				f.exec(t, "UPDATE semantic_build SET physical_build_id='other-physical'")
			case "incomplete":
				f.exec(t, "UPDATE semantic_build SET complete=0")
			case "malformed":
				f.exec(t, "UPDATE semantic_build SET coverage_hash='malformed'")
			}

			calls := 0
			h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				calls++

				if change != "ready" {
					t.Fatal("query provider entered for unavailable metadata")
				}

				return [][]float32{{1, 0}}, nil
			})
			fallbacks := 0
			h.lexical = hybridLexicalFunc(func(ctx context.Context, r memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
				fallbacks++

				return f.store.SearchRecall(ctx, r)
			})

			hits, report, err := h.SearchRecallWithReport(t.Context(), request)
			if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision-a" || report.Requested != "hybrid" || report.Validate() != nil {
				t.Fatalf("ordinary result: %#v %#v %v", hits, report, err)
			}

			if change == "ready" {
				if calls != 1 || fallbacks != 0 || report.Effective != "hybrid" || report.LexicalCandidates != 1 || report.SemanticCandidates != 1 || report.ScannedChunks != 1 {
					t.Fatalf("ready channels: calls=%d fallback=%d report=%#v", calls, fallbacks, report)
				}

				return
			}

			if calls != 0 || fallbacks != 1 || report.Effective != "degraded" || report.Reason != "unavailable" || report.LexicalCandidates != 1 || report.SemanticCandidates != 0 || report.ScannedChunks != 0 {
				t.Fatalf("negative report: calls=%d fallback=%d report=%#v", calls, fallbacks, report)
			}

			request.MinScopeChangeSeq = 7
			hits, report, err = h.SearchRecallWithReport(t.Context(), request)

			code, class, classified := memory.ClassifyError(err)
			if !classified || code != memory.CodeConflict || class != memory.ErrorClassRetryable || hits != nil || report != nil || calls != 0 || fallbacks != 1 {
				t.Fatalf("minimum result: %#v %#v %v calls=%d fallback=%d", hits, report, err, calls, fallbacks)
			}
		})
	}
}

type preflightFaultSnapshot struct {
	semantic.SearchSnapshot

	state     func() (semantic.State, bool, error)
	closeView func() error
}

func (s preflightFaultSnapshot) SemanticState() (semantic.State, bool, error) {
	if s.state != nil {
		return s.state()
	}

	return s.SearchSnapshot.SemanticState()
}

func (s preflightFaultSnapshot) Close() error {
	err := s.SearchSnapshot.Close()
	if s.closeView != nil {
		return s.closeView()
	}

	return err
}

func assertHybridReadersReacquirable(t *testing.T, f *sqliteManagerFixture) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	one, err := f.store.OpenSearchSnapshot(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()

	two, err := f.store.OpenSearchSnapshot(ctx, f.scope)
	if err != nil {
		t.Fatalf("second reader leaked: %v", err)
	}
	defer two.Close()
}

// A semantic-only identity is negative solely at SemanticState. Moving that
// classification to Open/Close, ignoring Close, or swallowing context fails.
func TestSQLiteHybridPreflightErrorBoundaries(t *testing.T) {
	private := errors.New("private SQL query and canonical text")
	storage := memory.RetryableError(memory.CodeStoreFailure, "private SQL", private)

	busy := memory.RetryableError(memory.CodeConflict, "private locked SQL", private)
	for _, test := range []struct {
		name                  string
		open, state, closeErr error
		negative              bool
		cancelAt              string
	}{
		{name: "open-closed", open: semantic.ErrClosed},
		{name: "open-busy", open: busy},
		{name: "open-storage", open: storage},
		{name: "open-wrapped-canceled", open: fmt.Errorf("private open: %w", context.Canceled)},
		{name: "open-wrapped-deadline", open: fmt.Errorf("private open: %w", context.DeadlineExceeded)},
		{name: "state-invalid", state: semantic.ErrInvalid, negative: true},
		{name: "state-capacity", state: semantic.ErrCapacity, negative: true},
		{name: "state-conflict", state: semantic.ErrConflict, negative: true},
		{name: "state-corrupt", state: semantic.ErrCorrupt, negative: true},
		{name: "state-closed", state: semantic.ErrClosed, negative: true},
		{name: "state-storage", state: storage},
		{name: "state-storage-wrapped-semantic", state: memory.RetryableError(memory.CodeStoreFailure, "private storage", semantic.ErrCorrupt)},
		{name: "state-busy-wrapped-semantic", state: memory.RetryableError(memory.CodeConflict, "private locked SQL", semantic.ErrConflict)},
		{name: "state-unexpected", state: private},
		{name: "state-wrapped-canceled", state: fmt.Errorf("private state: %w", context.Canceled)},
		{name: "state-wrapped-deadline", state: fmt.Errorf("private state: %w", context.DeadlineExceeded)},
		{name: "state-negative-and-context", state: errors.Join(semantic.ErrCorrupt, context.Canceled)},
		{name: "close-positive", closeErr: storage},
		{name: "close-negative", state: semantic.ErrCorrupt, closeErr: storage},
		{name: "close-closed", closeErr: semantic.ErrClosed},
		{name: "close-wrapped-context", closeErr: fmt.Errorf("private close: %w", context.Canceled)},
		{name: "primary-state-before-close", state: busy, closeErr: storage},
		{name: "cancel-state", state: storage, cancelAt: "state"},
		{name: "cancel-close", state: storage, closeErr: busy, cancelAt: "close"},
		{name: "deadline-state", state: storage, cancelAt: "deadline-state"},
		{name: "deadline-close", closeErr: storage, cancelAt: "deadline-close"},
	} {
		for _, minimum := range []uint64{0, 7} {
			t.Run(fmt.Sprintf("%s/min-%d", test.name, minimum), func(t *testing.T) {
				f, h, request := newSQLHybrid(t)
				request.MinScopeChangeSeq = minimum

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				if strings.HasPrefix(test.cancelAt, "deadline") {
					ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
					defer cancel()
				}

				reader := h.reader
				closed, opens, calls := 0, 0, 0
				h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
					opens++

					if test.open != nil {
						return nil, test.open
					}

					snapshot, err := reader.OpenSearchSnapshot(ctx, scope)
					if err != nil {
						return nil, err
					}

					return preflightFaultSnapshot{SearchSnapshot: snapshot,
						state: func() (semantic.State, bool, error) {
							if test.cancelAt == "state" {
								cancel()
							}

							if test.cancelAt == "deadline-state" {
								<-ctx.Done()
							}

							if test.state != nil {
								return semantic.State{}, false, test.state
							}

							return snapshot.SemanticState()
						}, closeView: func() error {
							closed++

							if test.cancelAt == "close" {
								cancel()
							}

							if test.cancelAt == "deadline-close" {
								<-ctx.Done()
							}

							return test.closeErr
						}}, nil
				}}
				h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
					calls++

					t.Fatal("provider crossed failed/negative probe")

					return nil, nil
				})
				hits, report, err := h.SearchRecallWithReport(ctx, request)

				if calls != 0 || opens != 1 || (test.open == nil && closed != 1) {
					t.Fatalf("ownership calls=%d opens=%d closes=%d", calls, opens, closed)
				}

				if test.negative && minimum == 0 {
					if err != nil || len(hits) != 1 || report.Effective != "degraded" || report.Validate() != nil {
						t.Fatalf("semantic negative: %#v %#v %v", hits, report, err)
					}
				} else {
					if err == nil || hits != nil || report != nil || strings.Contains(err.Error(), "private") {
						t.Fatalf("failure hidden/leaked: %#v %#v %v", hits, report, err)
					}

					want := test.open
					if want == nil {
						want = test.state
					}

					if want == nil || test.name == "close-negative" || test.negative {
						want = test.closeErr
					}

					if test.cancelAt != "" {
						want = ctx.Err()
					}

					if want != nil && !errors.Is(err, want) {
						t.Fatalf("primary identity lost: got=%v want=%v", err, want)
					}

					code, class, classified := memory.ClassifyError(err)

					wantCode, wantClass, wantClassified := memory.ClassifyError(want)
					if test.negative {
						wantCode, wantClass, wantClassified = memory.CodeConflict, memory.ErrorClassRetryable, true
					}

					if wantClassified && (!classified || code != wantCode || class != wantClass) {
						t.Fatalf("classification lost: %v", err)
					}

					if !wantClassified && want != nil && !errors.Is(want, context.Canceled) && !errors.Is(want, context.DeadlineExceeded) && code != memory.CodeStoreFailure {
						t.Fatalf("unexpected error not classified: %v", err)
					}
				}

				assertHybridReadersReacquirable(t, f)
			})
		}
	}
}

type hybridPhysicalReader struct {
	SearchReader

	physical func(context.Context, memory.Scope, string) (memory.ProjectionManifest, bool, error)
}

func (r hybridPhysicalReader) LoadActivePhysicalProjectionManifest(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
	return r.physical(ctx, scope, id)
}

type hybridManifestReaderFunc func(context.Context, memory.Scope, string) (memory.ProjectionManifest, bool, error)

func (read hybridManifestReaderFunc) LoadActiveProjectionManifest(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
	return read(ctx, scope, id)
}

// A negative observation still traverses all authority boundaries before the
// ordinary fallback/minimum decision; post-provider authority remains fresh.
func TestSQLiteHybridPreflightAuthorityPriority(t *testing.T) {
	for _, boundary := range []string{"canonical", "manifest", "physical", "uncertified", "lexical", "provider-then-authority", "cancel-canonical", "cancel-physical"} {
		for _, minimum := range []uint64{0, 7} {
			t.Run(fmt.Sprintf("%s/min-%d", boundary, minimum), func(t *testing.T) {
				f, h, request := newSQLHybrid(t)
				request.MinScopeChangeSeq = minimum
				failure := memory.RetryableError(memory.CodeStoreFailure, "private authority", errors.New("private text"))

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				if boundary != "provider-then-authority" {
					f.exec(t, "DELETE FROM semantic_active")
				}

				canonical, physical, manifests := h.canonical, h.reader, h.manifests

				var trace []string

				closed := false
				authorityReads := 0
				h.reader = hybridBoundaryReader{SearchReader: physical, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
					view, err := physical.OpenSearchSnapshot(ctx, scope)
					if err != nil {
						return nil, err
					}

					return preflightFaultSnapshot{SearchSnapshot: view, closeView: func() error {
						closed = true

						trace = append(trace, "closed")

						return nil
					}}, nil
				}}
				h.canonical = hybridScopeReaderFunc(func(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
					if !closed {
						t.Fatal("authority before preflight Close")
					}

					trace = append(trace, "canonical")
					authorityReads++

					if boundary == "cancel-canonical" {
						cancel()
					}

					if boundary == "canonical" || boundary == "cancel-canonical" || (boundary == "provider-then-authority" && authorityReads > 1) {
						return memory.ScopeState{}, failure
					}

					return canonical.LoadScopeState(ctx, scope)
				})
				h.manifests = hybridManifestReaderFunc(func(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
					trace = append(trace, "manifest")

					return manifests.LoadActiveProjectionManifest(ctx, scope, id)
				})
				h.reader = hybridPhysicalReader{SearchReader: h.reader, physical: func(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
					trace = append(trace, "physical")

					if boundary == "cancel-physical" {
						cancel()
					}

					if boundary == "physical" || boundary == "cancel-physical" {
						return memory.ProjectionManifest{}, false, failure
					}

					return physical.LoadActivePhysicalProjectionManifest(ctx, scope, id)
				}}

				if boundary == "manifest" {
					f.checkpoints.err = failure
				}

				if boundary == "uncertified" {
					f.canonical.setSequence(8)
				}

				if boundary == "lexical" {
					h.lexical = hybridLexicalFunc(func(context.Context, memory.RecallRequest) ([]memory.RecallProjectionHit, error) { return nil, failure })
				}

				calls := 0
				h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
					calls++

					trace = append(trace, "provider")

					return nil, errors.New("private provider")
				})
				hits, report, err := h.SearchRecallWithReport(ctx, request)

				wantCalls := 0
				if boundary == "provider-then-authority" {
					wantCalls = 1
				}

				if calls != wantCalls {
					t.Fatalf("calls=%d trace=%v", calls, trace)
				}

				wantTrace := "closed,canonical,manifest,physical"

				switch boundary {
				case "canonical", "cancel-canonical":
					wantTrace = "closed,canonical"
				case "manifest":
					wantTrace = "closed,canonical,manifest"
				case "provider-then-authority":
					wantTrace += ",provider,canonical"
				}

				if strings.Join(trace, ",") != wantTrace {
					t.Fatalf("authority phase trace=%v want=%s", trace, wantTrace)
				}

				if boundary == "uncertified" && minimum == 0 {
					if err != nil || len(hits) != 1 || report.Effective != "degraded" {
						t.Fatalf("uncertified=%#v %#v %v", hits, report, err)
					}
				} else if (boundary == "uncertified" || boundary == "lexical") && minimum != 0 {
					code, class, _ := memory.ClassifyError(err)
					if hits != nil || report != nil || code != memory.CodeConflict || class != memory.ErrorClassRetryable {
						t.Fatalf("minimum=%#v %#v %v", hits, report, err)
					}
				} else if hits != nil || report != nil || !errors.Is(err, failure) || strings.Contains(err.Error(), "private") {
					t.Fatalf("authority failure masked: %#v %#v %v trace=%v", hits, report, err, trace)
				}

				assertHybridReadersReacquirable(t, f)
			})
		}
	}
}

type hybridHydrationFailureReader struct {
	*fakeCanonical

	failure error
	calls   int
}

func (r *hybridHydrationFailureReader) LoadRecallRecords(context.Context, memory.Scope, []string) ([]memory.RecallRecord, error) {
	r.calls++

	return nil, r.failure
}

func TestSQLiteHybridPreflightFallbackHydrationFailure(t *testing.T) {
	for _, minimum := range []uint64{0, 7} {
		t.Run(fmt.Sprint(minimum), func(t *testing.T) {
			f, h, request := newSQLHybrid(t)
			f.exec(t, "DELETE FROM semantic_active")

			request.MinScopeChangeSeq = minimum
			failure := memory.RetryableError(memory.CodeStoreFailure, "canonical hydration unavailable", nil)
			canonical := &hybridHydrationFailureReader{fakeCanonical: f.canonical, failure: failure}

			service, err := memoryread.NewRecallService(canonical, h)
			if err != nil {
				t.Fatal(err)
			}

			h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				t.Fatal("negative metadata entered inference")

				return nil, nil
			})

			response, err := service.Search(t.Context(), request)
			if response.Results != nil || response.Retrieval != nil {
				t.Fatalf("hydration error released response: %#v", response)
			}

			if minimum == 0 {
				if !errors.Is(err, failure) || canonical.calls != 1 {
					t.Fatalf("fallback hydration failure hidden: %v calls=%d", err, canonical.calls)
				}
			} else {
				code, class, _ := memory.ClassifyError(err)
				if code != memory.CodeConflict || class != memory.ErrorClassRetryable || canonical.calls != 0 {
					t.Fatalf("minimum hydrated: %v calls=%d", err, canonical.calls)
				}
			}
		})
	}
}

// The initial metadata open is distinct from both the shared channel snapshot
// and postread observation; inference finishes before either channel is opened.
func TestSQLiteHybridFinalChannelSnapshotFollowsVectors(t *testing.T) {
	f, h, request := newSQLHybrid(t)

	var trace []string

	canonical := h.canonical
	h.canonical = hybridScopeReaderFunc(func(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
		trace = append(trace, "authority")

		return canonical.LoadScopeState(ctx, scope)
	})
	h.provider = queryProviderFunc(func(ctx context.Context, _ []string) ([][]float32, error) {
		if strings.Join(trace, ",") != "authority" {
			t.Fatalf("pre-provider authority trace=%v", trace)
		}

		trace = append(trace, "embed")
		// Unrelated publication and both readers progress while inference owns
		// no transaction, lease or scope/publication lock.
		if err := f.manager.Repair(ctx, f.scope); err != nil {
			return nil, err
		}

		assertHybridReadersReacquirable(t, f)

		trace = append(trace, "vectors")

		return [][]float32{{1, 0}}, nil
	})
	phases := wrapHybridChannelRead(t, h, func(s *hybridBoundarySnapshot) {
		if strings.Join(trace, ",") != "authority,embed,vectors,authority" {
			t.Fatalf("channel admission trace=%v", trace)
		}

		trace = append(trace, "channels")
		s.afterClose = func() { trace = append(trace, "channel-close") }
	})

	hits, report, err := h.SearchRecallWithReport(t.Context(), request)
	if err != nil || len(hits) != 1 || report.Effective != "hybrid" || strings.Join(trace, ",") != "authority,embed,vectors,authority,channels,channel-close,authority" || strings.Join(phases(), ",") != "preflight,channels,postread" {
		t.Fatalf("final vector/channel/authority order: %#v %#v %v trace=%v", hits, report, err, trace)
	}
}

type hybridResult struct {
	hits   []memory.RecallProjectionHit
	report *memory.RetrievalReport
	err    error
}

// Store.acquire adds its operation lease before deriving the child context.
// Observing Done during that derivation proves admission, with both readers
// already held; it does not assume scheduling order or database/sql FIFO.
type hybridAdmissionContext struct {
	context.Context

	admitted chan struct{}
	once     *sync.Once
}

func (ctx *hybridAdmissionContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.admitted) })

	return ctx.Context.Done()
}

func holdHybridReaders(t *testing.T, f *sqliteManagerFixture, ctx context.Context) func() {
	t.Helper()

	one, err := f.store.OpenSearchSnapshot(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}

	two, err := f.store.OpenSearchSnapshot(ctx, f.scope)
	if err != nil {
		_ = one.Close()

		t.Fatal(err)
	}

	release := sync.OnceFunc(func() { _ = one.Close(); _ = two.Close() })
	t.Cleanup(release)

	return release
}

func startWaitingHybrid(t *testing.T, h *Hybrid, ctx context.Context, request memory.RecallRequest) (<-chan hybridResult, <-chan struct{}) {
	t.Helper()

	reader := h.reader
	admitted := make(chan struct{})

	var once sync.Once

	h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
		return reader.OpenSearchSnapshot(&hybridAdmissionContext{Context: ctx, admitted: admitted, once: &once}, scope)
	}}
	result := make(chan hybridResult, 1)

	go func() {
		hits, report, err := h.SearchRecallWithReport(ctx, request)
		result <- hybridResult{hits, report, err}
	}()

	return result, admitted
}

func awaitHybridResult(t *testing.T, result <-chan hybridResult) hybridResult {
	t.Helper()

	select {
	case got := <-result:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("recall did not drain")

		return hybridResult{}
	}
}

func TestSQLiteHybridPreflightPoolWaitCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline-", deadline), func(t *testing.T) {
			f, h, request := newSQLHybrid(t)
			release := holdHybridReaders(t, f, t.Context())

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			want := context.Canceled

			if deadline {
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()

				want = context.DeadlineExceeded
			}

			var calls atomic.Int32

			h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
				calls.Add(1)

				return [][]float32{{1, 0}}, nil
			})

			result, admitted := startWaitingHybrid(t, h, ctx, request)
			select {
			case <-admitted:
			case <-ctx.Done():
				t.Fatal("probe was not admitted before deadline")
			}

			if !deadline {
				cancel()
			}

			got := awaitHybridResult(t, result)
			if !errors.Is(got.err, want) || got.hits != nil || got.report != nil || calls.Load() != 0 {
				t.Fatalf("pool cancellation=%#v calls=%d", got, calls.Load())
			}

			release()
			assertHybridReadersReacquirable(t, f)
			// Independently releasing capacity permits a normal request to run.
			h.reader = f.store

			hits, report, err := h.SearchRecallWithReport(t.Context(), request)
			if err != nil || len(hits) != 1 || report.Effective != "hybrid" || calls.Load() != 1 {
				t.Fatalf("progress after release: %#v %#v %v", hits, report, err)
			}
		})
	}
}

func TestSQLiteHybridPreflightStoreCloseDrainsWaiter(t *testing.T) {
	f, h, request := newSQLHybrid(t)
	holdHybridReaders(t, f, t.Context())

	var calls atomic.Int32

	var trace []string

	reader, canonical, manifests := h.reader, h.canonical, h.manifests
	h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
		trace = append(trace, "preflight-wait")

		view, err := reader.OpenSearchSnapshot(ctx, scope)
		if err != nil {
			trace = append(trace, "preflight-error")

			return nil, err
		}

		trace = append(trace, "preflight-open")

		return preflightFaultSnapshot{SearchSnapshot: view, closeView: func() error {
			trace = append(trace, "preflight-close")

			return nil
		}}, nil
	}}
	h.canonical = hybridScopeReaderFunc(func(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
		trace = append(trace, "authority-canonical")

		return canonical.LoadScopeState(ctx, scope)
	})
	h.manifests = hybridManifestReaderFunc(func(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
		trace = append(trace, "authority-manifest")

		return manifests.LoadActiveProjectionManifest(ctx, scope, id)
	})
	h.reader = hybridPhysicalReader{SearchReader: h.reader, physical: func(ctx context.Context, scope memory.Scope, id string) (memory.ProjectionManifest, bool, error) {
		trace = append(trace, "authority-physical")

		return reader.LoadActivePhysicalProjectionManifest(ctx, scope, id)
	}}
	h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
		calls.Add(1)

		return [][]float32{{1, 0}}, nil
	})

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	result, admitted := startWaitingHybrid(t, h, ctx, request)
	select {
	case <-admitted:
	case <-ctx.Done():
		t.Fatal("waiting probe not admitted")
	}
	// Actual Store lifetime cancellation closes the held owners and waiter;
	// explicit owner release is deliberately deferred until after the drain.
	if err := f.store.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}

	got := awaitHybridResult(t, result)
	// Lifetime callbacks race legitimately: released owners may let the probe
	// finish before its cancellation callback. Its next authority lease then
	// rejects with the Store's original ErrClosed, rather than caller context.
	canceled := errors.Is(got.err, context.Canceled)

	rejected := errors.Is(got.err, sqlitestore.ErrClosed) && strings.Join(trace, ",") ==
		"preflight-wait,preflight-open,preflight-close,authority-canonical,authority-manifest,authority-physical"
	if (!canceled && !rejected) || ctx.Err() != nil || got.hits != nil || got.report != nil || calls.Load() != 0 {
		t.Fatalf("lifetime cancellation=%#v cause=%v caller=%v calls=%d", got, errors.Unwrap(got.err), ctx.Err(), calls.Load())
	}

	code, class, classified := memory.ClassifyError(got.err)
	if (canceled && classified) || (rejected && (code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent)) {
		t.Fatalf("shutdown classification: %v trace=%v", got.err, trace)
	}

	if _, err := f.store.OpenSearchSnapshot(t.Context(), f.scope); !errors.Is(err, semantic.ErrClosed) {
		t.Fatalf("new lease accepted after Close: %v", err)
	}
}

func TestSQLiteHybridPreflightPublicationRaces(t *testing.T) {
	for _, change := range []string{"negative-ready", "invalidate", "degraded", "model", "source", "physical", "coherent-new-build"} {
		for _, minimum := range []uint64{0, 7} {
			for _, empty := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/min-%d/empty-%v", change, minimum, empty), func(t *testing.T) {
					f, h, request := newSQLHybrid(t)

					request.MinScopeChangeSeq = minimum
					if empty {
						request.SourceID = "absent"
					}

					calls, opens := 0, 0
					reader := h.reader
					h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
						opens++

						return reader.OpenSearchSnapshot(ctx, scope)
					}}

					if change == "negative-ready" {
						f.exec(t, "DELETE FROM semantic_active")

						h.reader = hybridBoundaryReader{SearchReader: h.reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
							opens++

							view, err := reader.OpenSearchSnapshot(ctx, scope)
							if err != nil {
								return nil, err
							}

							return preflightFaultSnapshot{SearchSnapshot: view, closeView: func() error { return f.manager.Repair(ctx, f.scope) }}, nil
						}}
					}

					h.provider = queryProviderFunc(func(ctx context.Context, _ []string) ([][]float32, error) {
						calls++

						state, _, err := f.store.ActiveState(ctx, f.scope)
						if err != nil {
							return nil, err
						}

						switch change {
						case "invalidate", "coherent-new-build":
							if err := f.store.InvalidatePublication(ctx, f.scope, state.BuildID, state.PublicationEpoch); err != nil {
								return nil, err
							}

							if change == "coherent-new-build" {
								if err := f.manager.Repair(ctx, f.scope); err != nil {
									return nil, err
								}
							}
						case "degraded":
							generation := semantic.StateFromGeneration(state)

							generation.BuildID = "racing-degraded"
							if _, err := f.store.PublishDegraded(ctx, semantic.Degraded{Generation: generation, SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: state.PublicationEpoch}, state.BuildID); err != nil {
								return nil, err
							}
						case "model":
							f.exec(t, "UPDATE semantic_build SET model_space='changed-model'")
						case "source":
							f.canonical.setSequence(8)
						case "physical":
							replaceHybridPhysical(t, f)
						}

						return [][]float32{{1, 0}}, nil
					})
					hits, report, err := h.SearchRecallWithReport(t.Context(), request)

					wantCalls := 1
					if change == "negative-ready" {
						wantCalls = 0

						if opens != 1 {
							t.Fatalf("negative observation reprobed: %d", opens)
						}
					}

					if calls != wantCalls {
						t.Fatalf("provider calls=%d want=%d", calls, wantCalls)
					}

					if change != "coherent-new-build" && minimum != 0 {
						code, class, _ := memory.ClassifyError(err)
						if hits != nil || report != nil || code != memory.CodeConflict || class != memory.ErrorClassRetryable {
							t.Fatalf("racing stale pair released: %#v %#v %v", hits, report, err)
						}
					} else {
						wantHits, effective := 1, "degraded"
						if empty {
							wantHits = 0
						}

						if change == "coherent-new-build" {
							effective = "hybrid"
						}

						if err != nil || len(hits) != wantHits || report.Effective != effective || report.Validate() != nil {
							t.Fatalf("race result: %#v %#v %v", hits, report, err)
						}

						if effective == "degraded" && (report.SemanticCandidates != 0 || report.ScannedChunks != 0) {
							t.Fatalf("stale dense report: %#v", report)
						}
					}
				})
			}
		}
	}
}

func replaceHybridPhysical(t *testing.T, f *sqliteManagerFixture) {
	t.Helper()
	// Keep the real lexical population intact while changing only the exact
	// physical binding. An empty replacement would lose the fallback control.
	f.exec(t, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state,record_count,source_count,conversation_count)
 SELECT 'replacement',scope_kind,scope_key,generation_id,watermark,state,record_count,source_count,conversation_count FROM lexical_build LIMIT 1`)
	f.exec(t, `INSERT INTO lexical_record(physical_build_id,item_id,revision_id,revision,change_seq,kind,category,memory_key,
 created_at,observed_at,valid_from,valid_until,expires_at,sensitivity,retention,raw_tokens,ru_tokens,en_tokens)
 SELECT 'replacement',item_id,revision_id,revision,change_seq,kind,category,memory_key,
 created_at,observed_at,valid_from,valid_until,expires_at,sensitivity,retention,raw_tokens,ru_tokens,en_tokens
 FROM lexical_record WHERE physical_build_id<>'replacement'`)
	f.exec(t, `INSERT INTO lexical_source SELECT n.rowid,s.id FROM lexical_source s JOIN lexical_record o ON o.rowid=s.record_rowid
 JOIN lexical_record n ON n.revision_id=o.revision_id AND n.physical_build_id='replacement' WHERE o.physical_build_id<>'replacement'`)
	f.exec(t, `INSERT INTO lexical_conversation SELECT n.rowid,s.id FROM lexical_conversation s JOIN lexical_record o ON o.rowid=s.record_rowid
 JOIN lexical_record n ON n.revision_id=o.revision_id AND n.physical_build_id='replacement' WHERE o.physical_build_id<>'replacement'`)
	f.exec(t, `INSERT INTO lexical_fts(rowid,raw_tokens,ru_tokens,en_tokens)
 SELECT rowid,raw_tokens,ru_tokens,en_tokens FROM lexical_record WHERE physical_build_id='replacement'`)
	f.exec(t, "UPDATE lexical_active SET physical_build_id='replacement'")
}

// Capturing effective time after pool admission or Close loses the only
// eligible revision; explicit historical requests and caller structs survive.
func TestSQLiteHybridPreflightPinsEntryAsOf(t *testing.T) {
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := start.Add(time.Second)

	for _, boundary := range []string{"pool", "close"} {
		for _, negative := range []bool{false, true} {
			for _, explicit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/negative-%v/explicit-%v", boundary, negative, explicit), func(t *testing.T) {
					f, h, request := newSQLHybrid(t, func(record *memory.RecallRecord) { record.Temporal.ExpiresAt = &expires })
					if negative {
						f.exec(t, "DELETE FROM semantic_active")
					}

					var clock atomic.Int64
					clock.Store(start.UnixNano())

					h.now = func() time.Time { return time.Unix(0, clock.Load()) }

					if explicit {
						request.AsOf = &start

						clock.Store(expires.Add(time.Hour).UnixNano())
					}

					checkTime := func(r memory.RecallRequest) {
						if r.AsOf == nil || !r.AsOf.Equal(start) {
							t.Errorf("entry AsOf changed: %v", r.AsOf)
						}
					}
					reader := h.reader
					opens, channels, fallbacks := 0, 0, 0
					h.reader = hybridBoundaryReader{SearchReader: reader, open: func(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error) {
						opens++

						view, err := reader.OpenSearchSnapshot(ctx, scope)
						if err != nil {
							return nil, err
						}

						wrapped := &hybridBoundarySnapshot{SearchSnapshot: view}
						if opens == 1 && boundary == "close" {
							wrapped.afterClose = func() { clock.Store(expires.Add(time.Hour).UnixNano()) }
						}

						wrapped.lexical = func(ctx context.Context, r memory.RecallRequest, limit int) ([]memory.RecallProjectionHit, error) {
							checkTime(r)

							channels++

							return view.SearchLexical(ctx, r, limit)
						}
						wrapped.dense = func(ctx context.Context, r memory.RecallRequest, q [][]float32, limit int) ([]semantic.DenseMatch, semantic.ScanCounts, error) {
							checkTime(r)

							channels++

							return view.SearchSemantic(ctx, r, q, limit)
						}

						return wrapped, nil
					}}
					h.lexical = hybridLexicalFunc(func(ctx context.Context, r memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
						checkTime(r)

						fallbacks++

						return f.store.SearchRecall(ctx, r)
					})

					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()

					var got hybridResult

					if boundary == "pool" {
						release := holdHybridReaders(t, f, t.Context())

						result, admitted := startWaitingHybrid(t, h, ctx, request)
						select {
						case <-admitted:
						case <-ctx.Done():
							t.Fatal("probe did not enter pool wait")
						}

						clock.Store(expires.Add(time.Hour).UnixNano())
						release()

						got = awaitHybridResult(t, result)
					} else {
						got.hits, got.report, got.err = h.SearchRecallWithReport(ctx, request)
					}

					if got.err != nil || len(got.hits) != 1 || got.hits[0].RevisionID != "revision-a" {
						t.Fatalf("entry time lost: %#v", got)
					}

					if (!explicit && request.AsOf != nil) || (explicit && request.AsOf != &start) {
						t.Fatal("caller AsOf mutated")
					}

					if (negative && (channels != 0 || fallbacks != 1)) || (!negative && (channels != 2 || fallbacks != 0)) {
						t.Fatalf("time assertions did not reach intended phase: channels=%d fallback=%d", channels, fallbacks)
					}
					// The same exact effective time remains the canonical hydration
					// boundary, including strict expiry equality.
					h.reader, h.lexical = f.store, f.store

					service, err := memoryread.NewRecallService(f.canonical, h)
					if err != nil {
						t.Fatal(err)
					}

					request.AsOf = &start

					response, err := service.Search(t.Context(), request)
					if err != nil || len(response.Results) != 1 {
						t.Fatalf("historical hydration: %#v %v", response, err)
					}

					request.AsOf = &expires

					response, err = service.Search(t.Context(), request)
					if err != nil || len(response.Results) != 0 {
						t.Fatalf("expiry equality hydration: %#v %v", response, err)
					}
				})
			}
		}
	}
}

func TestSQLiteHybridPreflightActualSQLErrors(t *testing.T) {
	for _, table := range []string{"semantic_build", "lexical_build"} {
		for _, minimum := range []uint64{0, 7} {
			t.Run(fmt.Sprintf("%s/min-%d", table, minimum), func(t *testing.T) {
				f, h, request := newSQLHybrid(t)
				request.MinScopeChangeSeq = minimum
				// Disable FK only on the explicitly owned corruption connection;
				// all normal Store connections retain foreign_keys=ON.
				conn, err := f.db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON"); _ = conn.Close() })

				for _, statement := range []string{"PRAGMA foreign_keys=OFF", "DROP TABLE " + table, "PRAGMA foreign_keys=ON"} {
					if _, err := conn.ExecContext(t.Context(), statement); err != nil {
						t.Fatal(err)
					}
				}

				h.provider = queryProviderFunc(func(context.Context, []string) ([][]float32, error) {
					t.Fatal("provider crossed actual SQL failure")

					return nil, nil
				})
				hits, report, err := h.SearchRecallWithReport(t.Context(), request)

				code, class, _ := memory.ClassifyError(err)
				if hits != nil || report != nil || code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent || strings.Contains(err.Error(), table) {
					t.Fatalf("actual SQL failure masked/leaked: %#v %#v %v", hits, report, err)
				}
			})
		}
	}
}

// Port the meaningful TASK013 reviewer overlay without changing its archived
// provenance; both changed-certificate probes retain their final-channel hook.
func TestSQLiteHybridRetainsReviewerChannelContracts(t *testing.T) {
	for _, contract := range []string{"four-windows", "timestamp-only", "changed-pair"} {
		t.Run(contract, func(t *testing.T) {
			f, h, request := newSQLHybrid(t)
			windows := 0
			h.provider = queryProviderFunc(func(_ context.Context, inputs []string) ([][]float32, error) {
				windows = len(inputs)

				vectors := make([][]float32, len(inputs))
				for i := range vectors {
					vectors[i] = []float32{1, 0}
				}

				return vectors, nil
			})

			if contract == "four-windows" {
				request.Query = strings.Repeat("eligible ", 400)
			}

			if contract == "timestamp-only" {
				request.MinScopeChangeSeq = 7
			}

			wrapHybridChannelRead(t, h, func(s *hybridBoundarySnapshot) {
				s.afterClose = func() {
					switch contract {
					case "timestamp-only":
						manifest := f.checkpoints.manifest
						manifest.UpdatedAt = manifest.UpdatedAt.Add(time.Second)
						f.checkpoints.set(manifest)
					case "changed-pair":
						f.exec(t, "DELETE FROM semantic_active")
					}
				}
			})

			hits, report, err := h.SearchRecallWithReport(t.Context(), request)
			if contract == "timestamp-only" {
				code, class, _ := memory.ClassifyError(err)
				if hits != nil || report != nil || code != memory.CodeConflict || class != memory.ErrorClassRetryable {
					t.Fatalf("timestamp change accepted: %#v %#v %v", hits, report, err)
				}
			} else if err != nil || len(hits) != 1 || report.Validate() != nil {
				t.Fatalf("reviewer contract: %#v %#v %v", hits, report, err)
			} else if contract == "four-windows" && (windows != 4 || report.Effective != "hybrid" || report.OmittedQueryWindows == 0 || report.OmittedQueryRunes == 0) {
				t.Fatalf("query window omission contract lost: windows=%d report=%#v", windows, report)
			} else if contract == "changed-pair" && (report.Effective != "degraded" || report.SemanticCandidates != 0 || hits[0].Semantic != 0) {
				t.Fatalf("changed pair leaked dense: %#v %#v", hits, report)
			}
		})
	}
}
