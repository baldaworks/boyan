//go:build integration

package storefixture_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	derive "github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// The former collector fixture supplied these records from a scanner fake.
// Here every head, hydrated payload and layer comes from the actual SQL owner.
func TestPublicBoundaryMaximumModelLayersAndSerializedBudget(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
		base := nativeMutation(t)
		_, err := store.ApplyCanonicalMutation(t.Context(), base)
		requireProjectionNoError(t, err)
		seedMaximumBoundaryModel(t, p, base)

		reader := publicTraceReader(t, f.Store)
		view, err := reader.LoadCanonicalBoundaryViewAt(t.Context(), base.Scope, base.Operation.CommittedAt)
		requireProjectionNoError(t, err)

		if len(view.Snapshot.Atoms) != 2046 || len(view.Snapshot.Scenarios) != 1 || len(view.Snapshot.Profiles) != 1 {
			t.Fatalf("maximum model layers: atoms=%d scenarios=%d profiles=%d",
				len(view.Snapshot.Atoms), len(view.Snapshot.Scenarios), len(view.Snapshot.Profiles))
		}

		assertNativeBoundarySerializedBudget(t, view.Snapshot)
	})
}

func seedMaximumBoundaryModel(t *testing.T, p mutationProbe, base memory.CanonicalMutation) {
	t.Helper()

	children := []memory.CanonicalMutation{
		compatibleBoundaryChild(t, base, memory.DerivedKindScenario),
		compatibleBoundaryChild(t, base, memory.DerivedKindProfile),
	}
	err := p.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		for i := 1; i < 2046; i++ {
			item, revision := base.Items[0], base.Revisions[0]
			item.ItemID = fmt.Sprintf("item-model-%05d", i)
			revision.ItemID = item.ItemID

			revision.RevisionID = "revision-" + item.ItemID
			if err := writeBoundaryModelRecord(t, tx, base.Scope, item, revision); err != nil {
				return err
			}
		}

		for _, child := range children {
			payload := child.Payloads[len(child.Payloads)-1]
			key := canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(payload.Ref.ID)}}

			encoded, err := canonicalsql.Encode(canonicalsql.Payload, payload.Data)
			if err != nil {
				return err
			}

			if err := tx.Put(t.Context(), key, encoded); err != nil {
				return err
			}

			if err := writeBoundaryModelRecord(t, tx, base.Scope, child.Items[0], child.Revisions[0]); err != nil {
				return err
			}
		}

		return nil
	})
	requireProjectionNoError(t, err)
}

func writeBoundaryModelRecord(t *testing.T, tx *canonicalsql.Tx, scope memory.Scope,
	item memory.MemoryItem, revision memory.MemoryRevision) error {
	t.Helper()

	for _, row := range []struct {
		family canonicalsql.Family
		id     string
		value  any
	}{
		{canonicalsql.Item, item.ItemID, item},
		{canonicalsql.Revision, revision.RevisionID, revision},
		{canonicalsql.Head, item.ItemID, memory.ItemHead{ItemID: item.ItemID, RevisionID: revision.RevisionID}},
	} {
		data, err := canonicalsql.Encode(row.family, row.value)
		if err != nil {
			return err
		}

		key := canonicalsql.Key{Family: row.family, Scope: scope, Parts: [][]byte{[]byte(row.id)}}
		if err := tx.Put(t.Context(), key, data); err != nil {
			return err
		}
	}

	return nil
}

type nativeBoundaryBudgetInvoker struct{ calls int }

func (i *nativeBoundaryBudgetInvoker) Invoke(context.Context, derive.StructuredInvocation) ([]byte, error) {
	i.calls++

	return []byte(`{"output":[]}`), nil
}

func (*nativeBoundaryBudgetInvoker) Close(context.Context) error { return nil }

func assertNativeBoundarySerializedBudget(t *testing.T, snapshot memory.ScopeSnapshot) {
	t.Helper()

	invoker := &nativeBoundaryBudgetInvoker{}
	deriver, err := derive.NewDeriver(invoker)
	requireProjectionNoError(t, err)
	boundary, err := memory.NewBoundary(snapshot.Scope,
		memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"boundary", memory.BoundaryReasonClose, time.Now().UTC())
	requireProjectionNoError(t, err)

	request := memory.ScenarioSynthesisRequest{SchemaVersion: memory.DerivedSchemaVersionV1,
		Derivation: memory.LegacyDerivationRef(), Boundary: boundary,
		View: memory.ScopeView{SchemaVersion: snapshot.SchemaVersion, Scope: snapshot.Scope, Version: snapshot.Version,
			Atoms: snapshot.Atoms, Scenarios: snapshot.Scenarios, Profiles: snapshot.Profiles}}
	_, err = deriver.SynthesizeScenarios(t.Context(), request)
	assertMutationCode(t, err, memory.CodeLimitExceeded)

	if invoker.calls != 0 {
		t.Fatal("oversized serialized model input reached invocation")
	}
}
