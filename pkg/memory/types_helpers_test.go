package memory

func testIdentity() (Scope, ConversationRef) {
	return Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}, ConversationRef{
		ConversationID:      "tg-1-0",
		AgentConversationID: "tg-1-0",
		LineageID:           "lineage-1",
	}
}
