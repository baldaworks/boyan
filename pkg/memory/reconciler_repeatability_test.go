package memory

import "testing"

func TestPolicyRegistryIsRepeatable(t *testing.T) {
	t.Parallel()

	policy := PolicyRegistry{Version: "policy-v1"}
	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	candidate := reconciliationCandidate(MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	second, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if first.Item != second.Item || first.RevisionID != second.RevisionID || first.Lifecycle != second.Lifecycle {
		t.Fatalf("reconciliation is not repeatable: %+v != %+v", first, second)
	}
}
