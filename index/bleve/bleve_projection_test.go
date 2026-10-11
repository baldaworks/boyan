package bleve_test

import bleve "github.com/baldaworks/boyan/index/bleve"

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

	projection, err := bleve.Open(root, bleve.Config{BatchSize: 1})
	requireNoError(t, err, "NewBleveRecallProjection() error = %v")

	defer func() { _ = projection.Close() }()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	first := recallProjectionDocument(scope, "item-1", stateTestRevisionOneID, "русское решение", now, 1)
	second := recallProjectionDocument(
		scope,
		"item-2",
		"revision-2",
		"english deployment decision",
		now.Add(time.Minute),
		2,
	)

	generation, err := projection.NewGeneration("generation-1")
	requireNoError(t, err, "NewGeneration() error = %v")

	requireNoError(t, generation.Index(context.Background(), first), "Index(first) error = %v")

	requireNoError(t, generation.Index(context.Background(), second), "Index(second) error = %v")

	requireNoError(t, generation.Commit(context.Background()), "Commit() error = %v")

	requireNoError(t, generation.Close(), "Close() error = %v")

	requireNoError(
		t,
		projection.ActivateGeneration(
			context.Background(),
			"generation-1",
		),
		"ActivateGeneration() error = %v",
	)

	assertBilingualProjectionHits(t, projection, scope)

	assertProjectionGenerationSwitch(t, projection, root, scope, now)
}

func TestBleveRecallProjectionScopedGenerationsSurviveIndependentActivation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	projection, err := bleve.Open(root)
	requireNoError(t, err, "NewBleveRecallProjection() error = %v")

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

		if err := generation.Index(
			context.Background(),
			recallProjectionDocument(
				scope,
				"item-"+string(rune('a'+index)),
				"scoped-revision-"+string(rune('a'+index)),
				"independent scope memory",
				now,
				1,
			),
		); err != nil {
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

	assertIndependentScopedProjectionHits(t, projection, scopes, "SearchRecall(%s) = %#v, error %v; want %s")

	requireNoError(t, projection.Close(), "Close(projection) error = %v")

	reopened, err := bleve.Open(root, bleve.DefaultConfig())
	requireNoError(t, err, "reopen projection error = %v")

	defer func() { _ = reopened.Close() }()

	assertIndependentScopedProjectionHits(t, reopened, scopes, "SearchRecall(reopened %s) = %#v, error %v; want %s")
}

func recallProjectionDocument(
	scope boyan.Scope,
	itemID, revisionID, text string,
	createdAt time.Time,
	changeSeq uint64,
) boyan.RecallProjectionDocument {
	category := boyan.AtomCategoryDecision

	return boyan.RecallProjectionDocument{
		Scope: scope, ItemID: itemID, RevisionID: revisionID, Revision: 1,
		Kind: boyan.MemoryKindState, Category: &category, MemoryKey: boyan.MemoryKey(itemID + "-key"),
		Text: text, CreatedAt: createdAt, Temporal: boyan.Temporal{ObservedAt: createdAt},
		Sensitivity: boyan.SensitivityStandard, Retention: boyan.RetentionClassStandard,
		SourceIDs: []string{"source-1"}, ConversationIDs: []string{"conversation-1"}, ScopeChangeSeq: changeSeq,
	}
}

func assertProjectionGenerationSwitch(
	t *testing.T,
	projection *bleve.BleveRecallProjection,
	root string,
	scope boyan.Scope,
	now time.Time,
) {
	t.Helper()

	generation, err := projection.NewGeneration("generation-2")
	requireNoError(t, err, "NewGeneration(generation-2) error = %v")

	third := recallProjectionDocument(scope, "item-3", "revision-3", "new canonical choice", now.Add(2*time.Minute), 3)
	requireNoError(t, generation.Index(context.Background(), third), "Index(third) error = %v")

	requireNoError(t, generation.Commit(context.Background()), "Commit(generation-2) error = %v")

	requireNoError(t, generation.Close(), "Close(generation-2) error = %v")

	requireNoError(
		t,
		projection.ActivateGeneration(
			context.Background(),
			"generation-2",
		),
		"ActivateGeneration(generation-2) error = %v",
	)

	oldHits, err := projection.SearchRecall(
		context.Background(),
		boyan.RecallRequest{Scope: scope, Query: "deployment", Limit: 10},
	)
	requireNoError(t, err, "SearchRecall(after switch) error = %v")

	if len(oldHits) != 0 {
		t.Fatalf("SearchRecall(after switch) = %#v, want old generation hidden", oldHits)
	}

	newHits, err := projection.SearchRecall(
		context.Background(),
		boyan.RecallRequest{Scope: scope, Query: "canonical", Limit: 10},
	)
	if err != nil || len(newHits) != 1 || newHits[0].RevisionID != "revision-3" {
		t.Fatalf("SearchRecall(new generation) = %#v, error %v", newHits, err)
	}

	requireNoError(t, projection.Close(), "Close(projection) error = %v")

	reopened, err := bleve.Open(root)
	requireNoError(t, err, "reopen projection error = %v")

	defer func() { _ = reopened.Close() }()

	reopenedHits, err := reopened.SearchRecall(
		context.Background(),
		boyan.RecallRequest{Scope: scope, Query: "canonical", Limit: 10},
	)
	if err != nil || len(reopenedHits) != 1 || reopenedHits[0].RevisionID != "revision-3" {
		t.Fatalf("SearchRecall(reopened) = %#v, error %v", reopenedHits, err)
	}
}

func assertIndependentScopedProjectionHits(
	t *testing.T,
	projection *bleve.BleveRecallProjection,
	scopes []boyan.Scope,
	message string,
) {
	t.Helper()

	for index, scope := range scopes {
		hits, err := projection.SearchRecall(
			context.Background(),
			boyan.RecallRequest{Scope: scope, Query: "independent", Limit: 10},
		)

		want := "scoped-revision-" + string(rune('a'+index))
		if err != nil || len(hits) != 1 || hits[0].RevisionID != want {
			t.Fatalf(message, scope.Key, hits, err, want)
		}
	}
}

func assertBilingualProjectionHits(t *testing.T, projection *bleve.BleveRecallProjection, scope boyan.Scope) {
	t.Helper()

	category := boyan.AtomCategoryDecision

	russianHits, err := projection.SearchRecall(context.Background(), boyan.RecallRequest{
		Scope: scope, Query: "решение", Limit: 10, Category: &category,
	})
	requireNoError(t, err, "Russian SearchRecall() error = %v")

	if len(russianHits) != 1 || russianHits[0].RevisionID != stateTestRevisionOneID {
		t.Fatalf("Russian SearchRecall() = %#v", russianHits)
	}

	englishHits, err := projection.SearchRecall(
		context.Background(),
		boyan.RecallRequest{Scope: scope, Query: "deployment", Limit: 10},
	)
	requireNoError(t, err, "English SearchRecall() error = %v")

	if len(englishHits) != 1 || englishHits[0].RevisionID != "revision-2" {
		t.Fatalf("English SearchRecall() = %#v", englishHits)
	}
}
