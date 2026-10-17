package sqlite

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Removing any admission branch admits the denied row into a new physical
// destination (or claims a denied semantic population), which this test catches.
func TestCanonicalPrivacyGuardsEveryMaterialProducer(t *testing.T) {
	for _, entry := range []string{
		"apply", "rebuild", "incremental copy", "commit lazy copy", "zero chunk Stage", "chunk Stage",
	} {
		for _, selector := range []string{"revision", "Evidence", "membership"} {
			if entry == "zero chunk Stage" && selector == "membership" {
				continue
			}

			t.Run(entry+"/"+selector, func(t *testing.T) {
				runPrivacyProducerCase(t, entry, selector)
			})
		}
	}
}

func runPrivacyProducerCase(t *testing.T, entry, selector string) {
	t.Helper()
	store, _ := openTestStore(t, true)
	scope := lexicalTestScope

	if strings.Contains(entry, "Stage") {
		g, _, _ := semanticFixture()
		scope = g.Scope
	}

	privacy := newPrivacyTestAuthority(scope, "revision", "safe")
	privacy.revisions["safe"] = memory.MemoryRevision{RevisionID: "safe"}
	record := applyRecord("revision")
	record.Scope = scope
	record.SourceIDs = []string{"membership"}
	reader := &applyReader{records: map[string]memory.RecallRecord{"revision": record}}

	a, err := NewSQLiteCanonicalApplier(store, reader, privacy)
	if err != nil {
		t.Fatal(err)
	}

	if entry == "incremental copy" || entry == "commit lazy copy" {
		seedLexical(t, store, "active", scope, []lexicalFixture{
			{id: "revision", raw: "needle", sources: []string{"membership"}}, {id: "safe", raw: "safe"},
		})
		execTest(t, store.writer, "UPDATE lexical_build SET record_count=2,source_count=1 "+
			"WHERE physical_build_id='active'")
	}

	if strings.Contains(entry, "Stage") {
		g, _, _ := semanticFixture()
		seedPrivacySemanticSource(t, store, g)
	}
	// Prepared input exists before the scope admission. A committed
	// denial precedes its release, including cached/raw copied inputs.
	unlock, err := store.lockScope(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}

	finished := make(chan error, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	go func() { finished <- executePrivacyProducer(ctx, store, a, scope, entry) }()

	denyTestPrivacy(privacy, selector)
	unlock()

	err = <-finished

	assertPrivacyProducerOutcome(t, store, a, scope, entry, err)

	if selector == "revision" && privacy.loads != 0 && !strings.Contains(entry, "copy") {
		t.Fatal("direct denial unnecessarily loaded structural material")
	}
}

func executePrivacyProducer(ctx context.Context, store *Store, a *SQLiteCanonicalApplier,
	scope memory.Scope, entry string) error {
	switch entry {
	case "apply":
		return a.Apply(ctx, scope, "new", nil, []memory.MemoryRevision{{RevisionID: "revision"}})
	case "rebuild":
		err := a.BeginProjectionRebuild(ctx, scope, "new")
		if err == nil {
			err = a.Apply(ctx, scope, "new", nil, []memory.MemoryRevision{{RevisionID: "revision"}})
		}

		return err
	case "incremental copy":
		return a.Apply(ctx, scope, "active", nil, nil)
	case "commit lazy copy":
		return a.CommitProjectionManifest(ctx, publicationManifest("active", math.MaxUint64))
	default:
		g, coverage, chunks := semanticFixture()
		chunks[0].SourceIDs = []string{"membership"}

		if entry == "zero chunk Stage" {
			coverage[0].ChunkCount = 0
			chunks = nil
		}

		_, err := store.Stage(ctx, g, coverage, chunks)

		return err
	}
}

func assertPrivacyProducerOutcome(t *testing.T, store *Store, a *SQLiteCanonicalApplier,
	scope memory.Scope, entry string, err error) {
	t.Helper()

	if strings.Contains(entry, "Stage") {
		want := semantic.ErrConflict
		if entry == "zero chunk Stage" {
			// Portable admission has always rejected zero-count
			// coverage. Persisted zero coverage is reconciler evidence.
			want = semantic.ErrInvalid
		}

		if !errors.Is(err, want) {
			t.Fatalf("denied Stage did not conflict: %v", err)
		}

		assertPrivacyCount(t, store, "SELECT count(*) FROM semantic_build", 0)
		assertPrivacyCount(t, store, "SELECT count(*) FROM semantic_coverage", 0)
		assertPrivacyCount(t, store, "SELECT count(*) FROM semantic_chunk", 0)

		return
	}

	if err != nil {
		t.Fatal(err)
	}

	stage := a.stages[scope]
	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record "+
		"WHERE physical_build_id=? AND revision_id='revision'", 0, stage.id)

	if strings.Contains(entry, "copy") {
		assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record "+
			"WHERE physical_build_id=? AND revision_id='safe'", 1, stage.id)
		assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record WHERE physical_build_id='active'", 2)
	}
}

func seedPrivacySemanticSource(t *testing.T, store *Store, g semantic.Generation) {
	t.Helper()
	execTest(t, store.writer, `INSERT INTO lexical_build
 (physical_build_id,scope_kind,scope_key,generation_id,watermark,state)
 VALUES('privacy-source',?,?,?,?,'committed')`, g.Scope.Kind, g.Scope.Key,
		g.LexicalGenerationID, encodeUint64(g.LexicalWatermark))
	execTest(t, store.writer, "INSERT INTO lexical_active VALUES(?,?,'privacy-source')", g.Scope.Kind, g.Scope.Key)
}

func assertPrivacyCount(t *testing.T, store *Store, query string, want int, args ...any) {
	t.Helper()

	var count int
	if err := store.reader.QueryRowContext(t.Context(), query, args...).Scan(&count); err != nil || count != want {
		t.Fatalf("count=%d want=%d err=%v", count, want, err)
	}
}

//nolint:cyclop // Error sources and subsequent fresh admission have distinct, explicit assertions.
func TestCanonicalPrivacyAuthorityErrorsAndFreshAdmission(t *testing.T) {
	for _, mode := range []string{"lookup", "missing", "wrong identity", "cancel", "unknown source"} {
		t.Run(mode, func(t *testing.T) {
			store, _ := openTestStore(t, false)
			privacy := newPrivacyTestAuthority(lexicalTestScope, "revision")
			reader := &applyReader{records: map[string]memory.RecallRecord{"revision": applyRecord("revision")}}

			a, err := NewSQLiteCanonicalApplier(store, reader, privacy)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			switch mode {
			case "lookup":
				privacy.err = errors.New("private canonical contents")
			case "missing":
				privacy.missing = true
			case "wrong identity":
				privacy.wrong = true
			case "cancel":
				privacy.before = func(context.Context) { cancel() }
			case "unknown source":
				delete(privacy.sources, "source")
			}

			err = a.Apply(ctx, lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "revision"}})
			if err == nil || strings.Contains(err.Error(), "private canonical contents") {
				t.Fatalf("authority failure admitted material or exposed contents: %v", err)
			}

			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}

			assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record", 0)
			assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_build WHERE state='committed'", 0)
		})
	}

	t.Run("fresh after scope reacquisition", testFreshPrivacyAdmission)
}

func denyTestPrivacy(privacy *privacyTestAuthority, selector string) {
	privacy.mu.Lock()
	switch selector {
	case "revision":
		privacy.denied["revision"] = true
	case "Evidence":
		privacy.sources["evidence"] = true
	case "membership":
		privacy.sources["membership"] = true
	}
	privacy.mu.Unlock()
}

func testFreshPrivacyAdmission(t *testing.T) {
	store, _ := openTestStore(t, false)
	privacy := newPrivacyTestAuthority(lexicalTestScope, "revision")
	reader := &applyReader{records: map[string]memory.RecallRecord{"revision": applyRecord("revision")}}

	a, err := NewSQLiteCanonicalApplier(store, reader, privacy)
	if err != nil {
		t.Fatal(err)
	}

	revisions := []memory.MemoryRevision{{RevisionID: "revision"}}
	if err := a.Apply(t.Context(), lexicalTestScope, "new", nil, revisions); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record", 1)
	privacy.denyRevision("revision")

	if err := a.Apply(t.Context(), lexicalTestScope, "new", nil, revisions); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record", 0)
}
