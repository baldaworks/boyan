//nolint:cyclop,funlen,gocognit,lll // Internal WAL/grouping observations require actual writer and connection limits.
package sqlite

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

// This is a component grouping/cancellation observation, distinct from the
// real-canonical full-consumer benchmark. Known fixture authority is explicit.
// A pinned safe reader prevents WAL reuse, making commit-frame deltas observable.
func TestMatchedSQLGroupingCosts(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit cost measurement")
	}

	for _, kind := range []string{"normal129", "oversized-scalar", "aggregate-memberships"} {
		for repetition := range 10 {
			t.Run(fmt.Sprintf("%s/%d", kind, repetition), func(t *testing.T) {
				store, root := openTestStore(t, false)
				reader := &applyReader{records: map[string]memory.RecallRecord{}}

				count := 3
				if kind == "normal129" {
					count = 129
				}

				revisions := make([]memory.MemoryRevision, count)
				for i := range count {
					id := fmt.Sprintf("group-%05d", i)

					record := applyRecord(id)
					if i == 1 && kind == "oversized-scalar" {
						record.ConversationIDs = []string{strings.Repeat("c", 2097153)}
					}

					if i == 1 && kind == "aggregate-memberships" {
						record.ConversationIDs = nil
						for j := range 256 {
							record.ConversationIDs = append(record.ConversationIDs, fmt.Sprintf("membership-%03d-", j)+strings.Repeat("m", 8192))
						}
					}

					reader.records[id] = record
					revisions[i] = memory.MemoryRevision{RevisionID: id}
				}

				applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
				if err != nil {
					t.Fatal(err)
				}

				if err := applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "groups"); err != nil {
					t.Fatal(err)
				}

				pin, err := store.reader.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer pin.Rollback()

				var n int
				if err := pin.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&n); err != nil {
					t.Fatal(err)
				}

				wal := filepath.Join(root, "search", "search.sqlite-wal")

				before, err := matchedWALCommits(wal)
				if err != nil {
					t.Fatal(err)
				}

				var memBefore, memAfter runtime.MemStats
				runtime.ReadMemStats(&memBefore)

				start := time.Now()
				err = applier.Apply(t.Context(), lexicalTestScope, "groups", nil, revisions)
				elapsed := time.Since(start)

				runtime.ReadMemStats(&memAfter)

				if err != nil {
					t.Fatal(err)
				}

				after, err := matchedWALCommits(wal)
				if err != nil {
					t.Fatal(err)
				}
				// The stage is already building; beginBatch's no-op UPDATE produces
				// no WAL commit frame. Thus every observed modifying commit here
				// belongs to writeInputs. This does not count read/no-op transactions.
				groups := after - before

				want := 2
				if kind != "normal129" {
					want = 3
				}

				if groups != want {
					t.Fatalf("actual group commits=%d want=%d total_delta=%d", groups, want, after-before)
				}

				if reader.maxIDs != 1 {
					t.Fatalf("hydration max IDs=%d", reader.maxIDs)
				}

				fmt.Printf("BenchmarkSQLGroups/%s-4\t1\t%d ns/op\t%d B/op\t%d allocs/op\t%d input-groups/op\t%d WAL-commits/op\n", kind, elapsed.Nanoseconds(), memAfter.TotalAlloc-memBefore.TotalAlloc, memAfter.Mallocs-memBefore.Mallocs, groups, after-before)
				t.Logf("GROUPS records=%d normal_target=2097152 row_target=128 isolated=%t effective_connections=%+v max_hydrated_ids=%d pinned_reader=true WAL_physical_bytes_not_bounded=true", count, kind != "normal129", store.connectionLimits, reader.maxIDs)
			})
		}
	}
}

type matchedPrivacyCounts struct {
	canonicalPrivacyReader

	revision, source, structural uint64
	pause                        func(context.Context)
}

func (a *matchedPrivacyCounts) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	a.revision++
	if a.pause != nil {
		a.pause(ctx)
	}

	return a.canonicalPrivacyReader.IsRevisionDenied(ctx, scope, id)
}
func (a *matchedPrivacyCounts) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	a.source++

	return a.canonicalPrivacyReader.IsSourceDenied(ctx, scope, id)
}
func (a *matchedPrivacyCounts) LoadCanonicalRevisions(ctx context.Context, req memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	a.structural++

	if len(req.RevisionIDs) != 1 {
		return nil, fmt.Errorf("authority read is not single-ID")
	}

	return a.canonicalPrivacyReader.LoadCanonicalRevisions(ctx, req)
}

func TestMatchedSQLSourceZeroAndWriterCosts(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit final-only correctness-path cost measurement")
	}

	for repetition := range 10 {
		t.Run(fmt.Sprint(repetition), func(t *testing.T) {
			canonical := denialCanonicalStore(t)
			store, root := openTestStore(t, true)
			denialCanonicalRevision(t, canonical, lexicalTestScope, "zero", "denied-source")
			seedDenialSemantic(t, store, lexicalTestScope, "zero-owner", "zero", false, false)

			authority := &matchedPrivacyCounts{canonicalPrivacyReader: canonical}
			if err := store.bindCanonicalPrivacy(authority); err != nil {
				t.Fatal(err)
			}

			if err := canonical.DenySource(t.Context(), lexicalTestScope, "denied-source", lexicalTestTime); err != nil {
				t.Fatal(err)
			}

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			start := time.Now()
			err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope)
			elapsed := time.Since(start)

			runtime.ReadMemStats(&after)

			if err != nil {
				t.Fatal(err)
			}

			assertPrivacyCount(t, store, "SELECT count(*) FROM semantic_coverage", 0)
			fmt.Printf("BenchmarkFinalOnly/SourceZeroCoverage-4\t1\t%d ns/op\t%d B/op\t%d allocs/op\t%d revision-lookups/op\t%d source-lookups/op\t%d single-structural-loads/op\n", elapsed.Nanoseconds(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, authority.revision, authority.source, authority.structural)
			// A deliberate callback barrier measures the same-scope lease consequence,
			// without introducing a product delay or treating5ms as a latency target.
			denialCanonicalRevision(t, canonical, lexicalTestScope, "safe", "safe-source")
			seedLexical(t, store, "safe-owner", lexicalTestScope, []lexicalFixture{{id: "safe", raw: "needle", sources: []string{"safe-source"}}})

			entered, release := make(chan struct{}), make(chan struct{})

			var once sync.Once

			authority.pause = func(ctx context.Context) {
				once.Do(func() { assertPrivacyCallbackResources(t, ctx, store, lexicalTestScope); close(entered); <-release })
			}

			result := make(chan error, 1)
			go func() { result <- store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope) }()

			<-entered

			applier, err := NewSQLiteCanonicalApplier(store, &applyReader{}, authority)
			if err != nil {
				close(release)
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
			start = time.Now()
			err = applier.BeginProjectionRebuild(ctx, lexicalTestScope, "waiting-writer")
			wait := time.Since(start)

			cancel()
			close(release)

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("same-scope waiter lost deadline: %v", err)
			}

			if err := <-result; err != nil {
				t.Fatal(err)
			}

			info, err := os.Stat(filepath.Join(root, "search", "search.sqlite-wal"))
			if err != nil {
				t.Fatal(err)
			}

			t.Logf("WRITER controlled_callback_barrier=true same_scope_wait_ns=%d caller_deadline_ns=5000000 returned_deadline=true canonical_outside_SQL_and_readers_closed=true WAL_file_length=%d global_ownership_probe_scans_retained_history=true not_fixed_cost=true", wait.Nanoseconds(), info.Size())
		})
	}
}

func matchedWALCommits(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	if len(data) < 32 {
		return 0, fmt.Errorf("short WAL")
	}

	page := int(binary.BigEndian.Uint32(data[8:12]))
	if page == 1 {
		page = 65536
	}

	if page < 512 {
		return 0, fmt.Errorf("invalid WAL page")
	}

	count := 0

	for offset := 32; offset+24+page <= len(data); offset += 24 + page {
		if string(data[offset+8:offset+16]) != string(data[16:24]) {
			break
		}

		if binary.BigEndian.Uint32(data[offset+4:offset+8]) != 0 {
			count++
		}
	}

	return count, nil
}

func TestMatchedSQLCancellationObservations(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit cost measurement")
	}

	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{{"oversized-membership-and-copy", TestCanonicalCancelDuringOversizedMembershipAndCopy}, {"oversized-scrub", TestCanonicalCancelDuringOversizedScrubPreservesPinnedReader}} {
		t.Run(test.name, func(t *testing.T) {
			start := time.Now()

			test.run(t)
			t.Logf("OBSERVATION complete_fixture_wall_ns=%d original_contexts_and_guards_unchanged=true no_latency_SLA=true", time.Since(start).Nanoseconds())
		})
	}
	// A caller already canceled before admission returns its context unchanged.
	store, _ := openTestStore(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	err := store.ReconcileAllCanonicalDenials(ctx)
	t.Logf("OBSERVATION already_canceled_startup_ns=%d error_type=%T", time.Since(start).Nanoseconds(), err)
}
