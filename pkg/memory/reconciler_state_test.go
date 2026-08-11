package memory

import "testing"

func TestPolicyRegistryReconcilesStateParaphrases(t *testing.T) {
	t.Parallel()

	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	policy := PolicyRegistry{Version: "policy-v1"}
	first, err := policy.Reconcile(scope, reconciliationCandidate(MemoryKindState, "Ada", "Lives In", []string{"Bishkek"}), nil)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	second, err := policy.Reconcile(scope, reconciliationCandidate(MemoryKindState, " ada ", "lives   in", []string{"bishkek"}), []MemoryItem{first.Item})
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if second.Action != ReconciliationActionSupersede || second.Item.ItemID != first.Item.ItemID || second.Lifecycle.Type != LifecycleEventSupersede {
		t.Fatalf("state reconciliation = %+v", second)
	}
}

func TestPolicyRegistryRetainsEventIdentityAcrossEdit(t *testing.T) {
	t.Parallel()

	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	policy := PolicyRegistry{Version: "policy-v1"}
	candidate := reconciliationCandidate(MemoryKindEvent, "Ada", "Visited", []string{"Bishkek"})
	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	edited := candidate
	edited.Predicate = "Visited Again"
	second, err := policy.ReconcileActive(scope, edited, []ActiveMemoryItem{{Item: first.Item, Evidence: candidate.Memory.Evidence}})
	if err != nil {
		t.Fatalf("edited ReconcileActive() error = %v", err)
	}
	if second.Action != ReconciliationActionSupersede || second.Item.ItemID != first.Item.ItemID {
		t.Fatalf("event edit reconciliation = %+v", second)
	}
}
