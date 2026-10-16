//nolint:testpackage // Reuse real lexical and vector-store fixtures.
package semanticprojection

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
)

func TestHybridReportsIndependentContributions(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)

	hits, err := hybrid.SearchRecall(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	if len(hits) != 2 || hits[0].Lexical != 1.0/61 || hits[0].Semantic != 1.0/61 ||
		hits[1].Lexical != 0 || hits[1].Semantic != 1.0/62 {
		t.Fatalf("channel contributions: %+v", hits)
	}
}

//nolint:cyclop,funlen // Publication, reopening and repeated comparisons form one restart contract.
func TestHybridReportDeterministicAfterReopening(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)
	ctx := context.Background()

	wantHits, wantReport, err := hybrid.SearchRecallWithReport(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	state, found, err := hybrid.store.(*semanticstore.Store).ActiveState(ctx, request.Scope)
	if err != nil || !found {
		t.Fatalf("state: %v %v", found, err)
	}

	snapshot, found, err := hybrid.store.Snapshot(ctx, state.View())
	if err != nil || !found {
		t.Fatalf("snapshot: %v %v", found, err)
	}

	dir := t.TempDir()

	store, err := semanticstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	chunks := snapshot.Chunks()

	coverage := make([]semanticstore.CoverageRecord, len(chunks))
	for i, chunk := range chunks {
		coverage[i] = semanticstore.CoverageRecord{ItemID: chunk.ItemID, RevisionID: chunk.RevisionID,
			Revision: chunk.Revision, ContentHash: chunk.ContentHash, ChunkCount: 1}
	}

	generation := semanticstore.Generation{Scope: state.Scope, BuildID: state.BuildID,
		LexicalGenerationID: state.LexicalGenerationID, LexicalWatermark: state.LexicalWatermark,
		ModelSpace: state.ModelSpace, Dimensions: state.Dimensions, ReadyAt: state.ReadyAt}
	if _, err := store.Stage(ctx, generation, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	if err := store.Publish(ctx, semanticstore.Publication{View: state.View(), BuildID: state.BuildID}); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := semanticstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	hybrid.store = reopened
	for range 3 {
		hits, report, err := hybrid.SearchRecallWithReport(ctx, request)
		if err != nil || !reflect.DeepEqual(hits, wantHits) || !reflect.DeepEqual(report, wantReport) {
			t.Fatalf("reopened deterministic report: %+v %+v %v", hits, report, err)
		}
	}
}

//nolint:cyclop // Both healthy and degraded report branches are verified against transport JSON.
func TestHybridReportSurvivesFallbackWithoutProviderProse(t *testing.T) {
	hybrid, _, _, request := hybridFixture(t)

	_, report, err := hybrid.SearchRecallWithReport(context.Background(), request)
	if err != nil || report == nil || report.Effective != "hybrid" || report.Validate() != nil {
		t.Fatalf("hybrid report: %+v %v", report, err)
	}

	body, err := json.Marshal(report)
	if err != nil || string(body) != `{"effective":"hybrid"}` {
		t.Fatalf("public report: %s %v", body, err)
	}

	hybrid.store = openSemanticStore(t)

	_, report, err = hybrid.SearchRecallWithReport(context.Background(), request)
	if err != nil || report == nil || report.Effective != "degraded" ||
		report.Reason != "unavailable" || report.Validate() != nil {
		t.Fatalf("fallback report: %+v %v", report, err)
	}
}
