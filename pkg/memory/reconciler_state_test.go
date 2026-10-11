package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import "testing"

func TestPolicyRegistryReconcilesStateParaphrases(t *testing.T) {
	t.Parallel()

	scope := memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}
	policy := memory.PolicyRegistry{Version: "policy-v1"}

	first, err := policy.Reconcile(
		scope,
		reconciliationCandidate(memory.MemoryKindState, "Ada", "Lives In", []string{"Bishkek"}),
		nil,
	)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	second, err := policy.Reconcile(
		scope,
		reconciliationCandidate(memory.MemoryKindState, " ada ", "lives   in", []string{"bishkek"}),
		[]memory.MemoryItem{first.Item},
	)
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}

	if second.Action != memory.ReconciliationActionSupersede ||
		second.Item.ItemID != first.Item.ItemID ||
		second.Lifecycle.Type != memory.LifecycleEventSupersede {
		t.Fatalf("state reconciliation = %+v", second)
	}
}

func TestPolicyRegistryRetainsEventIdentityAcrossEdit(t *testing.T) {
	t.Parallel()

	scope := memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}
	policy := memory.PolicyRegistry{Version: "policy-v1"}
	candidate := reconciliationCandidate(memory.MemoryKindEvent, "Ada", "Visited", []string{"Bishkek"})

	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	edited := candidate
	edited.Predicate = "Visited Again"

	second, err := policy.ReconcileActive(
		scope,
		edited,
		[]memory.ActiveMemoryItem{{Item: first.Item, Evidence: candidate.Memory.Evidence}},
	)
	if err != nil {
		t.Fatalf("edited ReconcileActive() error = %v", err)
	}

	if second.Action != memory.ReconciliationActionSupersede ||
		second.Item.ItemID != first.Item.ItemID {
		t.Fatalf("event edit reconciliation = %+v", second)
	}
}
