//nolint:cyclop,gocognit,funlen,lll // Real SQLite/Badger fixture matrices keep exact ownership and finite-page assertions together.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalDenialReconcileRealPublicationAndLatePublish(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, _ := openTestStore(t, true)
		g, coverage, chunks := semanticFixture()
		chunks[0].ItemID = "revision"
		coverage[0].ItemID = "revision"

		denialCanonicalRevision(t, s, g.Scope, "revision", "source")
		denialCanonicalRevision(t, s, g.Scope, "eligible", "source")
		seedPrivacySemanticSource(t, store, g)

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if _, err := store.Stage(t.Context(), g, coverage, chunks); err != nil {
			t.Fatal(err)
		}

		if err := store.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
			t.Fatal(err)
		}

		active, found, err := store.ActiveState(t.Context(), g.Scope)
		if err != nil || !found || active.PublicationEpoch == 0 {
			t.Fatalf("real publication %#v %v", active, err)
		}

		target := g
		target.BuildID = "pending-target"

		chunks[0].BuildID = target.BuildID
		if _, err := store.Stage(t.Context(), target, coverage, chunks); err != nil {
			t.Fatal(err)
		}

		peer := g
		peer.BuildID = "safe-peer"
		chunks[0].BuildID = peer.BuildID
		chunks[0].RevisionID = "eligible"
		chunks[0].ItemID = "eligible"
		coverage[0].RevisionID = "eligible"

		coverage[0].ItemID = "eligible"
		if _, err := store.Stage(t.Context(), peer, coverage, chunks); err != nil {
			t.Fatal(err)
		}

		empty := g

		empty.BuildID = "empty-peer"
		if _, err := store.Stage(t.Context(), empty, nil, nil); err != nil {
			t.Fatal(err)
		}

		if err := s.DenyRevision(t.Context(), g.Scope, "revision", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), g.Scope); err != nil {
			t.Fatal(err)
		}

		degraded, found, err := store.ActiveState(t.Context(), g.Scope)
		if err != nil || !found || degraded.Status != semantic.StatusDegraded || degraded.PublicationEpoch != active.PublicationEpoch {
			t.Fatalf("degraded marker %#v %v", degraded, err)
		}

		publication := semantic.Publication{View: g.View(), BuildID: target.BuildID, ExpectedActiveBuildID: g.BuildID, ExpectedActiveEpoch: active.PublicationEpoch}
		if err := store.Publish(t.Context(), publication); !errors.Is(err, semantic.ErrConflict) && !errors.Is(err, semantic.ErrCorrupt) {
			t.Fatalf("revoked candidate published %v", err)
		}

		publication.BuildID = peer.BuildID
		if err := store.Publish(t.Context(), publication); err != nil {
			t.Fatalf("healthy peer lost epoch authority %v", err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), g.Scope); err != nil {
			t.Fatal(err)
		}

		winner, found, err := store.ActiveState(t.Context(), g.Scope)
		if err != nil || !found || winner.BuildID != peer.BuildID || winner.Status != semantic.StatusReady {
			t.Fatalf("healthy newer winner changed %#v %v", winner, err)
		}

		assertPrivacyCount(t, store, `SELECT complete FROM semantic_build WHERE build_id='empty-peer'`, 1)
	})
}

func TestCanonicalDenialReconcileCandidateRewrite(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, rewrite := range []string{"ordinal", "epoch", "owner"} {
			t.Run("semantic "+rewrite, func(t *testing.T) {
				s := newCanonical(t)
				store, _ := openTestStore(t, true)
				seedDenialSemantic(t, store, lexicalTestScope, "owner", "target", false, true)
				seedDenialSemantic(t, store, lexicalTestScope, "peer", "target", false, false)

				if err := s.DenyRevision(t.Context(), lexicalTestScope, "target", lexicalTestTime); err != nil {
					t.Fatal(err)
				}

				rows, err := store.privacyCandidates(t.Context(), lexicalTestScope, privacyChunk, 0)
				if err != nil || len(rows) != 1 {
					t.Fatalf("rows %v %v", rows, err)
				}

				proof, denied, err := store.provePrivacyRow(t.Context(), s, rows[0])
				if err != nil || !denied {
					t.Fatalf("proof %#v %v", proof, err)
				}

				switch rewrite {
				case "ordinal":
					execTest(t, store.writer, `UPDATE semantic_chunk SET ordinal=1`)
				case "epoch":
					execTest(t, store.writer, `UPDATE semantic_build SET epoch=? WHERE build_id='owner'`, encodeUint64(2))
				case "owner":
					execTest(t, store.writer, `UPDATE semantic_chunk SET build_id='peer'`)
				}

				if err := store.deletePrivacyProofs(t.Context(), []privacyProof{proof}); err != nil {
					t.Fatal(err)
				}

				assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_chunk`, 1)
				assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_build WHERE complete=1`, 2)
			})
		}

		for _, rewrite := range []string{"revision", "item", "owner", "source", "rowid", "scope"} {
			t.Run(rewrite, func(t *testing.T) {
				s := newCanonical(t)
				store, _ := openTestStore(t, false)
				denialCanonicalRevision(t, s, lexicalTestScope, "target", "safe")
				seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle", sources: []string{"denied"}}})
				seedLexical(t, store, "peer-owner", lexicalTestScope, nil)

				if err := s.DenySource(t.Context(), lexicalTestScope, "denied", lexicalTestTime); err != nil {
					t.Fatal(err)
				}

				rows, err := store.privacyCandidates(t.Context(), lexicalTestScope, privacyLexical, 0)
				if err != nil || len(rows) != 1 {
					t.Fatalf("rows %v %v", rows, err)
				}

				proof, denied, err := store.provePrivacyRow(t.Context(), s, rows[0])
				if err != nil || !denied || !proof.sourceMembership {
					t.Fatalf("proof %#v %v", proof, err)
				}

				switch rewrite {
				case "revision":
					execTest(t, store.writer, `UPDATE lexical_record SET revision_id='eligible'`)
				case "item":
					execTest(t, store.writer, `UPDATE lexical_record SET item_id='eligible'`)
				case "owner":
					execTest(t, store.writer, `UPDATE lexical_record SET physical_build_id='peer-owner'`)
				case "source":
					execTest(t, store.writer, `UPDATE lexical_source SET id='safe'`)
				case "rowid":
					execTest(t, store.writer, `DELETE FROM lexical_fts`)
					execTest(t, store.writer, `DELETE FROM lexical_record`)
					seedLexical(t, store, "recycled", lexicalTestScope, []lexicalFixture{{id: "eligible", raw: "needle"}})
				case "scope":
					execTest(t, store.writer, `UPDATE lexical_build SET scope_kind='personal' WHERE physical_build_id='owner'`)
				}

				if err := store.deletePrivacyProofs(t.Context(), []privacyProof{proof}); err != nil {
					t.Fatal(err)
				}

				assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)
				assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_build WHERE revoked=1`, 0)
			})
		}
	})
}

func TestCanonicalDenialReconcileProofByteGroups(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, _ := openTestStore(t, false)
		source := "source-" + strings.Repeat("x", 17000)

		var fixtures []lexicalFixture

		for i := range 130 {
			id := fmt.Sprintf("evidence-%03d", i)
			denialCanonicalRevision(t, s, lexicalTestScope, id, source)
			fixtures = append(fixtures, lexicalFixture{id: id, raw: "needle"})
		}

		seedLexical(t, store, "owner", lexicalTestScope, fixtures)

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if err := s.DenySource(t.Context(), lexicalTestScope, source, lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		execTest(t, store.writer, `CREATE TRIGGER proof_page_failure BEFORE DELETE ON lexical_record WHEN OLD.rowid=128 BEGIN SELECT RAISE(ABORT,'injected page failure'); END`)

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err == nil {
			t.Fatal("fault did not fire")
		}

		var remaining int
		if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&remaining); err != nil {
			t.Fatal(err)
		}

		if remaining >= 130 {
			t.Fatalf("oversized aggregate proof page was not split: remaining=%d", remaining)
		}

		execTest(t, store.writer, `DROP TRIGGER proof_page_failure`)

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 0)
	})
}

func TestCanonicalDenialReconcileBoundedPagesAndMemberships(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, _ := openTestStore(t, false)
		denialCanonicalRevision(t, s, lexicalTestScope, "safe", "source")

		if err := s.DenyRevision(t.Context(), lexicalTestScope, "denied", lexicalTestTime); err != nil {
			t.Fatal(err)
		}
		// Repeated immutable IDs in distinct retained physical builds keep this real
		// canonical fixture small while exceeding the semantic cap in lexical SQL.
		seedLexical(t, store, "first", lexicalTestScope, []lexicalFixture{{id: "safe", raw: "needle"}})

		if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
			for i := range 8322 {
				build := fmt.Sprintf("history-%05d", i)
				if _, err := tx.ExecContext(ctx, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark) VALUES(?,?,?,?,?)`, build, lexicalTestScope.Kind, lexicalTestScope.Key, "generation", encodeUint64(1)); err != nil {
					return err
				}

				id := "denied"
				if i < 129 {
					id = "safe"
				}

				result, err := tx.ExecContext(ctx, `INSERT INTO lexical_record(physical_build_id,item_id,revision_id,revision,change_seq,kind,created_at,observed_at) VALUES(?,?,?, ?,?,'state',?,?)`, build, id, id, encodeUint64(1), encodeUint64(1), encodeTime(lexicalTestTime), encodeTime(lexicalTestTime))
				if err != nil {
					return err
				}

				row, err := result.LastInsertId()
				if err != nil {
					return err
				}

				if err := insertLexicalFTS(ctx, tx, row, "needle", "", ""); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		calls := 0

		a := &denialFaultAuthority{canonicalPrivacyReader: s, before: func(context.Context) { calls++ }, check: func(ctx context.Context) { assertPrivacyCallbackResources(t, ctx, store, lexicalTestScope) }, load: func(ctx context.Context, req memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
			if len(req.RevisionIDs) != 1 {
				t.Fatal("non-single structural read")
			}

			return s.LoadCanonicalRevisions(ctx, req)
		}}
		if err := store.bindCanonicalPrivacy(a); err != nil {
			t.Fatal(err)
		}

		page, err := store.privacyCandidates(t.Context(), lexicalTestScope, privacyLexical, 0)
		if err != nil || len(page) != 128 {
			t.Fatalf("candidate bound %d %v", len(page), err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 130)

		if calls < 8323 {
			t.Fatalf("scan skipped preserved/target rows: %d", calls)
		}
		// One oversized aggregate membership row is isolated. Source IDs are read in
		// finite groups; no text/vector payload is needed to discover the last deny.
		large := strings.Repeat("x", 17000)

		var sources []string
		for i := range 260 {
			sources = append(sources, fmt.Sprintf("%03d-%s", i, large))
		}

		denialCanonicalRevision(t, s, lexicalTestScope, "fanout", "source")
		seedLexical(t, store, "fanout", lexicalTestScope, []lexicalFixture{{id: "fanout", raw: "needle", sources: sources}})

		var id int64
		if err := store.reader.QueryRow(`SELECT rowid FROM lexical_record WHERE revision_id='fanout'`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		page, err = store.privacyCandidates(t.Context(), lexicalTestScope, privacyLexical, id-1)
		if err != nil || len(page) != 1 || page[0].estimatedBytes <= lexicalPageBytes {
			t.Fatalf("oversized isolation %d %v", len(page), err)
		}

		members, err := store.privacyMemberships(t.Context(), page[0], "")
		if err != nil || len(members) == 0 || len(members) > 128 {
			t.Fatalf("membership bound %d %v", len(members), err)
		}

		var bytes uint64
		for _, id := range members {
			bytes += uint64(len(id)) + 8
		}

		if bytes > lexicalPageBytes {
			t.Fatalf("normal membership page %d", bytes)
		}

		if err := s.DenySource(t.Context(), lexicalTestScope, sources[len(sources)-1], lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record WHERE revision_id='fanout'`, 0)
	})
}

func seedDenialSemantic(t *testing.T, store *Store, scope memory.Scope, build, revision string, active, chunks bool) {
	t.Helper()
	seedZeroCoverage(t, store, build, scope, revision)

	epoch := uint64(0)
	if active {
		epoch = 7
	}

	execTest(t, store.writer, `UPDATE semantic_build SET epoch=?,revision_count=1,chunk_count=?,ready_at=? WHERE build_id=?`, encodeUint64(epoch), boolInt(chunks), encodeTime(lexicalTestTime), build)

	if active {
		execTest(t, store.writer, `INSERT INTO semantic_active VALUES(?,?,?,?)`, scope.Kind, scope.Key, build, encodeUint64(epoch))
	}

	if !chunks {
		return
	}

	err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE semantic_coverage SET chunk_count=1 WHERE build_id=?`, build); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `INSERT INTO semantic_chunk(build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,dimensions,embedding,kind,created_at,observed_at)
 VALUES(?,?,?, ?,0,'content','input',?,1,?,'state',?,?)`, build, revision, revision, encodeUint64(1), encodeUint64(1), []byte{0, 0, 128, 63}, encodeTime(lexicalTestTime), encodeTime(lexicalTestTime))
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_source VALUES(?,'source')`, id); err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO semantic_conversation VALUES(?,'conversation')`, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func TestCanonicalDenialReconcileAllFamiliesAndOwners(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)

		store, _ := openTestStore(t, true)
		for _, id := range []string{"denied", "safe"} {
			denialCanonicalRevision(t, s, lexicalTestScope, id, "source")
		}

		seedLexical(t, store, "previous", lexicalTestScope, []lexicalFixture{{id: "denied", raw: "needle", sources: []string{"source"}, conversations: []string{"conversation"}}})
		seedLexical(t, store, "current", lexicalTestScope, []lexicalFixture{{id: "safe", raw: "needle", sources: []string{"source"}, conversations: []string{"conversation"}}})
		seedDenialSemantic(t, store, lexicalTestScope, "old-active", "denied", true, true)
		seedDenialSemantic(t, store, lexicalTestScope, "target-pending", "denied", false, true)
		seedDenialSemantic(t, store, lexicalTestScope, "peer-pending", "safe", false, true)
		seedDenialSemantic(t, store, lexicalTestScope, "empty-peer", "safe", false, false)
		execTest(t, store.writer, `DELETE FROM semantic_coverage WHERE build_id='empty-peer'`)

		peer := memory.Scope{Kind: memory.ScopeKindPersonal, Key: lexicalTestScope.Key}
		seedDenialSemantic(t, store, peer, "kind-peer", "denied", false, true)

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if err := s.DenyRevision(t.Context(), lexicalTestScope, "denied", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		snapshot, err := store.read(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = snapshot.Close() }()

		var pinned int
		if err := snapshot.tx.QueryRowContext(t.Context(), `SELECT count(*) FROM semantic_chunk WHERE build_id='old-active'`).Scan(&pinned); err != nil || pinned != 1 {
			t.Fatalf("pin %d %v", pinned, err)
		}

		for range 2 {
			if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
				t.Fatal(err)
			}

			for _, table := range []string{"semantic_chunk", "semantic_coverage"} {
				assertPrivacyCount(t, store, `SELECT count(*) FROM `+table+` WHERE build_id IN ('old-active','target-pending')`, 0)
			}

			for _, table := range []string{"semantic_chunk", "semantic_coverage", "semantic_source", "semantic_conversation", "lexical_record", "lexical_source", "lexical_conversation", "lexical_fts"} {
				want := 1
				if table[:8] == "semantic" {
					want = 2
				}

				assertPrivacyCount(t, store, `SELECT count(*) FROM `+table, want)
			}

			assertPrivacyCount(t, store, `SELECT complete FROM semantic_build WHERE build_id='peer-pending'`, 1)
			assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_build WHERE build_id='empty-peer' AND complete=1 AND epoch=?`, 1, encodeUint64(0))
			assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_active WHERE build_id='old-active' AND epoch=?`, 1, encodeUint64(7))
		}

		if err := snapshot.tx.QueryRowContext(t.Context(), `SELECT count(*) FROM semantic_chunk WHERE build_id='old-active'`).Scan(&pinned); err != nil || pinned != 1 {
			t.Fatalf("MVCC %d %v", pinned, err)
		}
	})
}

func TestCanonicalDenialReconcileZeroCoverageAfterLostSQLProof(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, _ := openTestStore(t, true)
		denialCanonicalRevision(t, s, lexicalTestScope, "zero", "denied-source")
		seedDenialSemantic(t, store, lexicalTestScope, "zero-owner", "zero", false, false)

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if err := s.DenySource(t.Context(), lexicalTestScope, "denied-source", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_coverage`, 0)
		assertPrivacyCount(t, store, `SELECT complete FROM semantic_build WHERE build_id='zero-owner'`, 0)
	})
}

func denialCanonicalStore(t *testing.T) denialCanonicalOwner {
	t.Helper()

	s, err := canonicalbadger.OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	return s
}

func denialCanonicalRevision(t *testing.T, s denialCanonicalOwner, scope memory.Scope, id string, sources ...string) {
	t.Helper()

	state, err := s.LoadScopeState(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("payload-" + id)
	digest := sha256.Sum256(data)
	ref := memory.PayloadRef{ID: "payload-" + id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}

	r := memory.MemoryRevision{SchemaVersion: memory.MemorySchemaVersion, RevisionID: id, ItemID: id,
		Revision: 1, Temporal: memory.Temporal{ObservedAt: lexicalTestTime},
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: ref}
	for _, source := range sources {
		r.Evidence = append(r.Evidence, memory.EvidenceRef{SourceID: source, MessageID: "message", Role: memory.MessageRoleUser,
			StartByte: 0, EndByte: 1, AssertionMode: memory.AssertionModeUser})
	}

	_, err = s.ApplyCanonicalMutation(t.Context(), memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope, ExpectedScopeVersion: state.Version,
		Operation: memory.OperationRecord{OperationID: "op-" + id, Fingerprint: "fp-" + id, CommittedAt: lexicalTestTime},
		Items:     []memory.MemoryItem{{ItemID: id, Scope: scope, Kind: memory.MemoryKindEvent}}, Revisions: []memory.MemoryRevision{r},
		Payloads: []memory.CanonicalPayload{{Ref: ref, Data: data}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Omitting any of the three authority paths leaves its exact target behind;
// treating structural presence as denial deletes the independently eligible row.
func TestCanonicalDenialReconcileIdentityAuthority(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, _ := openTestStore(t, false)

		for _, id := range []string{"direct", "evidence", "membership", "eligible"} {
			sources := []string{"safe-source"}
			if id == "evidence" {
				sources = append(sources, "denied-source")
			}

			denialCanonicalRevision(t, s, lexicalTestScope, id, sources...)
		}

		seedLexical(t, store, "retained", lexicalTestScope, []lexicalFixture{
			{id: "direct", raw: "needle"}, {id: "evidence", raw: "needle"},
			{id: "membership", raw: "needle", sources: []string{"denied-source"}},
			{id: "eligible", raw: "needle", sources: []string{"safe-source"}},
		})

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if err := s.DenyRevision(t.Context(), lexicalTestScope, "direct", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := s.DenySource(t.Context(), lexicalTestScope, "denied-source", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record", 1)
		assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_record WHERE revision_id='eligible'", 1)
		assertPrivacyCount(t, store, "SELECT count(*) FROM lexical_fts", 1)
		assertPrivacyCount(t, store, "SELECT revoked FROM lexical_build WHERE physical_build_id='retained'", 1)

		a, err := NewSQLiteCanonicalApplier(store, &applyReader{}, s)
		if err != nil {
			t.Fatal(err)
		}

		if err := a.sealStage(t.Context(), memory.ProjectionManifest{Scope: lexicalTestScope, GenerationID: "retained", Watermark: 1}, "retained"); !privacyRetryableConflict(err) {
			t.Fatalf("revoked physical build resealed: %v", err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}
	})
}
