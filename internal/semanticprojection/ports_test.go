//nolint:testpackage // Exercise consumer ports with the existing real legacy fixtures.
package semanticprojection

import (
	"context"
	"testing"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
)

var _ CandidateLifecycle = (*sqlitestore.Store)(nil)

func TestPortableBuildStorePreservesTypedNilRejection(t *testing.T) {
	t.Parallel()

	var store *semanticstore.Store

	canonical := &fakeCanonical{}
	manifests := &fakeManifestSource{}

	manager, err := New(Dependencies{Canonical: canonical, Records: canonical,
		Manifests: manifests, Physical: manifests, Provider: &fakeProvider{}, Store: store,
		ModelSpace: testModelSpace("model")})
	if err == nil {
		_ = manager.Close(context.Background())

		t.Fatal("New accepted typed nil legacy store through BuildStore")
	}
}

func TestLegacySearchPortPreservesTypedNilRejection(t *testing.T) {
	hybrid, provider, _, _ := hybridFixture(t)

	var store *semanticstore.Store
	if _, err := NewHybrid(hybrid.lexical, store, provider, hybrid.space); err == nil {
		t.Fatal("NewHybrid accepted typed nil legacy store through LegacySearchReader")
	}
}
