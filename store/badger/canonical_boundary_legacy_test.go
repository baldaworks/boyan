package badger //nolint:testpackage // Injects malformed immutable records and exact encoded legacy keys.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func legacyBoundaryFixture(t *testing.T) (*CanonicalStore, memory.Boundary) {
	t.Helper()

	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	boundary, err := memory.NewBoundary(memory.Scope{Key: "legacy:bounded", Kind: memory.ScopeKindPersonal},
		memory.ConversationRef{ConversationID: "c", AgentConversationID: "a"}, "close", memory.BoundaryReasonClose,
		time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	return store, boundary
}

func putLegacyNamespaceKey(t *testing.T, store *CanonicalStore, scope memory.Scope, operation string) {
	t.Helper()

	key, err := canonicalKey(scope, badgerRecordOperation, operation)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, []byte("unreadable operation"))
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyClassificationConflict(t *testing.T, store *CanonicalStore, boundary memory.Boundary) {
	t.Helper()

	_, err := store.VerifyCanonicalBoundary(context.Background(), boundary)

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeConflict || class != memory.ErrorClassPermanent {
		t.Fatalf("legacy classification error=%v, want permanent conflict", err)
	}
}

func TestBoundaryLegacyExactEncodedNamespace(t *testing.T) {
	const prefix = "boyan-derived:v1:operation:"

	tests := []struct {
		name, operation   string
		neighbor, blocked bool
	}{
		{name: "hex", operation: prefix + strings.Repeat("a", 64), blocked: true},
		{name: "opaque fixed length", operation: prefix + "!" + strings.Repeat("0", 63), blocked: true},
		{name: "short", operation: prefix + strings.Repeat("a", 63)},
		{name: "long", operation: prefix + strings.Repeat("a", 65)},
		{name: "other namespace", operation: "boyan-derived:v2:operation:" + strings.Repeat("a", 64)},
		{name: "neighbor scope", operation: prefix + strings.Repeat("a", 64), neighbor: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, boundary := legacyBoundaryFixture(t)

			scope := boundary.Scope
			if test.neighbor {
				scope.Key += "x"
			}

			putLegacyNamespaceKey(t, store, scope, test.operation)

			if test.blocked {
				assertLegacyClassificationConflict(t, store, boundary)

				return
			}

			result, err := store.VerifyCanonicalBoundary(context.Background(), boundary)
			if err != nil || result.Certified {
				t.Fatalf("clean namespace=%+v %v", result, err)
			}
		})
	}
}

func TestBoundaryLegacyOpaqueFirstKeyCannotHideLaterHex(t *testing.T) {
	store, boundary := legacyBoundaryFixture(t)

	const prefix = "boyan-derived:v1:operation:"
	putLegacyNamespaceKey(t, store, boundary.Scope, prefix+"!"+strings.Repeat("0", 63))
	putLegacyNamespaceKey(t, store, boundary.Scope, prefix+strings.Repeat("f", 64))
	assertLegacyClassificationConflict(t, store, boundary)
}

func TestBoundaryLegacyRechecksBeforeBothWriteRoutes(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			store, boundary := legacyBoundaryFixture(t)
			if _, err := store.VerifyCanonicalBoundary(context.Background(), boundary); err != nil {
				t.Fatal(err)
			}

			putLegacyNamespaceKey(t, store, boundary.Scope, "boyan-derived:v1:operation:"+strings.Repeat("0", 64))

			identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
			if err != nil {
				t.Fatal(err)
			}

			_, err = commitBoundaryFaultFixture(store, identity, nonempty)

			code, class, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeConflict || class != memory.ErrorClassPermanent {
				t.Fatalf("write after stale proof=%v", err)
			}

			state, err := store.LoadScopeState(context.Background(), boundary.Scope)
			if err != nil || state.Version != 0 {
				t.Fatalf("rejected stage changed state=%+v %v", state, err)
			}
		})
	}
}

func TestBoundaryLegacyCapabilityValidation(t *testing.T) {
	tests := []struct {
		name   string
		marker any
	}{
		{name: "unsupported version", marker: map[string]any{"version": 2,
			"scope": memory.Scope{Key: "legacy:bounded", Kind: memory.ScopeKindPersonal}}},
		{name: "wrong scope", marker: map[string]any{"version": 1,
			"scope": memory.Scope{Key: "another", Kind: memory.ScopeKindPersonal}}},
		{name: "missing fields", marker: map[string]any{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, boundary := legacyBoundaryFixture(t)

			key, err := canonicalKey(boundary.Scope, "boundary-capability", "v1")
			if err != nil {
				t.Fatal(err)
			}

			if err := store.db.Update(func(txn *badger.Txn) error {
				return putCanonicalRecord(txn, key, "boundary-capability", test.marker)
			}); err != nil {
				t.Fatal(err)
			}

			_, err = store.VerifyCanonicalBoundary(context.Background(), boundary)

			code, _, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeStoreFailure {
				t.Fatalf("invalid capability=%v, want store failure", err)
			}
		})
	}
}

func TestBoundaryLegacyCapabilityIsAtomicAndCertifiedReplayIndependent(t *testing.T) {
	store, boundary := legacyBoundaryFixture(t)

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	store.beforeCanonicalMutationCommit = func() error { return errors.New("failed stage") }
	if _, err := commitBoundaryFaultFixture(store, identity, false); err == nil {
		t.Fatal("missing injected failure")
	}

	assertBoundaryCapabilityPresence(t, store, boundary.Scope, false)

	store.beforeCanonicalMutationCommit = nil
	if _, err := commitBoundaryFaultFixture(store, identity, false); err != nil {
		t.Fatal(err)
	}

	assertBoundaryCapabilityPresence(t, store, boundary.Scope, true)
	assertCertifiedBoundaryIgnoresMarker(t, store, identity)
}

func assertCertifiedBoundaryIgnoresMarker(
	t *testing.T, store *CanonicalStore, identity memory.CanonicalBoundaryIdentityCommit,
) {
	t.Helper()

	boundary := identity.Boundary
	// A valid capability permits a different new boundary despite later derived
	// operations. It never changes or substitutes an existing input certificate.
	putLegacyNamespaceKey(t, store, boundary.Scope, "boyan-derived:v1:operation:"+strings.Repeat("f", 64))

	next, err := memory.NewBoundary(boundary.Scope, boundary.Conversation, "next", boundary.Reason, boundary.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.VerifyCanonicalBoundary(context.Background(), next)
	if err != nil || !result.SafelyNew {
		t.Fatalf("marked scope=%+v %v", result, err)
	}

	key, err := canonicalKey(boundary.Scope, "boundary-capability", "v1")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Set(key, []byte("corrupt marker")) }); err != nil {
		t.Fatal(err)
	}

	result, err = store.VerifyCanonicalBoundary(context.Background(), boundary)
	if err != nil || !result.Certified {
		t.Fatalf("certified input depends on marker=%+v %v", result, err)
	}

	if _, err := commitBoundaryFaultFixture(store, identity, false); err != nil {
		t.Fatalf("certified replay=%v", err)
	}

	_, err = store.CommitCanonicalOperation(context.Background(), memory.CanonicalOperationCommitRequest{
		Scope: boundary.Scope, OperationID: "unfinished-stage", Fingerprint: "unfinished-stage", ExpectedScopeVersion: 1,
		CommittedAt: boundary.OccurredAt, BoundaryIdentity: &identity,
	})
	if err != nil {
		t.Fatalf("certified unfinished stage depends on marker: %v", err)
	}
}

func assertBoundaryCapabilityPresence(t *testing.T, store *CanonicalStore, scope memory.Scope, want bool) {
	t.Helper()

	err := store.db.View(func(txn *badger.Txn) error {
		found, err := loadBoundaryCapability(txn, scope)
		if err != nil {
			return err
		}

		if found != want {
			t.Errorf("capability present=%v, want %v", found, want)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryLegacyMarkedScopeRejectsUnprovenDerivedWrites(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			store, boundary := legacyBoundaryFixture(t)

			identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := commitBoundaryFaultFixture(store, identity, false); err != nil {
				t.Fatal(err)
			}

			operation := "boyan-derived:v1:operation:" + strings.Repeat("d", 64)
			if nonempty {
				mutation := canonicalRevisionMutation(
					boundary.Scope, 1, operation, canonicalRevision("unproven-revision", "unproven-item"),
				)
				_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
			} else {
				_, err = store.CommitCanonicalOperation(context.Background(), memory.CanonicalOperationCommitRequest{
					Scope: boundary.Scope, OperationID: operation, Fingerprint: "unproven", ExpectedScopeVersion: 1,
					CommittedAt: boundary.OccurredAt})
			}

			code, class, ok := memory.ClassifyError(err)
			if !ok || code != memory.CodeConflict || class != memory.ErrorClassPermanent {
				t.Fatalf("marked unproven write=%v", err)
			}

			state, err := store.LoadScopeState(context.Background(), boundary.Scope)
			if err != nil || state.Version != 1 {
				t.Fatalf("unproven write advanced scope=%+v %v", state, err)
			}
		})
	}
}

func TestBoundaryLegacyConcurrentCapabilityInitialization(t *testing.T) {
	store, boundary := legacyBoundaryFixture(t)

	identities := capabilityRaceIdentities(t, boundary)

	start := make(chan struct{})

	type result struct {
		index int
		err   error
	}

	results := make(chan result, 2)

	for index, identity := range identities {
		go func() {
			<-start

			_, err := commitCapabilityInitialization(store, identity, 0)
			results <- result{index: index, err: err}
		}()
	}

	close(start)

	loser := -1
	winners := 0

	for range identities {
		result := <-results
		if result.err == nil {
			winners++

			continue
		}

		code, _, ok := memory.ClassifyError(result.err)
		if !ok || code != memory.CodeConflict {
			t.Fatalf("initialization race=%v", result.err)
		}

		loser = result.index
	}

	if winners != 1 || loser < 0 {
		t.Fatalf("initialization winners=%d loser=%d", winners, loser)
	}

	assertBoundaryCapabilityPresence(t, store, boundary.Scope, true)

	if _, err := commitCapabilityInitialization(store, identities[loser], 1); err != nil {
		t.Fatalf("retry distinct input=%v", err)
	}

	assertCapabilityRaceCertified(t, store, identities)
}

func commitCapabilityInitialization(store *CanonicalStore, identity memory.CanonicalBoundaryIdentityCommit,
	version uint64) (memory.CanonicalMutationOutcome, error) {
	return store.CommitCanonicalOperation(context.Background(), memory.CanonicalOperationCommitRequest{
		Scope: identity.Boundary.Scope, OperationID: identity.Certificate.ExportID,
		Fingerprint:          identity.Certificate.Fingerprint,
		ExpectedScopeVersion: version, CommittedAt: identity.Boundary.OccurredAt, BoundaryIdentity: &identity})
}

func capabilityRaceIdentities(t *testing.T, boundary memory.Boundary) []memory.CanonicalBoundaryIdentityCommit {
	t.Helper()

	next, err := memory.NewBoundary(boundary.Scope, boundary.Conversation, "next", boundary.Reason, boundary.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}

	identities := make([]memory.CanonicalBoundaryIdentityCommit, 2)
	for index, input := range []memory.Boundary{boundary, next} {
		identities[index], err = memory.NewCanonicalBoundaryIdentityCommit(input)
		if err != nil {
			t.Fatal(err)
		}
	}

	return identities
}

func TestBoundaryLegacyFirstMarkerRacesUnprovenWriter(t *testing.T) {
	for _, boundaryFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy wins", true: "boundary wins"}[boundaryFirst], func(t *testing.T) {
			store, boundary := legacyBoundaryFixture(t)

			identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
			if err != nil {
				t.Fatal(err)
			}

			certified := memory.CanonicalOperationCommitRequest{Scope: boundary.Scope, OperationID: "certified",
				Fingerprint: "certified", CommittedAt: boundary.OccurredAt, BoundaryIdentity: &identity}
			unproven := memory.CanonicalOperationCommitRequest{Scope: boundary.Scope,
				OperationID: "boyan-derived:v1:operation:" + strings.Repeat("0", 64), Fingerprint: "unproven",
				CommittedAt: boundary.OccurredAt}

			staged, winner := certified, unproven
			if boundaryFirst {
				staged, winner = unproven, certified
			}

			txn := store.db.NewTransaction(true)
			defer txn.Discard()

			stageCapabilityRaceTransaction(t, store, txn, staged)

			if _, err := store.CommitCanonicalOperation(context.Background(), winner); err != nil {
				t.Fatal(err)
			}

			if err := txn.Commit(); !errors.Is(err, badger.ErrConflict) {
				t.Fatalf("stale initialization committed: %v", err)
			}

			assertBoundaryCapabilityPresence(t, store, boundary.Scope, boundaryFirst)

			if !boundaryFirst {
				assertLegacyClassificationConflict(t, store, boundary)
			}
		})
	}
}

func stageCapabilityRaceTransaction(t *testing.T, store *CanonicalStore, txn *badger.Txn,
	request memory.CanonicalOperationCommitRequest) {
	t.Helper()

	scopeKey, err := badgerScopeKey(request.Scope)
	if err != nil {
		t.Fatal(err)
	}

	operationKey, err := badgerOperationKey(request.Scope, request.OperationID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.commitCanonicalOperationTxn(txn, request, scopeKey, operationKey); err != nil {
		t.Fatal(err)
	}
}

func assertCapabilityRaceCertified(
	t *testing.T, store *CanonicalStore, identities []memory.CanonicalBoundaryIdentityCommit,
) {
	t.Helper()

	for _, identity := range identities {
		result, err := store.VerifyCanonicalBoundary(context.Background(), identity.Boundary)
		if err != nil || !result.Certified {
			t.Fatalf("initialized certificate=%+v %v", result, err)
		}
	}
}
