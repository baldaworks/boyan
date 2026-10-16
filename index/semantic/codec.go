package semantic

import semanticcore "github.com/baldaworks/boyan/internal/semantic"

func encodeJSON(value any) ([]byte, error) { return semanticcore.EncodeJSON(value) }

func encodeStoredState(record storedState) ([]byte, error) {
	return semanticcore.EncodeStoredState(record)
}

func encodeActivePointer(pointer activePointer) ([]byte, error) {
	return semanticcore.EncodeActivePointer(pointer)
}

func encodeBoundedControlJSON(value any, maximum int) ([]byte, error) {
	return semanticcore.EncodeBoundedControlJSON(value, maximum)
}

func decodeJSON(encoded []byte, target any) error { return semanticcore.DecodeJSON(encoded, target) }

func errorsIsEOF(err error) bool { return semanticcore.ErrorsIsEOF(err) }

func encodeChunk(chunk Chunk) ([]byte, error) { return semanticcore.EncodeChunk(chunk) }

func decodeChunk(encoded []byte) (Chunk, error) { return semanticcore.DecodeChunk(encoded) }

func preflightStoredChunk(record storedChunk) error { return semanticcore.PreflightStoredChunk(record) }

func stateFromGeneration(state State) Generation { return semanticcore.StateFromGeneration(state) }

func stateMatchesView(state State, view View) bool { return semanticcore.StateMatchesView(state, view) }

func finalizeEncodedBytes(state State, rowBytes uint64) (State, error) {
	return semanticcore.FinalizeEncodedBytes(state, rowBytes)
}

type storedState = semanticcore.StoredProjectionState
type storedChunk = semanticcore.StoredProjectionChunk
type activePointer = semanticcore.ProjectionActivePointer

const float32Bytes = semanticcore.Float32Bytes
const encodedSizeConvergenceLimit = semanticcore.EncodedSizeConvergenceLimit
const maxControlValueBytes = semanticcore.MaxControlValueBytes
const maxActiveValueBytes = semanticcore.MaxActiveValueBytes
const maxStateValueBytes = semanticcore.MaxStateValueBytes

// Badger's ValueSize estimate can exceed the callback value by 16 bytes.
const badgerValueSizeOverestimate = 16
const maxChunkValueBytes = semanticcore.MaxChunkValueBytes
