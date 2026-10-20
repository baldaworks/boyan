//go:build integration

package sqlite

import (
	"context"
	"math"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

type scopedPublicationCheckpoints struct {
	*publicationCheckpoints

	peer      memory.ProjectionManifest
	peerLoads int
}

func (c *scopedPublicationCheckpoints) LoadActiveProjectionManifest(ctx context.Context,
	scope memory.Scope, projection string) (memory.ProjectionManifest, bool, error) {
	if scope == c.peer.Scope {
		c.peerLoads++

		return c.peer, true, nil
	}

	return c.publicationCheckpoints.LoadActiveProjectionManifest(ctx, scope, projection)
}

func (c *scopedPublicationCheckpoints) WalkProjectionManifests(ctx context.Context, projection string,
	visit func(memory.ProjectionManifest, bool) error) error {
	if err := c.publicationCheckpoints.WalkProjectionManifests(ctx, projection, visit); err != nil {
		return err
	}

	return visit(c.peer, true)
}

func TestActivationRetiresOnlyItsScopeAndGlobalRepairStillRecoversPeers(t *testing.T) {
	store, _ := openTestStore(t, false)
	peerScope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: lexicalTestScope.Key}
	seedLexical(t, store, "local-obsolete", lexicalTestScope, []lexicalFixture{{id: "old", raw: "needle"}})
	seedLexical(t, store, "local", lexicalTestScope, nil)
	seedLexical(t, store, "peer-obsolete", peerScope, []lexicalFixture{{id: "peer-old", raw: "needle"}})
	seedLexical(t, store, "peer", peerScope, nil)
	// Canonical-first crash window in a different scope, repaired at startup.
	execTest(t, store.writer, `DELETE FROM lexical_active WHERE scope_kind=? AND scope_key=?`,
		peerScope.Kind, peerScope.Key)

	c := &scopedPublicationCheckpoints{
		publicationCheckpoints: &publicationCheckpoints{active: publicationManifest("local", math.MaxUint64)},
		peer: publicationManifest("peer", math.MaxUint64),
	}
	c.peer.Scope = peerScope

	a, err := NewSQLiteCanonicalApplier(store, &applyReader{}, fixtureCanonicalPrivacy(t, store, nil))
	if err != nil {
		t.Fatal(err)
	}

	a.ConfigureProjectionRetention(c, "sqlite")

	if err := a.Activate(t.Context(), lexicalTestScope, "local"); err != nil {
		t.Fatal(err)
	}

	if c.peerLoads != 0 {
		t.Errorf("local activation loaded peer authority %d times", c.peerLoads)
	}

	assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_build WHERE physical_build_id='local-obsolete'`, 0)
	assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_build WHERE physical_build_id='peer-obsolete'`, 1)
	assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_active WHERE scope_kind=? AND scope_key=?`,
		0, peerScope.Kind, peerScope.Key)

	if err := a.ReconcileProjection(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_build WHERE physical_build_id='peer-obsolete'`, 0)
	assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_active WHERE physical_build_id='peer'`, 1)
}
