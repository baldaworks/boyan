//go:build integration

// Retains the original canonical-to-search identity assertions on both SQL authorities.

package storefixture_test

import (
	"database/sql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	search "github.com/baldaworks/boyan/index/sqlite"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalAcceptsConversationBeyondSQLitePageBudget(t *testing.T) {
	forEachCoordinatorAuthority(t, func(t *testing.T, backend canonicalsqltest.Backend) {
		verifyCanonicalConversationIdentity(t, backend, strings.Repeat("c", 2*1024*1024+1))
	})
}

func TestCanonicalLeadingNULConversationRetainsExactSQLiteMembership(t *testing.T) {
	forEachCoordinatorAuthority(t, func(t *testing.T, backend canonicalsqltest.Backend) {
		verifyCanonicalConversationIdentity(t, backend, "\x00cccccccc")
		verifyCanonicalConversationIdentity(t, backend, "\x00")
	})
}

func verifyCanonicalConversationIdentity(t *testing.T, backend canonicalsqltest.Backend, conversation string) {
	t.Helper()

	fixture := storefixture.New(t, backend)

	store := fixture.Store.(projectionOwner)
	requireProjectionNoError(t, store.PrepareSourcePrivacy(t.Context()))

	defer func() { _ = store.Close() }()

	scope := memory.Scope{Key: "canonical:large-identity", Kind: memory.ScopeKindPersonal}
	now := time.Now().UTC()

	turn, err := memory.NewTurn(
		scope,
		memory.ConversationRef{ConversationID: conversation, AgentConversationID: "agent"},
		"turn",
		now,
		"remember",
		"remembered",
	)
	requireProjectionNoError(t, err)

	evidence := projectionTurnEvidence(t, turn)

	mutation := canonicalReaderTestMutation(scope, now, evidence, turn)
	source := privacyHistoryHeader(t, turn)
	mutation.Sources[0].Payload = privacyRef("source-large", source)
	mutation.Messages[0].Payload = privacyRef("message-large", []byte(turn.Messages[0].Text))
	mutation.Messages[1].Payload = privacyRef("message-large-assistant", []byte(turn.Messages[1].Text))
	mutation.Revisions[0].Payload = privacyRef("revision-large", []byte("remembered"))

	mutation.Payloads = []memory.CanonicalPayload{
		{Ref: mutation.Sources[0].Payload, Data: source},
		{Ref: mutation.Messages[0].Payload, Data: []byte(turn.Messages[0].Text)},
		{Ref: mutation.Messages[1].Payload, Data: []byte(turn.Messages[1].Text)},
		{Ref: mutation.Revisions[0].Payload, Data: []byte("remembered")},
	}
	if _, err = store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	reader := publicReader(t, store)

	records, err := reader.LoadRecallRecords(t.Context(), scope, []string{mutation.Revisions[0].RevisionID})
	if err != nil || len(records) != 1 {
		t.Fatalf("canonical hydration: %d %v", len(records), err)
	}

	if len(records[0].ConversationIDs) != 1 || records[0].ConversationIDs[0] != conversation {
		t.Fatal("canonical identity changed")
	}

	t.Logf("canonical accepted and hydrated indivisible conversation identity: %d bytes", len(conversation))
	verifyCanonicalLargeSQLite(t, reader, store, store, scope, mutation.Revisions[0], conversation, now)
}

func verifyCanonicalLargeSQLite(
	t *testing.T,
	reader memory.RecallCanonicalReader,
	checkpoints memory.ProjectionCheckpointStore,
	canonical projectionOwner,
	scope memory.Scope,
	revision memory.MemoryRevision,
	conversation string,
	now time.Time,
) {
	t.Helper()

	dir := t.TempDir()
	requireProjectionNoError(t, os.Chmod(dir, 0o700))

	projection, err := search.Open(t.Context(), dir, search.Options{})
	requireProjectionNoError(t, err)

	defer func() { _ = projection.Close() }()

	applier, err := search.NewSQLiteCanonicalApplier(projection, reader, canonical)
	requireProjectionNoError(t, err)

	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	requireProjectionNoError(t, applier.BeginProjectionRebuild(t.Context(), scope, "canonical-large"))

	requireProjectionNoError(
		t,
		applier.Apply(t.Context(), scope, "canonical-large", nil, []memory.MemoryRevision{revision}),
	)

	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-large", 1, now)
	// A read-only connection inspects logical cleanup; publication uses only the
	// real applier and canonical checkpoint APIs.
	db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
	requireProjectionNoError(t, err)

	defer func() { _ = db.Close() }()

	assertProjectionConversationHit(
		t,
		projection,
		scope,
		revision,
		conversation,
		now,
		"real canonical->SQL filter: %+v %v",
	)

	requireProjectionNoError(t, projection.Close())

	projection, applier = reopenLargeProjection(t, dir, reader, checkpoints, canonical, scope)
	assertIncrementalConversationCopies(t, db, conversation)
	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-large", 1, now)

	verifyLargeProjectionRebuildAndForget(t, projection, applier, db, checkpoints, scope, revision, conversation, now)
}

func assertIncrementalConversationCopies(t *testing.T, db *sql.DB, conversation string) {
	t.Helper()

	var count int

	if err := db.QueryRow(`SELECT count(*) FROM lexical_conversation WHERE id=?`, conversation).
		Scan(&count); err != nil || count != 2 {
		t.Fatalf("incremental exact copy: %d %v", count, err)
	}
}

func reopenLargeProjection(t *testing.T, dir string, reader memory.RecallCanonicalReader,
	checkpoints memory.ProjectionCheckpointStore, canonical projectionOwner, scope memory.Scope,
) (*search.Store, *search.SQLiteCanonicalApplier) {
	t.Helper()

	projection, err := search.Open(t.Context(), dir, search.Options{})
	requireProjectionNoError(t, err)

	t.Cleanup(func() { _ = projection.Close() })

	applier, err := search.NewSQLiteCanonicalApplier(projection, reader, canonical)
	requireProjectionNoError(t, err)

	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	if resumed, err := applier.ResumeProjection(t.Context(), scope, "canonical-large"); err != nil || !resumed {
		t.Fatalf("real canonical reopen resume: %v %v", resumed, err)
	}

	requireProjectionNoError(t, applier.Apply(t.Context(), scope, "canonical-large", nil, nil))

	return projection, applier
}

func verifyLargeProjectionRebuildAndForget(
	t *testing.T,
	projection *search.Store,
	applier *search.SQLiteCanonicalApplier,
	db *sql.DB,
	checkpoints memory.ProjectionCheckpointStore,
	scope memory.Scope,
	revision memory.MemoryRevision,
	conversation string,
	now time.Time,
) {
	t.Helper()

	requireProjectionNoError(t, applier.BeginProjectionRebuild(t.Context(), scope, "canonical-rebuilt"))

	requireProjectionNoError(
		t,
		applier.Apply(t.Context(), scope, "canonical-rebuilt", nil, []memory.MemoryRevision{revision}),
	)

	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-rebuilt", 1, now.Add(time.Second))

	assertProjectionConversationHit(
		t,
		projection,
		scope,
		revision,
		conversation,
		now,
		"oversized rebuild publication: %+v %v",
	)

	requireProjectionNoError(t, applier.ScrubCanonicalForget(t.Context(), scope, nil, []string{revision.RevisionID}))

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("logical forget: %d %v", count, err)
	}
}

func publishCanonicalLargeSQLite(
	t *testing.T,
	applier *search.SQLiteCanonicalApplier,
	checkpoints memory.ProjectionCheckpointStore,
	scope memory.Scope,
	generation string,
	watermark uint64,
	now time.Time,
) {
	t.Helper()

	manifest := memory.ProjectionManifest{Scope: scope, ProjectionID: "sqlite", GenerationID: generation,
		Status: memory.ProjectionGenerationDirty, Watermark: watermark, UpdatedAt: now}
	requireProjectionNoError(t, checkpoints.MarkProjectionDirty(t.Context(), manifest))

	requireProjectionNoError(t, applier.CommitProjectionManifest(t.Context(), manifest))

	requireProjectionNoError(t, checkpoints.AdvanceProjectionWatermark(t.Context(), manifest, watermark, now))

	requireProjectionNoError(t, checkpoints.ActivateProjectionGeneration(t.Context(), manifest, now))

	requireProjectionNoError(t, applier.Activate(t.Context(), scope, generation))
}

func projectionTurnEvidence(t *testing.T, turn memory.Turn) memory.EvidenceRef {
	t.Helper()

	evidence, err := memory.NewEvidenceRef(
		turn.SourceID,
		turn.Messages[0].MessageID,
		memory.MessageRoleUser,
		turn.Messages[0].Text,
		0,
		uint32(len(turn.Messages[0].Text)),
		memory.AssertionModeUser,
	)
	requireProjectionNoError(t, err)

	return evidence
}
func assertProjectionConversationHit(t *testing.T, projection *search.Store, scope memory.Scope,
	revision memory.MemoryRevision, conversation string, now time.Time, message string) {
	t.Helper()

	hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{
		Scope: scope, Query: "remembered", ConversationID: conversation, Limit: 1, MinScopeChangeSeq: 1, AsOf: &now,
	})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != revision.RevisionID {
		t.Fatalf(message, hits, err)
	}
}
