//nolint:cyclop,funlen,gocognit,lll // Keep the paired workload matrix and exact input fields adjacent.
package sqlitegate_test

// Matched input and consumer harness. The identical file is added through a Go
// overlay to the read-only old archive; only backend adapters differ.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/memoryread"
	core "github.com/baldaworks/boyan/internal/semantic"
	m "github.com/baldaworks/boyan/pkg/memory"
)

var matchedAt = time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)

type matchedCorpus struct {
	name         string
	scope        m.Scope
	revisions    []m.MemoryRevision
	records      []m.RecallRecord
	canonical    matchedCanonicalOwner
	reader       *countedCanonical
	root         string
	seq          uint64
	conversation string
}

// Instrumentation delegates every call to real Badger. No metadata is rewritten.
type countedCanonical struct {
	m.RecallCanonicalReader

	store                                                          matchedCanonicalOwner
	loads, ids, maxIDs, revisionDenials, sourceDenials, structures uint64
	scopeReads, manifestReads                                      uint64
}

func (r *countedCanonical) LoadRecallRecords(ctx context.Context, scope m.Scope, ids []string) ([]m.RecallRecord, error) {
	r.loads++
	r.ids += uint64(len(ids))
	r.maxIDs = max(r.maxIDs, uint64(len(ids)))

	return r.RecallCanonicalReader.LoadRecallRecords(ctx, scope, ids)
}
func (r *countedCanonical) IsRevisionDenied(ctx context.Context, scope m.Scope, id string) (bool, error) {
	r.revisionDenials++

	return r.store.IsRevisionDenied(ctx, scope, id)
}
func (r *countedCanonical) IsSourceDenied(ctx context.Context, scope m.Scope, id string) (bool, error) {
	r.sourceDenials++

	return r.store.IsSourceDenied(ctx, scope, id)
}
func (r *countedCanonical) LoadCanonicalRevisions(ctx context.Context, req m.CanonicalRevisionReadRequest) ([]m.MemoryRevision, error) {
	r.structures++

	return r.store.LoadCanonicalRevisions(ctx, req)
}
func (r *countedCanonical) LoadScopeState(ctx context.Context, scope m.Scope) (m.ScopeState, error) {
	r.scopeReads++

	return r.store.LoadScopeState(ctx, scope)
}
func (r *countedCanonical) LoadActiveProjectionManifest(ctx context.Context, scope m.Scope, id string) (m.ProjectionManifest, bool, error) {
	r.manifestReads++

	return r.store.LoadActiveProjectionManifest(ctx, scope, id)
}

type matchedBackend interface {
	lexical() m.RecallProjection
	hybrid() m.RecallProjection
	rebuild(ctx context.Context, generation string) error
	copy(ctx context.Context) error
	semantic(ctx context.Context, status string) error
	dense(ctx context.Context) ([]core.DenseMatch, error)
	reopen(ctx context.Context) error
	close() error
	bytes() map[string]int64
	counters() map[string]uint64
}

var matchedCache = struct {
	sync.Mutex

	corpora  map[string]*matchedCorpus
	backends map[string]matchedBackend
}{corpora: map[string]*matchedCorpus{}, backends: map[string]matchedBackend{}}

func TestMain(run *testing.M) {
	code := run.Run()

	for name, backend := range matchedCache.backends {
		if err := backend.close(); err != nil {
			fmt.Fprintln(os.Stderr, err)

			code = 1
		}

		fmt.Printf("FOOTPRINT closed corpus=%s files=%v\n", name, backend.bytes())
	}

	for _, corpus := range matchedCache.corpora {
		if err := corpus.canonical.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)

			code = 1
		}

		_ = os.RemoveAll(corpus.root)
	}

	os.Exit(code)
}

func matchedFixture(tb testing.TB, name string) (*matchedCorpus, matchedBackend) {
	tb.Helper()
	matchedCache.Lock()
	defer matchedCache.Unlock()

	if corpus := matchedCache.corpora[name]; corpus != nil {
		return corpus, matchedCache.backends[name]
	}

	count := 9
	if name == "lexical16384" {
		count = 16384
	}

	if name == "semantic8192" {
		count = 8065
	}

	corpus, err := seedMatchedCorpus(context.Background(), name, count)
	if err != nil {
		tb.Fatal(err)
	}

	matchedCache.corpora[name] = corpus

	backend, err := openMatchedBackend(context.Background(), corpus)
	if err != nil {
		tb.Fatal(err)
	}

	matchedCache.backends[name] = backend
	start := time.Now()

	err = backend.rebuild(context.Background(), "baseline-v1")
	if err != nil {
		tb.Fatal(err)
	}

	tb.Logf("METRIC initial_build corpus=%s ns=%d bytes=%v hydration_calls=%d hydration_max_ids=%d authority_revision=%d authority_source=%d structures=%d", name, time.Since(start).Nanoseconds(), backend.bytes(), corpus.reader.loads, corpus.reader.maxIDs, corpus.reader.revisionDenials, corpus.reader.sourceDenials, corpus.reader.structures)

	if name == "semantic8192" {
		start = time.Now()

		if err := backend.semantic(context.Background(), "ready"); err != nil {
			tb.Fatal(err)
		}

		tb.Logf("METRIC semantic_stage_publish ns=%d bytes=%v", time.Since(start).Nanoseconds(), backend.bytes())
	}

	logMatchedInputs(tb, corpus)

	return corpus, backend
}

func payloadFor(id string, data []byte) m.CanonicalPayload {
	sum := sha256.Sum256(data)

	return m.CanonicalPayload{Ref: m.PayloadRef{ID: id, Digest: hex.EncodeToString(sum[:]), ByteSize: uint32(len(data))}, Data: data}
}

func seedMatchedCorpus(ctx context.Context, name string, count int, factories ...matchedCanonicalFactory) (*matchedCorpus, error) {
	dir, err := os.MkdirTemp("", "boyan-task19-"+name+"-")
	if err != nil {
		return nil, err
	}

	store, reader, err := openMatchedCanonical(ctx, dir, factories)
	if err != nil {
		return nil, err
	}

	c := &matchedCorpus{name: name, scope: m.Scope{Key: "baseline:v1", Kind: m.ScopeKindGroup}, canonical: store, root: dir}
	c.reader = &countedCanonical{RecallCanonicalReader: reader, store: store}
	controls := lexicalControls()
	category := m.AtomCategoryDecision

	c.conversation = "baseline-conversation"
	if name == "oversized" {
		c.conversation = strings.Repeat("c", 2097153)
	}

	sourceHeader := map[string]any{"export_id": "baseline-export", "conversation_id": c.conversation, "agent_conversation_id": "baseline-agent", "source_turn_id": "baseline-turn", "completed_at": matchedAt, "terminal_status": "completed"}
	// Preserve the historical benchmark bytes. The native correctness fixtures
	// use the supported source status and bind evidence before canonical writes.
	if len(factories) != 0 {
		sourceHeader["terminal_status"] = m.TurnTerminalStatusSuccess
	}

	sourceData, _ := json.Marshal(sourceHeader)
	sourcePayload := payloadFor("baseline-source-payload", sourceData)
	messagePayload := payloadFor("baseline-message-payload", []byte("baseline evidence"))

	evidenceDigest := ""
	if len(factories) != 0 {
		evidenceDigest = messagePayload.Ref.Digest
	}

	var pending m.CanonicalMutation

	for i := range count {
		id := fmt.Sprintf("bench-%05d", i)

		text := controls[i%len(controls)].text
		if name == "small" {
			id = controls[i].id
		}

		compat := m.CanonicalCompatibilityPayload{SchemaVersion: m.CanonicalCompatibilitySchemaVersion, Kind: m.DerivedKindAtom, Category: &category, Text: text, LegacyItemID: id, LegacyRevisionID: id, LegacyOperationID: "op-" + id}

		encoded, err := json.Marshal(compat)
		if err != nil {
			return nil, err
		}

		payload := payloadFor("payload-"+id, encoded)

		revision := m.MemoryRevision{SchemaVersion: m.MemorySchemaVersion, RevisionID: id, ItemID: id, Revision: 1, Temporal: m.Temporal{ObservedAt: matchedAt}, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard, Payload: payload.Ref, Evidence: []m.EvidenceRef{{SourceID: "baseline-source", MessageID: "baseline-message", Role: m.MessageRoleUser, StartByte: 0, EndByte: 8, AssertionMode: m.AssertionModeUser, TextDigest: evidenceDigest}}}
		if len(pending.Revisions) == 0 {
			pending = m.CanonicalMutation{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: c.scope, ExpectedScopeVersion: c.seq, Operation: m.OperationRecord{OperationID: "batch-" + id, Fingerprint: "fp-batch-" + id, CommittedAt: matchedAt}}
		}

		pending.Items = append(pending.Items, m.MemoryItem{ItemID: id, Scope: c.scope, Kind: m.MemoryKindState, MemoryKey: m.MemoryKey(id)})
		pending.Revisions = append(pending.Revisions, revision)
		pending.Heads = append(pending.Heads, m.ItemHead{ItemID: id, RevisionID: id})

		pending.Payloads = append(pending.Payloads, payload)
		if i == 0 {
			pending.Sources = []m.CanonicalSourceRecord{{SourceID: "baseline-source", Scope: c.scope, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard, Payload: sourcePayload.Ref}}
			pending.Messages = []m.MessageRecord{{MessageID: "baseline-message", SourceID: "baseline-source", Role: m.MessageRoleUser, Payload: messagePayload.Ref}}
			pending.Payloads = append(pending.Payloads, sourcePayload, messagePayload)
		}

		if len(pending.Revisions) == 64 || i == count-1 {
			outcome, err := store.ApplyCanonicalMutation(ctx, pending)
			if err != nil {
				return nil, fmt.Errorf("seed batch through %d: %w", i, err)
			}

			c.seq = outcome.ChangeSeq
			pending = m.CanonicalMutation{}
		}

		c.revisions = append(c.revisions, revision)
	}

	for _, rev := range c.revisions {
		records, err := reader.LoadRecallRecords(ctx, c.scope, []string{rev.RevisionID})
		if err != nil || len(records) != 1 {
			return nil, fmt.Errorf("seed hydrate %s count=%d: %w", rev.RevisionID, len(records), err)
		}

		if err := records[0].Validate(m.RecallRequest{Scope: c.scope, Query: "test", Limit: 10, AsOf: &matchedAt}, matchedAt); err != nil {
			return nil, err
		}

		if records[0].Category == nil || *records[0].Category != category || records[0].Text != controls[len(c.records)%len(controls)].text || len(records[0].ConversationIDs) != 1 || records[0].ConversationIDs[0] != c.conversation {
			return nil, fmt.Errorf("canonical representation mismatch")
		}

		c.records = append(c.records, records[0])
	}

	return c, nil
}

func matchedSpace() core.ModelSpace {
	return core.ModelSpace{Model: "baseline-onehot-v1", Revision: "v1", Dimensions: 768, QueryPrefix: "query: ", PassagePrefix: "passage: "}
}

type matchedProvider struct{ calls int }

func (p *matchedProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	p.calls++

	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = make([]float32, 768)
		out[i][i%4] = 1
	}

	return out, ctx.Err()
}

func matchedQuery() string { return strings.Repeat("engineer run test ", 70) }

func logMatchedInputs(tb testing.TB, c *matchedCorpus) {
	tb.Helper()

	encoded, _ := json.Marshal(c.records)
	sum := sha256.Sum256(encoded)

	space, err := core.SpaceFingerprint(matchedSpace())
	if err != nil {
		tb.Fatal(err)
	}

	prepared, err := core.PrepareQuery(context.Background(), matchedQuery(), "query: ")
	if err != nil || len(prepared.Windows) != 4 {
		tb.Fatalf("four windows: %d %v", len(prepared.Windows), err)
	}

	queryBytes, _ := json.Marshal(prepared)
	queryHash := sha256.Sum256(queryBytes)

	vectorBytes := make([]byte, 8192*768*4)
	for i := range 8192 {
		binary.LittleEndian.PutUint32(vectorBytes[(i*768+i%768)*4:], 0x3f800000)
	}

	vectorHash := sha256.Sum256(vectorBytes)
	tb.Logf("INPUT derived_profile=actual-canonical-v1 corpus=%s records=%d seq=%d corpus_sha256=%x prepared_query_sha256=%x vectors_sha256=%x model=%s dimensions=768 windows=4 time=%s go=%s gomaxprocs=%d", c.name, len(c.records), c.seq, sum, queryHash, vectorHash, space, matchedAt.Format(time.RFC3339), runtime.Version(), runtime.GOMAXPROCS(0))
}

func TestMatchedCanonicalInputs(t *testing.T) {
	t.Run("badger", func(t *testing.T) {
		c, backend := matchedFixture(t, "small")
		testMatchedCanonicalInputs(t, c, backend)
	})
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		c, backend := matchedSQLFixture(t, "small", fixture)
		testMatchedCanonicalInputs(t, c, backend)
	})
}

func testMatchedCanonicalInputs(t *testing.T, c *matchedCorpus, b matchedBackend) {
	t.Helper()

	service, err := memoryread.NewRecallService(c.reader, b.lexical())
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range lexicalRelevanceQueries() {
		response, err := service.Search(t.Context(), m.RecallRequest{Scope: c.scope, Query: q.query, Limit: 10, AsOf: &matchedAt})
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("CONTROL query=%q results=%d", q.query, len(response.Results))

		hits, err := b.lexical().SearchRecall(t.Context(), m.RecallRequest{Scope: c.scope, Query: q.query, Limit: 10, AsOf: &matchedAt})
		if err != nil {
			t.Fatal(err)
		}

		var ids []string
		for _, r := range hits {
			ids = append(ids, r.RevisionID)
		}

		sort.Strings(ids)

		want := q.want
		if q.query == "café" && matchedEngine() == "old" {
			want = nil
		}

		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("control %q ids=%v want=%v", q.query, ids, want)
		}
	}
}

func TestMatchedDenseOracle(t *testing.T) {
	t.Run("badger", func(t *testing.T) {
		c, backend := matchedFixture(t, "semantic8192")
		testMatchedDenseOracle(t, c, backend)
	})
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		c, backend := matchedSQLFixture(t, "semantic8192", fixture)
		testMatchedDenseOracle(t, c, backend)
	})
}

func testMatchedDenseOracle(t *testing.T, c *matchedCorpus, backend matchedBackend) {
	t.Helper()

	got, err := backend.dense(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	winners := map[string]float64{}

	for i := range 8192 {
		revision := 0
		if i >= 128 {
			revision = i - 127
		}

		id := fmt.Sprintf("bench-%05d", revision)

		score := float64(0)
		if i%768 < 4 {
			score = 1
		}

		winners[id] = max(winners[id], score)
	}

	want := make([]core.DenseMatch, 0, len(winners))
	for id, score := range winners {
		want = append(want, core.DenseMatch{RevisionID: id, Score: score})
	}

	sort.Slice(want, func(i, j int) bool {
		if want[i].Score != want[j].Score {
			return want[i].Score > want[j].Score
		}

		return want[i].RevisionID < want[j].RevisionID
	})

	if len(got) != 100 {
		t.Fatalf("dense candidates=%d", len(got))
	}

	for i := range got {
		if got[i].RevisionID != want[i].RevisionID || got[i].Score != want[i].Score {
			t.Fatalf("oracle rank=%d got=%+v want=%+v", i, got[i], want[i])
		}
	}

	t.Log("ORACLE exact onehot four-window revision maxima top100, first128 duplicate chunks,8065 distinct revisions PASS; no near-tie generalization")

	service, err := memoryread.NewRecallService(c.reader, backend.hybrid())
	if err != nil {
		t.Fatal(err)
	}

	for _, minimum := range []uint64{0, c.seq} {
		before := backend.counters()
		response, err := service.Search(t.Context(), m.RecallRequest{Scope: c.scope, Query: matchedQuery(), Limit: 25, AsOf: &matchedAt, MinScopeChangeSeq: minimum})

		after := backend.counters()
		if err != nil || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" || response.Retrieval.ScannedChunks != 8192 || response.Retrieval.SemanticCandidates != 100 || response.Retrieval.LexicalCandidates != 100 || after["provider"]-before["provider"] != 1 {
			t.Fatalf("full real canonical consumer/min=%d: report=%#v provider_delta=%d err=%v", minimum, response.Retrieval, after["provider"]-before["provider"], err)
		}

		t.Logf("CONSUMER minimum=%d four_prepared_windows=4 provider_calls=1 report=%+v results=%d counters=%v", minimum, response.Retrieval, len(response.Results), after)
	}
}

func BenchmarkMatched(b *testing.B) {
	for _, name := range []string{"small", "lexical16384", "semantic8192"} {
		c, backend := matchedFixture(b, name)

		modes := []string{"lexical"}
		if name == "semantic8192" {
			modes = []string{"hybrid-ready"}
		}

		for _, mode := range modes {
			for _, filter := range []string{"none", "source", "conversation", "key"} {
				for _, cache := range []string{"warm", "reopen"} {
					b.Run(name+"/"+mode+"/"+filter+"/"+cache, func(b *testing.B) {
						projection := backend.lexical()
						if mode == "hybrid-ready" {
							projection = backend.hybrid()
						}

						service, err := memoryread.NewRecallService(c.reader, projection)
						if err != nil {
							b.Fatal(err)
						}

						request := m.RecallRequest{Scope: c.scope, Query: "engineer run test", Limit: 10, AsOf: &matchedAt}
						if mode == "hybrid-ready" {
							request.Query = matchedQuery()
							request.Limit = 25
						}

						switch filter {
						case "source":
							request.SourceID = "baseline-source"
						case "conversation":
							request.ConversationID = "baseline-conversation"
						case "key":
							request.MemoryKey = "bench-00000"
							if name == "small" {
								request.MemoryKey = "ru"
							}
						}

						if cache == "warm" {
							if _, err := service.Search(context.Background(), request); err != nil {
								b.Fatal(err)
							}
						}

						b.ReportAllocs()

						before := backend.counters()

						var reopenNS, wallNS int64

						b.ResetTimer()

						for range b.N {
							var coldStart time.Time

							if cache == "reopen" {
								b.StopTimer()

								coldStart = time.Now()

								if err := backend.reopen(context.Background()); err != nil {
									b.Fatal(err)
								}

								projection = backend.lexical()
								if mode == "hybrid-ready" {
									projection = backend.hybrid()
								}

								service, err = memoryread.NewRecallService(c.reader, projection)
								if err != nil {
									b.Fatal(err)
								}

								reopenNS += time.Since(coldStart).Nanoseconds()

								b.StartTimer()
							}

							response, err := service.Search(context.Background(), request)
							if err != nil {
								b.Fatal(err)
							}

							if mode == "hybrid-ready" && (response.Retrieval == nil || response.Retrieval.Effective != "hybrid" || response.Retrieval.ScannedChunks > 8192) {
								b.Fatalf("not ready hybrid: %#v", response.Retrieval)
							}

							if cache == "reopen" {
								b.StopTimer()

								wallNS += time.Since(coldStart).Nanoseconds()
							}
						}

						b.StopTimer()

						if cache == "reopen" {
							b.ReportMetric(float64(reopenNS)/float64(b.N), "reopen-ns/op")
							b.ReportMetric(float64(wallNS)/float64(b.N), "full-wall-ns/op")
						}

						for key, value := range backend.counters() {
							b.ReportMetric(float64(value-before[key])/float64(b.N), key+"/op")
						}
					})
				}
			}
		}
	}
}

func BenchmarkMatchedFallback(b *testing.B) {
	for _, status := range []string{"missing", "degraded"} {
		c, backend := matchedFixture(b, "fallback-"+status)
		if status == "degraded" {
			if err := backend.semantic(context.Background(), status); err != nil {
				b.Fatal(err)
			}
		}

		b.Run(status, func(b *testing.B) {
			service, err := memoryread.NewRecallService(c.reader, backend.hybrid())
			if err != nil {
				b.Fatal(err)
			}

			request := m.RecallRequest{Scope: c.scope, Query: "engineer run test", Limit: 10, AsOf: &matchedAt}
			if _, err := service.Search(context.Background(), request); err != nil {
				b.Fatal(err)
			}

			before := backend.counters()

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				response, err := service.Search(context.Background(), request)
				if err != nil {
					b.Fatal(err)
				}

				if response.Retrieval == nil || response.Retrieval.Effective != "degraded" || response.Retrieval.ScannedChunks != 0 {
					b.Fatalf("not bounded lexical fallback: %#v", response.Retrieval)
				}
			}

			b.StopTimer()

			for key, value := range backend.counters() {
				b.ReportMetric(float64(value-before[key])/float64(b.N), key+"/op")
			}
		})
	}
}

func BenchmarkMatchedMaintenance(b *testing.B) {
	c, backend := matchedFixture(b, "lexical16384")
	for _, operation := range []string{"rebuild", "incremental-copy"} {
		b.Run(operation, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				var err error
				if operation == "rebuild" {
					err = backend.rebuild(context.Background(), fmt.Sprintf("rebuild-%d-%d", time.Now().UnixNano(), i))
				} else {
					err = backend.copy(context.Background())
				}

				if err != nil {
					b.Fatal(err)
				}
			}

			b.StopTimer()
			b.Logf("FOOTPRINT open=%v real_hydration_max_ids=%d current_seq=%d", backend.bytes(), c.reader.maxIDs, c.seq)
		})
	}
}

func projectionFootprint(root string) map[string]int64 {
	out := map[string]int64{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			info, e := d.Info()
			if e != nil {
				return e
			}

			rel, _ := filepath.Rel(root, path)
			out[rel] = info.Size()
		}

		return nil
	})

	return out
}
