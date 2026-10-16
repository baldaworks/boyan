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
	verifyCanonicalLargeSQLite(t, reader, scope, mutation.Revisions[0], conversation, now)
}

func verifyCanonicalLargeSQLite(t *testing.T, reader memory.RecallCanonicalReader, scope memory.Scope,
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

	applier, err := search.NewSQLiteCanonicalApplier(projection, reader)
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.BeginProjectionRebuild(t.Context(), scope, "canonical-large"); err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), scope, "canonical-large", nil, []memory.MemoryRevision{revision}); err != nil {
		t.Fatal(err)
	}
	// TASK-007 owns real publication. This controlled fixture makes the TASK-006
	// candidate observable through the existing real FTS/filter query.
	db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err = db.Exec(`UPDATE lexical_build SET state='committed'`); err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(`INSERT INTO lexical_active SELECT scope_kind,scope_key,physical_build_id FROM lexical_build`); err != nil {
		t.Fatal(err)
	}

	hits, err := projection.SearchRecall(t.Context(), memory.RecallRequest{Scope: scope, Query: "remembered", ConversationID: conversation, Limit: 1, AsOf: &now})
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

	applier, err = search.NewSQLiteCanonicalApplier(projection, reader)
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), scope, "canonical-large", nil, nil); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = db.QueryRow(`SELECT count(*) FROM lexical_conversation WHERE id=?`, conversation).Scan(&count); err != nil || count != 2 {
		t.Fatalf("incremental exact copy: %d %v", count, err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), scope, nil, []string{revision.RevisionID}); err != nil {
		t.Fatal(err)
	}

	if err = db.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("logical forget: %d %v", count, err)
	}
}
