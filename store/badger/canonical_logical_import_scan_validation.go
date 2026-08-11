package badger

import (
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateCanonicalLogicalRecord(record CanonicalLogicalExportRecord) error {
	if record.Kind != canonicalLogicalRecordKind || record.RecordType == "" || len(record.Envelope) == 0 {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record is invalid", nil)
	}
	if !isExportableCanonicalRecordType(record.RecordType) || len(record.Components) == 0 || record.Components[0] != record.RecordType {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record tuple is invalid", nil)
	}
	if _, err := encodeCanonicalLogicalKey(record.Components, record.Suffix); err != nil {
		return err
	}
	envelope, err := memory.UnmarshalRecordEnvelope(record.Envelope)
	if err != nil || envelope.RecordType != record.RecordType {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical record envelope is invalid", err)
	}
	return nil
}

func canonicalLogicalScopeRecord(record CanonicalLogicalExportRecord) (memory.Scope, bool) {
	if record.RecordType != badgerRecordScope || len(record.Components) != 3 {
		return memory.Scope{}, false
	}
	scope := memory.Scope{Key: record.Components[1], Kind: memory.ScopeKind(record.Components[2])}
	if err := scope.Validate(); err != nil {
		return memory.Scope{}, false
	}
	return scope, true
}

func sortedCanonicalLogicalScopes(scopes map[string]memory.Scope) []memory.Scope {
	result := make([]memory.Scope, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scope)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}
