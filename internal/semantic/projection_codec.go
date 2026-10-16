package semantic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/baldaworks/boyan/pkg/memory"
	"io"
	"math"
)

type StoredProjectionState struct {
	SchemaVersion string `json:"schema_version"`
	Complete      bool   `json:"complete"`
	State         State  `json:"state"`
}

type StoredProjectionChunk struct {
	SchemaVersion       string   `json:"schema_version"`
	ScopeKey            string   `json:"scope_key"`
	ScopeKind           string   `json:"scope_kind"`
	BuildID             string   `json:"build_id"`
	LexicalGenerationID string   `json:"lexical_generation_id"`
	LexicalWatermark    uint64   `json:"lexical_watermark"`
	ModelSpace          string   `json:"model_space"`
	Dimensions          int      `json:"dimensions"`
	ItemID              string   `json:"item_id"`
	RevisionID          string   `json:"revision_id"`
	Revision            uint64   `json:"revision"`
	Ordinal             uint32   `json:"ordinal"`
	ContentHash         string   `json:"content_hash"`
	InputHash           string   `json:"input_hash"`
	ScopeChangeSeq      uint64   `json:"scope_change_seq"`
	Kind                string   `json:"kind"`
	Category            string   `json:"category,omitempty"`
	MemoryKey           string   `json:"memory_key,omitempty"`
	CreatedAt           string   `json:"created_at"`
	ObservedAt          string   `json:"observed_at,omitempty"`
	ValidFrom           string   `json:"valid_from,omitempty"`
	ValidUntil          string   `json:"valid_until,omitempty"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	SourceIDs           []string `json:"source_ids,omitempty"`
	ConversationIDs     []string `json:"conversation_ids,omitempty"`
	Vector              []byte   `json:"vector"`
}

type ProjectionActivePointer struct {
	SchemaVersion string `json:"schema_version"`
	ScopeKey      string `json:"scope_key"`
	ScopeKind     string `json:"scope_kind"`
	BuildID       string `json:"build_id"`
	Epoch         uint64 `json:"epoch"`
}

const Float32Bytes = 4

const (
	EncodedSizeConvergenceLimit = 20
	// MaxControlValueBytes bounds JSON, which can expand a byte sixfold.
	// A validated state has at most a 4096-byte scope plus two 512-byte
	// identities, so all variable caller text occupies at most 30,720 encoded
	// bytes. Pointer text is smaller (27,648 bytes). Fixed hashes, enums,
	// timestamps, numeric fields, field names, and JSON framing keep both well
	// below this shared 64 KiB bound. Every control-record writer checks the
	// bound before its first write, and readers use the same cap.
	MaxControlValueBytes = 64 << 10
	MaxActiveValueBytes  = MaxControlValueBytes
	MaxStateValueBytes   = MaxControlValueBytes
	// MaxChunkValueBytes bounds two worst-case metadata arrays: 2*256*512 bytes,
	// each expanded at most sixfold by JSON's \u00xx escaping. The remaining
	// bounded identities and 4096-dimensional binary vector keep the complete
	// encoded record below 2 MiB; the aggregate reader budget remains 64 MiB.
	MaxChunkValueBytes = 2 << 20
)

func EncodeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode record", ErrInvalid)
	}

	return encoded, nil
}

func EncodeStoredState(record StoredProjectionState) ([]byte, error) {
	return EncodeBoundedControlJSON(record, MaxStateValueBytes)
}

func EncodeActivePointer(pointer ProjectionActivePointer) ([]byte, error) {
	return EncodeBoundedControlJSON(pointer, MaxActiveValueBytes)
}

func EncodeBoundedControlJSON(value any, maximum int) ([]byte, error) {
	encoded, err := EncodeJSON(value)
	if err != nil {
		return nil, err
	}

	if len(encoded) > maximum {
		return nil, ErrCapacity
	}

	return encoded, nil
}

func DecodeJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode record", ErrCorrupt)
	}

	if err := decoder.Decode(&struct{}{}); !ErrorsIsEOF(err) {
		return fmt.Errorf("%w: trailing record data", ErrCorrupt)
	}

	return nil
}

func ErrorsIsEOF(err error) bool {
	return err == io.EOF
}

func EncodeChunk(chunk ProjectionChunk) ([]byte, error) {
	vector := make([]byte, len(chunk.Vector)*Float32Bytes)
	for index, component := range chunk.Vector {
		binary.LittleEndian.PutUint32(vector[index*Float32Bytes:], math.Float32bits(component))
	}

	record := StoredProjectionChunk{
		SchemaVersion:       ProjectionSchemaVersion,
		ScopeKey:            chunk.Scope.Key,
		ScopeKind:           string(chunk.Scope.Kind),
		BuildID:             chunk.BuildID,
		LexicalGenerationID: chunk.LexicalGenerationID,
		LexicalWatermark:    chunk.LexicalWatermark,
		ModelSpace:          chunk.ModelSpace,
		Dimensions:          chunk.Dimensions,
		ItemID:              chunk.ItemID,
		RevisionID:          chunk.RevisionID,
		Revision:            chunk.Revision,
		Ordinal:             chunk.Ordinal,
		ContentHash:         chunk.ContentHash,
		InputHash:           chunk.InputHash,
		ScopeChangeSeq:      chunk.ScopeChangeSeq,
		Kind:                string(chunk.Kind),
		Category:            string(chunk.Category),
		MemoryKey:           string(chunk.MemoryKey),
		CreatedAt:           FormatTime(chunk.CreatedAt),
		ObservedAt:          FormatOptionalTime(chunk.ObservedAt),
		ValidFrom:           FormatOptionalTime(chunk.ValidFrom),
		ValidUntil:          FormatOptionalTime(chunk.ValidUntil),
		ExpiresAt:           FormatOptionalTime(chunk.ExpiresAt),
		SourceIDs:           append([]string(nil), chunk.SourceIDs...),
		ConversationIDs:     append([]string(nil), chunk.ConversationIDs...),
		Vector:              vector,
	}

	return EncodeJSON(record)
}

//nolint:funlen,cyclop // Explicit field decoding keeps corrupt-data checks auditable.
func DecodeChunk(encoded []byte) (ProjectionChunk, error) {
	if len(encoded) > MaxChunkValueBytes {
		return ProjectionChunk{}, ErrCorrupt
	}

	var record StoredProjectionChunk
	if err := DecodeJSON(encoded, &record); err != nil || record.SchemaVersion != ProjectionSchemaVersion ||
		PreflightStoredChunk(record) != nil {
		return ProjectionChunk{}, ErrCorrupt
	}

	createdAt, err := ParseTime(record.CreatedAt)
	if err != nil {
		return ProjectionChunk{}, err
	}

	observedAt, err := ParseOptionalTime(record.ObservedAt)
	if err != nil {
		return ProjectionChunk{}, err
	}

	validFrom, err := ParseOptionalTime(record.ValidFrom)
	if err != nil {
		return ProjectionChunk{}, err
	}

	validUntil, err := ParseOptionalTime(record.ValidUntil)
	if err != nil {
		return ProjectionChunk{}, err
	}

	expiresAt, err := ParseOptionalTime(record.ExpiresAt)
	if err != nil {
		return ProjectionChunk{}, err
	}

	vector := make([]float32, len(record.Vector)/Float32Bytes)
	for index := range vector {
		vector[index] = math.Float32frombits(binary.LittleEndian.Uint32(record.Vector[index*Float32Bytes:]))
	}

	chunk := ProjectionChunk{
		Scope:               memory.Scope{Key: record.ScopeKey, Kind: memory.ScopeKind(record.ScopeKind)},
		BuildID:             record.BuildID,
		LexicalGenerationID: record.LexicalGenerationID,
		LexicalWatermark:    record.LexicalWatermark,
		ModelSpace:          record.ModelSpace,
		Dimensions:          record.Dimensions,
		ItemID:              record.ItemID,
		RevisionID:          record.RevisionID,
		Revision:            record.Revision,
		Ordinal:             record.Ordinal,
		ContentHash:         record.ContentHash,
		InputHash:           record.InputHash,
		ScopeChangeSeq:      record.ScopeChangeSeq,
		Kind:                memory.MemoryKind(record.Kind),
		Category:            memory.AtomCategory(record.Category),
		MemoryKey:           memory.MemoryKey(record.MemoryKey),
		CreatedAt:           createdAt,
		ObservedAt:          observedAt,
		ValidFrom:           validFrom,
		ValidUntil:          validUntil,
		ExpiresAt:           expiresAt,
		SourceIDs:           append([]string(nil), record.SourceIDs...),
		ConversationIDs:     append([]string(nil), record.ConversationIDs...),
		Vector:              vector,
	}

	return chunk, nil
}

func PreflightStoredChunk(record StoredProjectionChunk) error {
	if record.Dimensions < 1 || record.Dimensions > MaxEmbeddingDimensions ||
		len(record.Vector) != record.Dimensions*Float32Bytes {
		return ErrCorrupt
	}

	if len(record.SourceIDs) > MaxMetadataIDs || len(record.ConversationIDs) > MaxMetadataIDs {
		return ErrCorrupt
	}

	for _, values := range [][]string{record.SourceIDs, record.ConversationIDs} {
		for _, value := range values {
			if len(value) > MaxIdentityBytes {
				return ErrCorrupt
			}
		}
	}

	return nil
}

func StateFromGeneration(state State) Generation {
	return Generation{
		Scope:               state.Scope,
		BuildID:             state.BuildID,
		LexicalGenerationID: state.LexicalGenerationID,
		LexicalWatermark:    state.LexicalWatermark,
		ModelSpace:          state.ModelSpace,
		Dimensions:          state.Dimensions,
		Omitted:             state.Omitted,
		ReadyAt:             state.ReadyAt,
	}
}

func StateMatchesView(state State, view View) bool {
	return state.Scope == view.Scope && state.LexicalGenerationID == view.LexicalGenerationID &&
		state.LexicalWatermark == view.LexicalWatermark && state.ModelSpace == view.ModelSpace &&
		state.Dimensions == view.Dimensions
}

func FinalizeEncodedBytes(state State, rowBytes uint64) (State, error) {
	for range EncodedSizeConvergenceLimit {
		encoded, err := EncodeStoredState(StoredProjectionState{
			SchemaVersion: ProjectionSchemaVersion, Complete: true, State: state,
		})
		if err != nil {
			return State{}, err
		}

		actual := rowBytes + uint64(LogicalStateKeyBytes+len(encoded))
		if actual == state.EncodedBytes {
			if err := CheckProjectionBounds(int(state.ChunkCount), actual); err != nil {
				return State{}, err
			}

			return state, nil
		}

		state.EncodedBytes = actual
	}

	return State{}, ErrInvalid
}
