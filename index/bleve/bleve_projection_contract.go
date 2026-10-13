package bleve

const (
	// BleveRecallProjectionSchema pins the application-owned projection
	// document contract independently of Bleve's internal index metadata.
	BleveRecallProjectionSchema  = "boyan-bleve/v2"
	bleveActiveGenerationFile    = ".active-generation"
	bleveScopedActivePrefix      = ".active-generation.scope."
	bleveCommittedGenerationFile = ".committed"
	bleveGenerationDirectory     = "generations"
	bleveBatchSize               = 128
)

const (
	bleveFieldTextStandard    = "text_standard"
	bleveFieldTextRussian     = "text_russian"
	bleveFieldTextEnglish     = "text_english"
	bleveFieldScopeKey        = "scope_key"
	bleveFieldScopeKind       = "scope_kind"
	bleveFieldItemID          = "item_id"
	bleveFieldRevisionID      = "revision_id"
	bleveFieldRevision        = "revision"
	bleveFieldKind            = "kind"
	bleveFieldCategory        = "category"
	bleveFieldMemoryKey       = "memory_key"
	bleveFieldCreatedAt       = "created_at"
	bleveFieldSourceIDs       = "source_ids"
	bleveFieldConversationIDs = "conversation_ids"
	bleveFieldChangeSeq       = "scope_change_seq"
)
