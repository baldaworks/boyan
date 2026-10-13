package canonicalcore_test

import (
	"context"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryIdentityIsPartitionedByExactScopeKind(t *testing.T) {
	t.Parallel()
	f := newBoundaryReplayFixture(t, false)
	ctx := context.Background()
	personal := f.export
	group := personal
	group.Scope.Kind = m.ScopeKindGroup
	group.Reason = m.BoundaryReasonReset
	requireNoError(t, group.Validate(), "same export in distinct exact scope: %v")

	for _, export := range []m.Boundary{personal, group} {
		identity, err := m.NewCanonicalBoundaryIdentityCommit(export)
		requireNoError(t, err, "identity: %v")
		state, err := f.store.LoadScopeState(ctx, export.Scope)
		requireNoError(t, err, "scope state: %v")
		_, err = f.store.CommitCanonicalOperation(ctx, m.CanonicalOperationCommitRequest{
			Scope: export.Scope, ExpectedScopeVersion: state.Version, OperationID: "scope-kind-operation",
			Fingerprint: "scope-kind-fingerprint", CommittedAt: f.at, BoundaryIdentity: &identity})
		requireNoError(t, err, "independent scoped certificate: %v")
	}

	for _, export := range []m.Boundary{personal, group} {
		result, err := f.store.VerifyCanonicalBoundary(ctx, export)
		requireNoError(t, err, "exact scope proof: %v")

		if !result.Certified {
			t.Fatal("scope lacks its independent proof")
		}

		if export.Reason == m.BoundaryReasonClose {
			export.Reason = m.BoundaryReasonReset
		} else {
			export.Reason = m.BoundaryReasonClose
		}

		_, err = f.store.VerifyCanonicalBoundary(ctx, export)
		requirePermanentIdentityConflict(t, err)
	}
}
