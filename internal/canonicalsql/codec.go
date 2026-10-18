package canonicalsql

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// MaxEnvelopeBytes bounds encoded values before copying or decoding.
const MaxEnvelopeBytes = 4 * 1024 * 1024

// Encode retains the existing deterministic JSON-in-RecordEnvelope codec.
// Domain callers remain responsible for validating their record/proof contracts.
func Encode(family Family, value any) ([]byte, error) {
	if _, err := familyDefinition(family); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return nil, invalid("canonical SQL payload cannot be encoded")
	}

	if len(payload) > MaxEnvelopeBytes {
		return nil, limitError()
	}

	encoded, err := memory.MarshalRecordEnvelope(string(family), payload)
	if err != nil {
		return nil, invalid("canonical SQL envelope cannot be encoded")
	}

	if len(encoded) > MaxEnvelopeBytes {
		return nil, limitError()
	}

	return encoded, nil
}

// Decode checks envelope length, checksum, type and JSON before decoding a DTO.
// It does not replace the consumer's domain-specific validation.
func Decode(family Family, encoded []byte, value any) error {
	payload, err := decodePayload(family, encoded)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(payload, value); err != nil {
		return invalid("invalid canonical SQL payload")
	}

	return nil
}

func decodePayload(family Family, encoded []byte) ([]byte, error) {
	if _, err := familyDefinition(family); err != nil {
		return nil, err
	}

	if len(encoded) > MaxEnvelopeBytes {
		return nil, limitError()
	}

	if len(encoded) == 0 {
		return nil, invalid("empty canonical SQL envelope")
	}

	envelope, err := memory.UnmarshalRecordEnvelope(encoded)
	if err != nil || envelope.RecordType != string(family) || !json.Valid(envelope.Payload) {
		return nil, invalid("invalid canonical SQL envelope")
	}

	return envelope.Payload, nil
}

func extraColumns(f Family) []string {
	switch f {
	case ScopeState:
		return []string{"version_u64", "change_seq_u64"}
	case Message:
		return []string{"source_id"}
	case Revision:
		return []string{"item_id", "revision_u64"}
	default:
		return nil
	}
}

// Materialized columns are derived from the envelope on write and compared on
// read. Only structural query fields live here; privacy/proof decisions do not.
func columnValues(key Key, encoded []byte) ([][]byte, error) {
	payload, err := decodePayload(key.Family, encoded)
	if err != nil {
		return nil, err
	}

	switch key.Family {
	case ScopeState:
		return scopeColumns(key, payload)
	case Message:
		return messageColumns(key, payload)
	case Revision:
		return revisionColumns(key, payload)
	default:
		return nil, nil
	}
}

func scopeColumns(key Key, payload []byte) ([][]byte, error) {
	var state memory.ScopeState
	if json.Unmarshal(payload, &state) != nil || state.Validate() != nil || state.Scope != key.Scope {
		return nil, invalid("canonical SQL scope binding mismatch")
	}

	return [][]byte{Uint64(state.Version), Uint64(state.ChangeSeq)}, nil
}

func messageColumns(key Key, payload []byte) ([][]byte, error) {
	var record memory.MessageRecord
	if json.Unmarshal(payload, &record) != nil || record.MessageID != string(key.Parts[0]) {
		return nil, invalid("canonical SQL message binding mismatch")
	}

	if err := validatePart("source_id", []byte(record.SourceID)); err != nil {
		return nil, err
	}

	return [][]byte{[]byte(record.SourceID)}, nil
}

func revisionColumns(key Key, payload []byte) ([][]byte, error) {
	var record memory.MemoryRevision
	if json.Unmarshal(payload, &record) != nil || record.RevisionID != string(key.Parts[0]) {
		return nil, invalid("canonical SQL revision binding mismatch")
	}

	if err := validatePart("item_id", []byte(record.ItemID)); err != nil {
		return nil, err
	}

	return [][]byte{[]byte(record.ItemID), Uint64(record.Revision)}, nil
}
