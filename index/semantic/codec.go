package semantic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/baldaworks/boyan/pkg/memory"
	"io"
	"math"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
)

type storedState struct {
	SchemaVersion string `json:"schema_version"`
	Complete      bool   `json:"complete"`
	State         State  `json:"state"`
}

type storedChunk struct {
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

type activePointer struct {
	SchemaVersion string `json:"schema_version"`
	ScopeKey      string `json:"scope_key"`
	ScopeKind     string `json:"scope_kind"`
	BuildID       string `json:"build_id"`
	Epoch         uint64 `json:"epoch"`
}

const float32Bytes = 4

const (
	encodedSizeConvergenceLimit = 20
	// JSON can expand a byte sixfold (for example, \x01 becomes \u0001).
	// A validated state has at most a 4096-byte scope plus two 512-byte
	// identities, so all variable caller text occupies at most 30,720 encoded
	// bytes. Pointer text is smaller (27,648 bytes). Fixed hashes, enums,
	// timestamps, numeric fields, field names, and JSON framing keep both well
	// below this shared 64 KiB bound. Every control-record writer checks the
	// bound before its first Badger write, and readers use the same cap.
	maxControlValueBytes = 64 << 10
	maxActiveValueBytes  = maxControlValueBytes
	maxStateValueBytes   = maxControlValueBytes
	// Badger's ValueSize subtracts an assumed six-byte value-log header. The
	// actual varint header is 5--22 bytes, so the estimate can exceed the value
	// by at most 16 bytes. Exact callback length is checked before every copy.
	badgerValueSizeOverestimate = 16
	// Two metadata arrays dominate the worst-case chunk: 2*256*512 bytes,
	// each expanded at most sixfold by JSON's \u00xx escaping. The remaining
	// bounded identities and 4096-dimensional binary vector keep the complete
	// encoded record below 2 MiB; the aggregate reader budget remains 64 MiB.
	maxChunkValueBytes = 2 << 20
)

func encodeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode record", ErrInvalid)
	}

	return encoded, nil
}

func encodeStoredState(record storedState) ([]byte, error) {
	return encodeBoundedControlJSON(record, maxStateValueBytes)
}

func encodeActivePointer(pointer activePointer) ([]byte, error) {
	return encodeBoundedControlJSON(pointer, maxActiveValueBytes)
}

func encodeBoundedControlJSON(value any, maximum int) ([]byte, error) {
	encoded, err := encodeJSON(value)
	if err != nil {
		return nil, err
	}

	if len(encoded) > maximum {
		return nil, ErrCapacity
	}

	return encoded, nil
}

func decodeJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode record", ErrCorrupt)
	}

	if err := decoder.Decode(&struct{}{}); !errorsIsEOF(err) {
		return fmt.Errorf("%w: trailing record data", ErrCorrupt)
	}

	return nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

func encodeChunk(chunk Chunk) ([]byte, error) {
	vector := make([]byte, len(chunk.Vector)*float32Bytes)
	for index, component := range chunk.Vector {
		binary.LittleEndian.PutUint32(vector[index*float32Bytes:], math.Float32bits(component))
	}

	record := storedChunk{
		SchemaVersion:       schemaVersion,
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
		CreatedAt:           formatTime(chunk.CreatedAt),
		ObservedAt:          formatOptionalTime(chunk.ObservedAt),
		ValidFrom:           formatOptionalTime(chunk.ValidFrom),
		ValidUntil:          formatOptionalTime(chunk.ValidUntil),
		ExpiresAt:           formatOptionalTime(chunk.ExpiresAt),
		SourceIDs:           append([]string(nil), chunk.SourceIDs...),
		ConversationIDs:     append([]string(nil), chunk.ConversationIDs...),
		Vector:              vector,
	}

	return encodeJSON(record)
}

//nolint:funlen,cyclop // Explicit field decoding keeps corrupt-data checks auditable.
func decodeChunk(encoded []byte) (Chunk, error) {
	if len(encoded) > maxChunkValueBytes {
		return Chunk{}, ErrCorrupt
	}

	var record storedChunk
	if err := decodeJSON(encoded, &record); err != nil || record.SchemaVersion != schemaVersion ||
		preflightStoredChunk(record) != nil {
		return Chunk{}, ErrCorrupt
	}

	createdAt, err := parseTime(record.CreatedAt)
	if err != nil {
		return Chunk{}, err
	}

	observedAt, err := parseOptionalTime(record.ObservedAt)
	if err != nil {
		return Chunk{}, err
	}

	validFrom, err := parseOptionalTime(record.ValidFrom)
	if err != nil {
		return Chunk{}, err
	}

	validUntil, err := parseOptionalTime(record.ValidUntil)
	if err != nil {
		return Chunk{}, err
	}

	expiresAt, err := parseOptionalTime(record.ExpiresAt)
	if err != nil {
		return Chunk{}, err
	}

	vector := make([]float32, len(record.Vector)/float32Bytes)
	for index := range vector {
		vector[index] = math.Float32frombits(binary.LittleEndian.Uint32(record.Vector[index*float32Bytes:]))
	}

	chunk := Chunk{
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

func preflightStoredChunk(record storedChunk) error {
	if record.Dimensions < 1 || record.Dimensions > semanticcore.MaxEmbeddingDimensions ||
		len(record.Vector) != record.Dimensions*float32Bytes {
		return ErrCorrupt
	}

	if len(record.SourceIDs) > maxMetadataIDs || len(record.ConversationIDs) > maxMetadataIDs {
		return ErrCorrupt
	}

	for _, values := range [][]string{record.SourceIDs, record.ConversationIDs} {
		for _, value := range values {
			if len(value) > maxIdentityBytes {
				return ErrCorrupt
			}
		}
	}

	return nil
}

func stateFromGeneration(state State) Generation {
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

func stateMatchesView(state State, view View) bool {
	return state.Scope == view.Scope && state.LexicalGenerationID == view.LexicalGenerationID &&
		state.LexicalWatermark == view.LexicalWatermark && state.ModelSpace == view.ModelSpace &&
		state.Dimensions == view.Dimensions
}

func finalizeEncodedBytes(state State, rowBytes uint64) (State, error) {
	for range encodedSizeConvergenceLimit {
		encoded, err := encodeStoredState(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
		if err != nil {
			return State{}, err
		}

		actual := rowBytes + uint64(len(stateKey(state.Scope, state.BuildID))+len(encoded))
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
