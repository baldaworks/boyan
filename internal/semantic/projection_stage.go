package semantic

import "context"

// Logical key overhead preserves v1 encoded-row admission accounting. It is
// independent of SQL file size, indexes, WAL pages, and physical storage quotas.
const (
	LogicalStateKeyBytes = 67
	LogicalChunkKeyBytes = 71
)

// PreflightStage validates complete generation metadata and computes its exact
// encoded byte budget without writing state. Missing vectors are represented by
// one valid dimension-sized placeholder because encoded vector size depends on
// dimensions, not component values.
func PreflightStage(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []ProjectionChunk,
) (State, error) {
	if ctx == nil || ValidateGeneration(generation) != nil {
		return State{}, ErrInvalid
	}

	if err := CheckProjectionBounds(len(chunks), 0); err != nil {
		return State{}, err
	}

	placeholder := make([]float32, generation.Dimensions)
	placeholder[0] = 1
	preflightChunks := make([]ProjectionChunk, len(chunks))
	copy(preflightChunks, chunks)

	for index := range preflightChunks {
		if len(preflightChunks[index].Vector) == 0 {
			preflightChunks[index].Vector = placeholder
		}
	}

	if err := PreflightStageInputs(ctx, generation, coverage, preflightChunks); err != nil {
		return State{}, err
	}

	state, _, err := PrepareProjection(ctx, generation, coverage, preflightChunks)

	return state, err
}

// PrepareProjection validates admission bounds before copying, then returns an
// owned canonical ordering and the v1 logical encoded-row budget.
//
//nolint:cyclop,funlen // Admission, coverage and encoding gates intentionally remain explicit.
func PrepareProjection(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []ProjectionChunk,
) (State, []ProjectionChunk, error) {
	if ctx == nil || ValidateGeneration(generation) != nil {
		return State{}, nil, ErrInvalid
	}

	if err := PreflightStageInputs(ctx, generation, coverage, chunks); err != nil {
		return State{}, nil, err
	}

	orderedCoverage, orderedChunks, coverageHash, err := ValidateAndOrder(ctx, generation, coverage, chunks)
	if err != nil {
		return State{}, nil, err
	}

	state := State{
		SchemaVersion:       ProjectionSchemaVersion,
		Scope:               generation.Scope,
		BuildID:             generation.BuildID,
		LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark:    generation.LexicalWatermark,
		ModelSpace:          generation.ModelSpace,
		Dimensions:          generation.Dimensions,
		Status:              StatusReady,
		ChunkCount:          uint32(len(orderedChunks)),
		RecordCount:         uint32(len(orderedCoverage)),
		CoverageHash:        coverageHash,
		Omitted:             generation.Omitted,
		ReadyAt:             generation.ReadyAt.UTC(),
	}

	encodedBytes := uint64(0)

	for _, chunk := range orderedChunks {
		if err := ctx.Err(); err != nil {
			return State{}, nil, err
		}

		value, encodeErr := EncodeChunk(chunk)
		if encodeErr != nil {
			return State{}, nil, encodeErr
		}

		if len(value) > MaxChunkValueBytes {
			return State{}, nil, ErrCapacity
		}

		encodedBytes += uint64(LogicalChunkKeyBytes + len(value))
		if encodedBytes > uint64(ProjectionByteLimit) {
			return State{}, nil, ErrCapacity
		}
	}

	state, err = FinalizeEncodedBytes(state, encodedBytes)
	if err != nil {
		return State{}, nil, err
	}

	return state, orderedChunks, nil
}

//nolint:cyclop // Each independent bound must be checked before cloning caller-owned data.
func PreflightStageInputs(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []ProjectionChunk,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(coverage) > ProjectionChunkLimit || len(chunks) > ProjectionChunkLimit {
		return ErrCapacity
	}

	if len(coverage) > len(chunks) {
		return ErrInvalid
	}

	coveredChunks := uint64(0)

	for _, record := range coverage {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(record.ItemID) > MaxIdentityBytes || len(record.RevisionID) > MaxIdentityBytes ||
			len(record.ContentHash) > 64 {
			return ErrInvalid
		}

		coveredChunks += uint64(record.ChunkCount)
		if coveredChunks > ProjectionChunkLimit {
			return ErrCapacity
		}

		if coveredChunks > uint64(len(chunks)) {
			return ErrInvalid
		}
	}

	if coveredChunks != uint64(len(chunks)) {
		return ErrInvalid
	}

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !ChunkWithinPreflightBounds(generation, chunk) {
			return ErrInvalid
		}
	}

	return nil
}

//nolint:cyclop // This is an exhaustive declarative bound check over the encoded record.
func ChunkWithinPreflightBounds(generation Generation, chunk ProjectionChunk) bool {
	if len(chunk.Scope.Key) > MaxScopeKeyBytes || len(chunk.BuildID) > MaxIdentityBytes ||
		len(chunk.LexicalGenerationID) > MaxIdentityBytes || len(chunk.ModelSpace) > 64 ||
		len(chunk.ItemID) > MaxIdentityBytes || len(chunk.RevisionID) > MaxIdentityBytes ||
		len(chunk.ContentHash) > 64 || len(chunk.InputHash) > 64 ||
		len(chunk.Category) > MaxIdentityBytes || len(chunk.MemoryKey) > MaxIdentityBytes ||
		len(chunk.SourceIDs) > MaxMetadataIDs || len(chunk.ConversationIDs) > MaxMetadataIDs ||
		len(chunk.Vector) != generation.Dimensions {
		return false
	}

	for _, values := range [][]string{chunk.SourceIDs, chunk.ConversationIDs} {
		for _, value := range values {
			if len(value) > MaxIdentityBytes {
				return false
			}
		}
	}

	return true
}
