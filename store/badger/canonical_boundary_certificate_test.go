package badger_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type boundaryCertificateVerifier interface {
	VerifyCanonicalBoundary(ctx context.Context, boundary m.Boundary) (m.BoundaryIdentityResult, error)
}

func certificateBoundary(t *testing.T) m.Boundary {
	t.Helper()

	export, err := m.NewBoundary(m.Scope{Key: "test:atomic-boundary", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}, "close",
		m.BoundaryReasonClose, time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	return export
}

func certificateIdentity(t *testing.T, export m.Boundary) *m.CanonicalBoundaryIdentityCommit {
	t.Helper()

	identity, err := m.NewCanonicalBoundaryIdentityCommit(export)
	if err != nil {
		t.Fatal(err)
	}

	return &identity
}

func certificateWrite(store *b.CanonicalStore, scope m.Scope, identity *m.CanonicalBoundaryIdentityCommit,
	nonempty bool) (m.CanonicalMutationOutcome, error) {
	if nonempty {
		mutation := canonicalRevisionMutation(
			scope, 0, "boundary-stage", canonicalRevision("revision-boundary", "item-boundary"),
		)
		mutation.BoundaryIdentity = identity

		return store.ApplyCanonicalMutation(context.Background(), mutation)
	}

	return store.CommitCanonicalOperation(context.Background(), m.CanonicalOperationCommitRequest{
		Scope: scope, OperationID: "boundary-stage", Fingerprint: "stage-fingerprint",
		CommittedAt: time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC), BoundaryIdentity: identity})
}

func certificateVerifier(t *testing.T, store *b.CanonicalStore) boundaryCertificateVerifier {
	t.Helper()

	verifier, ok := any(store).(boundaryCertificateVerifier)
	if !ok {
		t.Fatal("canonical store does not implement boundary certificate verification")
	}

	return verifier
}

func assertPermanentBoundaryConflict(t *testing.T, err error) {
	t.Helper()

	code, class, ok := m.ClassifyError(err)
	if !ok || code != m.CodeConflict || class != m.ErrorClassPermanent {
		t.Errorf("error=%v, want permanent boundary conflict", err)
	}
}

func TestBoundaryCertificateProtectsBothWriteRouteReplays(t *testing.T) {
	t.Parallel()

	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			store := openCanonicalTestStore(t, t.TempDir())
			t.Cleanup(func() { closeCanonicalTestStore(t, store) })
			original := certificateBoundary(t)
			identity := certificateIdentity(t, original)

			outcome, err := certificateWrite(store, original.Scope, identity, nonempty)
			if err != nil {
				t.Fatal(err)
			}

			replay, err := certificateWrite(store, original.Scope, identity, nonempty)
			if err != nil || !reflect.DeepEqual(replay, outcome) {
				t.Fatalf("matching replay=%+v error=%v", replay, err)
			}

			changed := original
			changed.Reason = m.BoundaryReasonReset
			_, err = certificateWrite(store, original.Scope, certificateIdentity(t, changed), nonempty)
			assertPermanentBoundaryConflict(t, err)
			verifier := certificateVerifier(t, store)

			result, err := verifier.VerifyCanonicalBoundary(context.Background(), original)
			if err != nil || !result.Certified {
				t.Fatalf("successful stage is not certified: %+v %v", result, err)
			}

			_, err = verifier.VerifyCanonicalBoundary(context.Background(), changed)
			assertPermanentBoundaryConflict(t, err)

			state, err := store.LoadScopeState(context.Background(), original.Scope)
			if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
				t.Errorf("conflicting retry changed state: %+v %v", state, err)
			}
		})
	}
}

func TestBoundaryCertificateCannotBeInventedFromLegacyStageReplay(t *testing.T) {
	t.Parallel()

	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			store := openCanonicalTestStore(t, t.TempDir())
			t.Cleanup(func() { closeCanonicalTestStore(t, store) })
			original := certificateBoundary(t)

			_, err := certificateWrite(store, original.Scope, nil, nonempty)
			if err != nil {
				t.Fatal(err)
			}

			_, err = certificateWrite(store, original.Scope, certificateIdentity(t, original), nonempty)
			assertPermanentBoundaryConflict(t, err)

			result, err := certificateVerifier(t, store).VerifyCanonicalBoundary(context.Background(), original)
			if err != nil || result.Certified {
				t.Errorf("legacy replay acquired invented proof: %+v %v", result, err)
			}
		})
	}
}

func TestBoundaryCertificateCompetingInputsHaveOneWinner(t *testing.T) {
	t.Parallel()

	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			store := openCanonicalTestStore(t, t.TempDir())
			t.Cleanup(func() { closeCanonicalTestStore(t, store) })
			original := certificateBoundary(t)
			changed := original
			changed.OccurredAt = changed.OccurredAt.Add(time.Hour)
			identities := []*m.CanonicalBoundaryIdentityCommit{certificateIdentity(t, original), certificateIdentity(t, changed)}
			start := make(chan struct{})
			results := make(chan error, 2)

			for _, identity := range identities {
				go func() {
					<-start

					_, err := certificateWrite(store, original.Scope, identity, nonempty)
					results <- err
				}()
			}

			close(start)

			winners := 0

			for range identities {
				err := <-results
				if err == nil {
					winners++
				} else {
					assertPermanentBoundaryConflict(t, err)
				}
			}

			if winners != 1 {
				t.Errorf("competing inputs committed %d winners", winners)
			}

			state, err := store.LoadScopeState(context.Background(), original.Scope)
			if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
				t.Errorf("competing inputs state=%+v error=%v", state, err)
			}
		})
	}
}

func TestBoundaryCertificateWriteChecksEraseFenceBeforeInputConflict(t *testing.T) {
	t.Parallel()
	store := openCanonicalTestStore(t, t.TempDir())
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })
	original := certificateBoundary(t)

	_, err := certificateWrite(store, original.Scope, certificateIdentity(t, original), false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.BeginScopeErase(context.Background(), m.ScopeEraseRequest{Scope: original.Scope,
		OperationID: "erase", Fingerprint: "erase-fingerprint", ExpectedScopeVersion: 1,
		ForgottenAt: original.OccurredAt.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	changed := original

	changed.Reason = m.BoundaryReasonReset
	for _, nonempty := range []bool{false, true} {
		_, err = certificateWrite(store, original.Scope, certificateIdentity(t, changed), nonempty)
		if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeConflict || !m.IsRetryable(err) {
			t.Errorf("certificate conflict bypassed pending erase fence: %v", err)
		}
	}
}
