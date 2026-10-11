package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import "testing"

func TestPolicyRegistryIsRepeatable(t *testing.T) {
	t.Parallel()

	policy := memory.PolicyRegistry{Version: "policy-v1"}
	scope := memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}
	candidate := reconciliationCandidate(memory.MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})

	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	second, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}

	if first.Item != second.Item || first.RevisionID != second.RevisionID ||
		first.Lifecycle != second.Lifecycle {
		t.Fatalf("reconciliation is not repeatable: %+v != %+v", first, second)
	}
}
