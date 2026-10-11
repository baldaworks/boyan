package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

func testIdentity() (memory.Scope, memory.ConversationRef) {
	return memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}, memory.ConversationRef{
		ConversationID:      "tg-1-0",
		AgentConversationID: "tg-1-0",
		LineageID:           "lineage-1",
	}
}
