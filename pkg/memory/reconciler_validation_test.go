package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"testing"
	"time"
)

func TestPolicyRegistryRejectsForeignAndAmbiguousActiveItems(t *testing.T) {
	t.Parallel()

	policy := memory.PolicyRegistry{Version: "policy-v1"}
	candidate := reconciliationCandidate(memory.MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	scope := memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}

	foreign := memory.MemoryItem{
		ItemID:    "item-1",
		Scope:     memory.Scope{Key: "telegram:2:0", Kind: memory.ScopeKindPersonal},
		Kind:      memory.MemoryKindState,
		MemoryKey: "key-1",
	}
	if _, err := policy.Reconcile(scope, candidate, []memory.MemoryItem{foreign}); err == nil {
		t.Fatal("foreign active item was accepted")
	}

	first, err := policy.Reconcile(scope, candidate, nil)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if _, err := policy.Reconcile(scope, candidate, []memory.MemoryItem{first.Item, first.Item}); err == nil {
		t.Fatal("ambiguous active state matches were accepted")
	}
}

func TestPolicyRegistryRejectsInvalidEvidenceAndTime(t *testing.T) {
	t.Parallel()

	policy := memory.PolicyRegistry{Version: "policy-v1"}
	scope := memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}
	candidate := reconciliationCandidate(memory.MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})

	candidate.Memory.Evidence[0].Role = memory.MessageRoleAssistant
	if _, err := policy.Reconcile(scope, candidate, nil); err == nil {
		t.Fatal("assistant-only evidence was accepted")
	}

	candidate = reconciliationCandidate(memory.MemoryKindState, "Ada", "Lives In", []string{"Bishkek"})
	from := candidate.Memory.Temporal.ObservedAt.Add(time.Hour)
	until := candidate.Memory.Temporal.ObservedAt
	candidate.Memory.Temporal.ValidFrom = &from

	candidate.Memory.Temporal.ValidUntil = &until
	if _, err := policy.Reconcile(scope, candidate, nil); err == nil {
		t.Fatal("inverted temporal interval was accepted")
	}
}
