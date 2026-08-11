package bleve

import (
	"context"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

const stateTestRevisionOneID = "revision-1"

func TestBleveRecallProjectionIndexesRussianAndEnglishWithGenerationSwitch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projection, err := Open(root, Config{BatchSize: 1})
	if err != nil {
		t.Fatalf("NewBleveRecallProjection() error = %v", err)
	}
	defer func() { _ = projection.Close() }()
	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	first := recallProjectionDocument(scope, "item-1", stateTestRevisionOneID, "русское решение", now, 1)
	second := recallProjectionDocument(scope, "item-2", "revision-2", "english deployment decision", now.Add(time.Minute), 2)

	generation, err := projection.NewGeneration("generation-1")
	if err != nil {
		t.Fatalf("NewGeneration() error = %v", err)
	}
	if err := generation.Index(context.Background(), first); err != nil {
		t.Fatalf("Index(first) error = %v", err)
	}
	if err := generation.Index(context.Background(), second); err != nil {
		t.Fatalf("Index(second) error = %v", err)
	}
	if err := generation.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := generation.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := projection.ActivateGeneration(context.Background(), "generation-1"); err != nil {
		t.Fatalf("ActivateGeneration() error = %v", err)
	}

	category := boyan.AtomCategoryDecision
	russianHits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{
		Scope: scope, Query: "решение", Limit: 10, Category: &category,
	})
	if err != nil {
		t.Fatalf("Russian SearchRecall() error = %v", err)
	}
	if len(russianHits) != 1 || russianHits[0].RevisionID != stateTestRevisionOneID {
		t.Fatalf("Russian SearchRecall() = %#v", russianHits)
	}
	englishHits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "deployment", Limit: 10})
	if err != nil {
		t.Fatalf("English SearchRecall() error = %v", err)
	}
	if len(englishHits) != 1 || englishHits[0].RevisionID != "revision-2" {
		t.Fatalf("English SearchRecall() = %#v", englishHits)
	}

	generation, err = projection.NewGeneration("generation-2")
	if err != nil {
		t.Fatalf("NewGeneration(generation-2) error = %v", err)
	}
	third := recallProjectionDocument(scope, "item-3", "revision-3", "new canonical choice", now.Add(2*time.Minute), 3)
	if err := generation.Index(context.Background(), third); err != nil {
		t.Fatalf("Index(third) error = %v", err)
	}
	if err := generation.Commit(context.Background()); err != nil {
		t.Fatalf("Commit(generation-2) error = %v", err)
	}
	if err := generation.Close(); err != nil {
		t.Fatalf("Close(generation-2) error = %v", err)
	}
	if err := projection.ActivateGeneration(context.Background(), "generation-2"); err != nil {
		t.Fatalf("ActivateGeneration(generation-2) error = %v", err)
	}
	oldHits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "deployment", Limit: 10})
	if err != nil {
		t.Fatalf("SearchRecall(after switch) error = %v", err)
	}
	if len(oldHits) != 0 {
		t.Fatalf("SearchRecall(after switch) = %#v, want old generation hidden", oldHits)
	}
	newHits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "canonical", Limit: 10})
	if err != nil || len(newHits) != 1 || newHits[0].RevisionID != "revision-3" {
		t.Fatalf("SearchRecall(new generation) = %#v, error %v", newHits, err)
	}

	if err := projection.Close(); err != nil {
		t.Fatalf("Close(projection) error = %v", err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("reopen projection error = %v", err)
	}
	defer func() { _ = reopened.Close() }()
	reopenedHits, err := reopened.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "canonical", Limit: 10})
	if err != nil || len(reopenedHits) != 1 || reopenedHits[0].RevisionID != "revision-3" {
		t.Fatalf("SearchRecall(reopened) = %#v, error %v", reopenedHits, err)
	}
}

func TestBleveRecallProjectionScopedGenerationsSurviveIndependentActivation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projection, err := Open(root)
	if err != nil {
		t.Fatalf("NewBleveRecallProjection() error = %v", err)
	}
	defer func() { _ = projection.Close() }()
	now := time.Date(2026, time.August, 6, 11, 0, 0, 0, time.UTC)
	scopes := []boyan.Scope{
		{Key: "telegram:scope-a", Kind: boyan.ScopeKindPersonal},
		{Key: "telegram:scope-b", Kind: boyan.ScopeKindPersonal},
	}
	for index, scope := range scopes {
		generationID := "scoped-generation-" + string(rune('a'+index))
		generation, err := projection.NewGeneration(generationID)
		if err != nil {
			t.Fatalf("NewGeneration(%s) error = %v", generationID, err)
		}
		if err := generation.Index(context.Background(), recallProjectionDocument(scope, "item-"+string(rune('a'+index)), "scoped-revision-"+string(rune('a'+index)), "independent scope memory", now, 1)); err != nil {
			t.Fatalf("Index(%s) error = %v", generationID, err)
		}
		if err := generation.Commit(context.Background()); err != nil {
			t.Fatalf("Commit(%s) error = %v", generationID, err)
		}
		if err := generation.Close(); err != nil {
			t.Fatalf("Close(%s) error = %v", generationID, err)
		}
		if err := projection.ActivateGenerationForScope(context.Background(), scope, generationID); err != nil {
			t.Fatalf("ActivateGenerationForScope(%s) error = %v", generationID, err)
		}
	}
	for index, scope := range scopes {
		hits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "independent", Limit: 10})
		want := "scoped-revision-" + string(rune('a'+index))
		if err != nil || len(hits) != 1 || hits[0].RevisionID != want {
			t.Fatalf("SearchRecall(%s) = %#v, error %v; want %s", scope.Key, hits, err, want)
		}
	}
	if err := projection.Close(); err != nil {
		t.Fatalf("Close(projection) error = %v", err)
	}
	reopened, err := Open(root, DefaultConfig())
	if err != nil {
		t.Fatalf("reopen projection error = %v", err)
	}
	defer func() { _ = reopened.Close() }()
	for index, scope := range scopes {
		hits, err := reopened.SearchRecall(context.Background(), boyan.RecallRequest{Scope: scope, Query: "independent", Limit: 10})
		want := "scoped-revision-" + string(rune('a'+index))
		if err != nil || len(hits) != 1 || hits[0].RevisionID != want {
			t.Fatalf("SearchRecall(reopened %s) = %#v, error %v; want %s", scope.Key, hits, err, want)
		}
	}
}

func recallProjectionDocument(scope boyan.Scope, itemID, revisionID, text string, createdAt time.Time, changeSeq uint64) boyan.RecallProjectionDocument {
	category := boyan.AtomCategoryDecision
	return boyan.RecallProjectionDocument{
		Scope: scope, ItemID: itemID, RevisionID: revisionID, Revision: 1,
		Kind: boyan.MemoryKindState, Category: &category, MemoryKey: boyan.MemoryKey(itemID + "-key"),
		Text: text, CreatedAt: createdAt, Temporal: boyan.Temporal{ObservedAt: createdAt},
		Sensitivity: boyan.SensitivityStandard, Retention: boyan.RetentionClassStandard,
		SourceIDs: []string{"source-1"}, ConversationIDs: []string{"conversation-1"}, ScopeChangeSeq: changeSeq,
	}
}
