//nolint:testpackage,cyclop,funlen,lll // Reuse actual canonical mutation fixtures to establish the accepted identity domain.
package badger

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	search "github.com/baldaworks/boyan/index/sqlite"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalAcceptsConversationBeyondSQLitePageBudget(t *testing.T) {
	verifyCanonicalConversationIdentity(t, strings.Repeat("c", 2*1024*1024+1))
}

func TestCanonicalLeadingNULConversationRetainsExactSQLiteMembership(t *testing.T) {
	verifyCanonicalConversationIdentity(t, "\x00cccccccc")
	verifyCanonicalConversationIdentity(t, "\x00")
}

func verifyCanonicalConversationIdentity(t *testing.T, conversation string) {
	t.Helper()

	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = store.Close() }()

	scope := memory.Scope{Key: "canonical:large-identity", Kind: memory.ScopeKindPersonal}
	now := time.Now().UTC()

	turn, err := memory.NewTurn(scope, memory.ConversationRef{ConversationID: conversation, AgentConversationID: "agent"}, "turn", now, "remember", "remembered")
	if err != nil {
		t.Fatal(err)
	}

	evidence, err := memory.NewEvidenceRef(turn.SourceID, turn.Messages[0].MessageID, memory.MessageRoleUser, turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	mutation := canonicalReaderTestMutation(scope, now, evidence, turn)
	source := canonicalReaderTestSourcePayload(t, turn)
	mutation.Sources[0].Payload = payloadRef("source-large", source)
	mutation.Messages[0].Payload = payloadRef("message-large", []byte(turn.Messages[0].Text))
	mutation.Messages[1].Payload = payloadRef("message-large-assistant", []byte(turn.Messages[1].Text))
	mutation.Revisions[0].Payload = payloadRef("revision-large", []byte("remembered"))

	mutation.Payloads = []memory.CanonicalPayload{
		{Ref: mutation.Sources[0].Payload, Data: source},
		{Ref: mutation.Messages[0].Payload, Data: []byte(turn.Messages[0].Text)},
		{Ref: mutation.Messages[1].Payload, Data: []byte(turn.Messages[1].Text)},
		{Ref: mutation.Revisions[0].Payload, Data: []byte("remembered")},
	}
	if _, err = store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

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

func verifyCanonicalLargeSQLite(t *testing.T, reader memory.RecallCanonicalReader, checkpoints memory.ProjectionCheckpointStore, canonical *CanonicalStore, scope memory.Scope,
	revision memory.MemoryRevision, conversation string, now time.Time) {
	t.Helper()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	projection, err := search.Open(t.Context(), dir, search.Options{})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = projection.Close() }()

	applier, err := search.NewSQLiteCanonicalApplier(projection, reader, canonical)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	if err = applier.BeginProjectionRebuild(t.Context(), scope, "canonical-large"); err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), scope, "canonical-large", nil, []memory.MemoryRevision{revision}); err != nil {
		t.Fatal(err)
	}

	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-large", 1, now)
	// A read-only connection inspects logical cleanup; publication uses only the
	// real applier and canonical checkpoint APIs.
	db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	hits, err := projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: "remembered", ConversationID: conversation, Limit: 1, MinScopeChangeSeq: 1, AsOf: &now})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != revision.RevisionID {
		t.Fatalf("real canonical->SQL filter: %+v %v", hits, err)
	}

	if err = projection.Close(); err != nil {
		t.Fatal(err)
	}

	projection, err = search.Open(t.Context(), dir, search.Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = projection.Close() })

	applier, err = search.NewSQLiteCanonicalApplier(projection, reader, canonical)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(checkpoints, "sqlite")

	if resumed, err := applier.ResumeProjection(t.Context(), scope, "canonical-large"); err != nil || !resumed {
		t.Fatalf("real canonical reopen resume: %v %v", resumed, err)
	}

	if err = applier.Apply(t.Context(), scope, "canonical-large", nil, nil); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = db.QueryRow(`SELECT count(*) FROM lexical_conversation WHERE id=?`, conversation).Scan(&count); err != nil || count != 2 {
		t.Fatalf("incremental exact copy: %d %v", count, err)
	}

	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-large", 1, now)

	if err := applier.BeginProjectionRebuild(t.Context(), scope, "canonical-rebuilt"); err != nil {
		t.Fatal(err)
	}

	if err := applier.Apply(t.Context(), scope, "canonical-rebuilt", nil, []memory.MemoryRevision{revision}); err != nil {
		t.Fatal(err)
	}

	publishCanonicalLargeSQLite(t, applier, checkpoints, scope, "canonical-rebuilt", 1, now.Add(time.Second))

	hits, err = projection.SearchRecallConsistent(t.Context(), memory.RecallRequest{Scope: scope, Query: "remembered", ConversationID: conversation, Limit: 1, MinScopeChangeSeq: 1, AsOf: &now})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != revision.RevisionID {
		t.Fatalf("oversized rebuild publication: %+v %v", hits, err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), scope, nil, []string{revision.RevisionID}); err != nil {
		t.Fatal(err)
	}

	if err = db.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("logical forget: %d %v", count, err)
	}
}

func publishCanonicalLargeSQLite(t *testing.T, applier *search.SQLiteCanonicalApplier,
	checkpoints memory.ProjectionCheckpointStore, scope memory.Scope, generation string, watermark uint64, now time.Time) {
	t.Helper()

	manifest := memory.ProjectionManifest{Scope: scope, ProjectionID: "sqlite", GenerationID: generation,
		Status: memory.ProjectionGenerationDirty, Watermark: watermark, UpdatedAt: now}
	if err := checkpoints.MarkProjectionDirty(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := applier.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := checkpoints.AdvanceProjectionWatermark(t.Context(), manifest, watermark, now); err != nil {
		t.Fatal(err)
	}

	if err := checkpoints.ActivateProjectionGeneration(t.Context(), manifest, now); err != nil {
		t.Fatal(err)
	}

	if err := applier.Activate(t.Context(), scope, generation); err != nil {
		t.Fatal(err)
	}
}
