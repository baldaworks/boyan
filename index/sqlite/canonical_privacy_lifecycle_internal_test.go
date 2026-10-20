//go:build integration

package sqlite

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalPrivacyCallsOutsideSQLUnderScope(t *testing.T) {
	for _, entry := range []string{"apply", "copy", "Stage"} {
		t.Run(entry, func(t *testing.T) {
			store, _ := openTestStore(t, true)
			scope := lexicalTestScope

			if entry == "Stage" {
				g, _, _ := semanticFixture()
				scope = g.Scope
				seedPrivacySemanticSource(t, store, g)
			}

			privacy := newPrivacyTestAuthority(scope, "revision")
			record := applyRecord("revision")
			record.Scope = scope

			a, err := NewSQLiteCanonicalApplier(store,
				&applyReader{records: map[string]memory.RecallRecord{"revision": record}}, privacy)
			if err != nil {
				t.Fatal(err)
			}

			if entry == "copy" {
				seedLexical(t, store, "active", scope, []lexicalFixture{{id: "revision", raw: "needle"}})
			}

			calls := 0
			privacy.before = func(ctx context.Context) {
				calls++

				assertPrivacyCallbackResources(t, ctx, store, scope)
			}

			switch entry {
			case "apply":
				err = a.Apply(t.Context(), scope, "new", nil, []memory.MemoryRevision{{RevisionID: "revision"}})
			case "copy":
				err = a.Apply(t.Context(), scope, "active", nil, nil)
			case "Stage":
				g, r, c := semanticFixture()
				_, err = store.Stage(t.Context(), g, r, c)
			}

			if err != nil || calls < 3 {
				t.Fatalf("admission callbacks missing or failed: %d %v", calls, err)
			}
		})
	}
}

func assertPrivacyCallbackResources(t *testing.T, ctx context.Context, store *Store, scope memory.Scope) {
	t.Helper()

	value, found := store.scopeWrites.Load(scope)
	if !found {
		t.Fatal("canonical callback before scope admission")
	}

	lease := value.(chan struct{})
	select {
	case <-lease:
		lease <- struct{}{}

		t.Fatal("canonical callback without held full-scope lease")
	default:
	}

	if store.reader.Stats().InUse != 0 || store.writer.Stats().InUse != 0 {
		t.Fatal("canonical callback retained a SQL connection/transaction")
	}

	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	writer, err := store.writer.Conn(bounded)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = writer.Close() }()

	first, err := store.reader.Conn(bounded)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = first.Close() }()

	second, err := store.reader.Conn(bounded)
	if err != nil {
		t.Fatal(err)
	}

	_ = second.Close()
}

func TestCanonicalPrivacyClosingStoreCannotBind(t *testing.T) {
	store, _ := openTestStore(t, false)

	_, release, err := store.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.CloseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if _, err := NewSQLiteCanonicalApplier(store, &applyReader{},
		newPrivacyTestAuthority(lexicalTestScope)); !errors.Is(err, ErrClosed) {
		t.Fatalf("closing store accepted authority: %v", err)
	}

	release()

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalPrivacyEmptyStageAndInvalidZeroCoverage(t *testing.T) {
	store, _ := openTestStore(t, true)
	g, coverage, _ := semanticFixture()
	seedPrivacySemanticSource(t, store, g)

	privacy := newPrivacyTestAuthority(g.Scope, "revision")
	if err := store.bindCanonicalPrivacy(privacy); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Stage(t.Context(), g, nil, nil); err != nil {
		t.Fatalf("valid bound identity-free empty Stage rejected: %v", err)
	}

	coverage[0].ChunkCount = 0

	g.BuildID = "invalid-zero-count"
	if _, err := store.Stage(t.Context(), g, coverage, nil); !errors.Is(err, semantic.ErrInvalid) {
		t.Fatalf("portable zero-count coverage policy changed: %v", err)
	}

	assertPrivacyCount(t, store, "SELECT count(*) FROM semantic_build WHERE build_id='invalid-zero-count'", 0)
}

func TestCanonicalPrivacyRevokedCannotLazyCopy(t *testing.T) {
	for _, target := range []string{"source", "destination"} {
		t.Run(target, func(t *testing.T) {
			store, _ := openTestStore(t, false)
			seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "revision", raw: "needle"}})
			execTest(t, store.writer, "UPDATE lexical_build SET record_count=1")

			a, err := NewSQLiteCanonicalApplier(store, &applyReader{},
				newPrivacyTestAuthority(lexicalTestScope, "revision"))
			if err != nil {
				t.Fatal(err)
			}

			unlock, err := store.lockScope(t.Context(), lexicalTestScope)
			if err != nil {
				t.Fatal(err)
			}

			stage, err := a.stage(t.Context(), lexicalTestScope, "active", false)

			unlock()

			if err != nil {
				t.Fatal(err)
			}

			owner := stage.source
			if target == "destination" {
				owner = stage.id
			}

			execTest(t, store.writer, "UPDATE lexical_build SET revoked=1 WHERE physical_build_id=?", owner)

			err = a.CommitProjectionManifest(t.Context(), publicationManifest("active", math.MaxUint64))
			if !privacyRetryableConflict(err) {
				t.Fatalf("revoked owner was copied/sealed: %v", err)
			}

			assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record WHERE physical_build_id=?", 0, stage.id)
		})
	}
}

func TestCanonicalPrivacyFailurePreservesLastCertifiedView(t *testing.T) {
	store, _ := openTestStore(t, false)
	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "revision", raw: "needle"}})
	execTest(t, store.writer, "UPDATE lexical_build SET record_count=1")

	privacy := newPrivacyTestAuthority(lexicalTestScope, "revision")

	a, err := NewSQLiteCanonicalApplier(store, &applyReader{}, privacy)
	if err != nil {
		t.Fatal(err)
	}

	privacy.err = errors.New("sensitive canonical failure")
	if err := a.Apply(t.Context(), lexicalTestScope, "active", nil, nil); !privacyCode(err, memory.CodeStoreFailure) {
		t.Fatalf("authority failure not propagated: %v", err)
	}

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	physical, found, err := snapshot.committedLexical(lexicalTestScope, "active", math.MaxUint64, true)
	if err != nil || !found || physical.id != "active" {
		t.Fatalf("last certificate damaged: %v %v", found, err)
	}
}
