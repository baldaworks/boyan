package canonicalsql

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func migratedTraceSourceRecord(payload []byte, scope memory.Scope) (memory.SourceRecord, bool, error) {
	var (
		migrated memory.SourceRecord
		shape    map[string]json.RawMessage
	)
	if err := json.Unmarshal(payload, &shape); err != nil {
		return memory.SourceRecord{}, false, nil
	}

	if _, recognized := shape["ref"]; !recognized {
		return memory.SourceRecord{}, false, nil
	}

	if err := json.Unmarshal(payload, &migrated); err != nil {
		return memory.SourceRecord{},
			true,
			memory.PermanentError(memory.CodeStoreFailure,
				"migrated source is malformed",
				err)
	}

	if err := migrated.Validate(); err != nil {
		return memory.SourceRecord{}, true, err
	}

	if migrated.Ref.Scope != scope {
		return memory.SourceRecord{},
			true,
			memory.PermanentError(memory.CodeScopeViolation,
				"migrated source scope does not match",
				nil)
	}

	if migrated.State != memory.SourceStateActive {
		return memory.SourceRecord{}, true, memory.PermanentError(memory.CodeForgotten, "migrated source is forgotten", nil)
	}

	return migrated, true, nil
}
