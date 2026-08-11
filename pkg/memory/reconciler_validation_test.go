package memory

import (
	"testing"
	"time"
)

func TestPolicyRegistryRejectsForeignAndAmbiguousActiveItems(t *testing.T) {
	t.Parallel()

	policy := PolicyRegistry{Version: "policy-v1"}
	candidate := reconciliationCandidate(MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	foreign := MemoryItem{ItemID: "item-1", Scope: Scope{Key: "telegram:2:0", Kind: ScopeKindPersonal}, Kind: MemoryKindState, MemoryKey: "key-1"}
	if _, err := policy.Reconcile(scope, candidate, []MemoryItem{foreign}); err == nil {
		t.Fatal("foreign active item was accepted")
	}
	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if _, err := policy.Reconcile(scope, candidate, []MemoryItem{first.Item, first.Item}); err == nil {
		t.Fatal("ambiguous active state matches were accepted")
	}
}

func TestPolicyRegistryRejectsInvalidEvidenceAndTime(t *testing.T) {
	t.Parallel()

	policy := PolicyRegistry{Version: "policy-v1"}
	scope := Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
	candidate := reconciliationCandidate(MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	candidate.Memory.Evidence[0].Role = MessageRoleAssistant
	if _, err := policy.Reconcile(scope, candidate, nil); err == nil {
		t.Fatal("assistant-only evidence was accepted")
	}
	candidate = reconciliationCandidate(MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	from := candidate.Memory.Temporal.ObservedAt.Add(time.Hour)
	until := candidate.Memory.Temporal.ObservedAt
	candidate.Memory.Temporal.ValidFrom = &from
	candidate.Memory.Temporal.ValidUntil = &until
	if _, err := policy.Reconcile(scope, candidate, nil); err == nil {
		t.Fatal("inverted temporal interval was accepted")
	}
}
