package badger

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func migratedTraceSourceRecord(payload []byte, scope memory.Scope) (memory.SourceRecord, bool, error) {
	var migrated memory.SourceRecord
	if err := json.Unmarshal(payload, &migrated); err == nil && migrated.Ref.Scope == scope && migrated.Ref.ConversationID != "" {
		if migrated.SchemaVersion == memory.DerivedSchemaVersionV1 && migrated.State == memory.SourceStateActive && migrated.Turn != nil {
			if err := migrated.Validate(); err != nil {
				return memory.SourceRecord{}, true, memory.PermanentError(memory.CodeStoreFailure, "migrated canonical source is invalid", err)
			}
			return migrated, true, nil
		}
	}
	return memory.SourceRecord{}, false, nil
}
