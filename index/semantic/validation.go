package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

const (
	maxIdentityBytes = 512
	maxMetadataIDs   = 256
	maxScopeKeyBytes = 4096
)

func CheckProjectionBounds(chunkCount int, encodedBytes uint64) error {
	if chunkCount < 0 || chunkCount > semanticcore.ProjectionChunkLimit ||
		encodedBytes > uint64(semanticcore.ProjectionByteLimit) {
		return ErrCapacity
	}

	return nil
}

func validateGeneration(generation Generation) error {
	if err := validateView(generation.View()); err != nil {
		return ErrInvalid
	}

	if !validIdentity(generation.BuildID) || generation.ReadyAt.IsZero() {
		return ErrInvalid
	}

	return nil
}

func validateView(view View) error {
	if err := validateScope(view.Scope); err != nil {
		return err
	}

	if !validIdentity(view.LexicalGenerationID) || !validHash(view.ModelSpace) {
		return ErrInvalid
	}

	if view.Dimensions < 1 || view.Dimensions > semanticcore.MaxEmbeddingDimensions {
		return ErrInvalid
	}

	return nil
}

func validateScope(scope memory.Scope) error {
	if len(scope.Key) > maxScopeKeyBytes {
		return ErrInvalid
	}

	if err := scope.Validate(); err != nil {
		return ErrInvalid
	}

	return nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\r\n\t\x00")
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func validateAndOrder(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) ([]CoverageRecord, []Chunk, string, error) {
	orderedCoverage, coverageByRevision, err := orderCoverage(ctx, coverage)
	if err != nil {
		return nil, nil, "", err
	}

	orderedChunks, err := orderChunks(ctx, chunks)
	if err != nil {
		return nil, nil, "", err
	}

	if err := validateChunkCoverage(ctx, generation, coverageByRevision, orderedChunks); err != nil {
		return nil, nil, "", err
	}

	return orderedCoverage, orderedChunks, coverageFingerprint(orderedCoverage), nil
}

func orderCoverage(
	ctx context.Context,
	coverage []CoverageRecord,
) ([]CoverageRecord, map[string]CoverageRecord, error) {
	ordered := append([]CoverageRecord(nil), coverage...)
	canceled := false

	sort.Slice(ordered, func(i, j int) bool {
		if ctx.Err() != nil {
			canceled = true

			return false
		}

		if ordered[i].ItemID != ordered[j].ItemID {
			return ordered[i].ItemID < ordered[j].ItemID
		}

		return ordered[i].RevisionID < ordered[j].RevisionID
	})

	if canceled {
		return nil, nil, ctx.Err()
	}

	byRevision := make(map[string]CoverageRecord, len(ordered))
	for index, record := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		if err := validateCoverageRecord(record); err != nil {
			return nil, nil, err
		}

		if index > 0 && sameCoverageIdentity(ordered[index-1], record) {
			return nil, nil, ErrInvalid
		}

		if _, duplicate := byRevision[record.RevisionID]; duplicate {
			return nil, nil, ErrInvalid
		}

		byRevision[record.RevisionID] = record
	}

	return ordered, byRevision, nil
}

func validateCoverageRecord(record CoverageRecord) error {
	if !validIdentity(record.ItemID) || !validIdentity(record.RevisionID) || record.Revision == 0 {
		return ErrInvalid
	}

	if !validHash(record.ContentHash) || record.ChunkCount == 0 {
		return ErrInvalid
	}

	return nil
}

func sameCoverageIdentity(left, right CoverageRecord) bool {
	return left.ItemID == right.ItemID && left.RevisionID == right.RevisionID
}

func orderChunks(ctx context.Context, chunks []Chunk) ([]Chunk, error) {
	ordered := cloneChunks(chunks)
	for index := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sort.Strings(ordered[index].SourceIDs)
		sort.Strings(ordered[index].ConversationIDs)
	}

	canceled := false

	sort.Slice(ordered, func(i, j int) bool {
		if ctx.Err() != nil {
			canceled = true

			return false
		}

		if ordered[i].ItemID != ordered[j].ItemID {
			return ordered[i].ItemID < ordered[j].ItemID
		}

		if ordered[i].RevisionID != ordered[j].RevisionID {
			return ordered[i].RevisionID < ordered[j].RevisionID
		}

		return ordered[i].Ordinal < ordered[j].Ordinal
	})

	if canceled {
		return nil, ctx.Err()
	}

	return ordered, nil
}

func validateChunkCoverage(
	ctx context.Context,
	generation Generation,
	coverageByRevision map[string]CoverageRecord,
	chunks []Chunk,
) error {
	seen := make(map[string]uint32, len(coverageByRevision))
	firstChunk := make(map[string]Chunk, len(coverageByRevision))

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		manifest, found := coverageByRevision[chunk.RevisionID]
		if !chunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) {
			return ErrInvalid
		}

		if err := validateChunk(generation, chunk); err != nil {
			return err
		}

		if first, exists := firstChunk[chunk.RevisionID]; exists {
			if !sameRecordMetadata(first, chunk) {
				return ErrInvalid
			}
		} else {
			firstChunk[chunk.RevisionID] = chunk
		}

		seen[chunk.RevisionID]++
	}

	for revisionID, manifest := range coverageByRevision {
		if err := ctx.Err(); err != nil {
			return err
		}

		if seen[revisionID] != manifest.ChunkCount {
			return ErrInvalid
		}
	}

	return nil
}

//nolint:cyclop // All record fields except ordinal, input hash, and vector must match explicitly.
func sameRecordMetadata(left, right Chunk) bool {
	return left.Scope == right.Scope && left.BuildID == right.BuildID &&
		left.LexicalGenerationID == right.LexicalGenerationID &&
		left.LexicalWatermark == right.LexicalWatermark && left.ModelSpace == right.ModelSpace &&
		left.Dimensions == right.Dimensions && left.ItemID == right.ItemID &&
		left.RevisionID == right.RevisionID && left.Revision == right.Revision &&
		left.ContentHash == right.ContentHash && left.ScopeChangeSeq == right.ScopeChangeSeq &&
		left.Kind == right.Kind && left.Category == right.Category && left.MemoryKey == right.MemoryKey &&
		left.CreatedAt.Equal(right.CreatedAt) && sameOptionalTime(left.ObservedAt, right.ObservedAt) &&
		sameOptionalTime(left.ValidFrom, right.ValidFrom) &&
		sameOptionalTime(left.ValidUntil, right.ValidUntil) && sameOptionalTime(left.ExpiresAt, right.ExpiresAt) &&
		slices.Equal(left.SourceIDs, right.SourceIDs) && slices.Equal(left.ConversationIDs, right.ConversationIDs)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return left.Equal(*right)
}

func chunkMatchesCoverage(chunk Chunk, manifest CoverageRecord, found bool, nextOrdinal uint32) bool {
	return found && chunk.ItemID == manifest.ItemID && chunk.Revision == manifest.Revision &&
		chunk.ContentHash == manifest.ContentHash && chunk.Ordinal == nextOrdinal
}

func validateChunk(generation Generation, chunk Chunk) error {
	if !chunkMatchesGeneration(generation, chunk) || !validChunkIdentity(chunk) {
		return ErrInvalid
	}

	if !validChunkBounds(generation, chunk) {
		return ErrInvalid
	}

	if chunk.Kind != memory.MemoryKindState && chunk.Kind != memory.MemoryKindEvent {
		return ErrInvalid
	}

	if err := validateOptionalMetadata(chunk); err != nil {
		return err
	}

	if err := semanticcore.ValidateVector(chunk.Vector, generation.Dimensions); err != nil {
		return ErrInvalid
	}

	return nil
}

func chunkMatchesGeneration(generation Generation, chunk Chunk) bool {
	return chunk.Scope == generation.Scope && chunk.BuildID == generation.BuildID &&
		chunk.LexicalGenerationID == generation.LexicalGenerationID &&
		chunk.LexicalWatermark == generation.LexicalWatermark &&
		chunk.ModelSpace == generation.ModelSpace && chunk.Dimensions == generation.Dimensions
}

func validChunkIdentity(chunk Chunk) bool {
	return validIdentity(chunk.ItemID) && validIdentity(chunk.RevisionID) && chunk.Revision != 0 &&
		validHash(chunk.ContentHash) && validHash(chunk.InputHash)
}

func validChunkBounds(generation Generation, chunk Chunk) bool {
	return chunk.ScopeChangeSeq != 0 && chunk.ScopeChangeSeq <= generation.LexicalWatermark &&
		!chunk.CreatedAt.IsZero() && len(chunk.SourceIDs) <= maxMetadataIDs &&
		len(chunk.ConversationIDs) <= maxMetadataIDs
}

func validateOptionalMetadata(chunk Chunk) error {
	if chunk.ObservedAt != nil && chunk.ObservedAt.IsZero() {
		return ErrInvalid
	}

	if !validOptionalString(string(chunk.Category)) || !validOptionalString(string(chunk.MemoryKey)) {
		return ErrInvalid
	}

	if !validSortedIdentities(chunk.SourceIDs) || !validSortedIdentities(chunk.ConversationIDs) {
		return ErrInvalid
	}

	if chunk.ValidFrom != nil && chunk.ValidUntil != nil && chunk.ValidUntil.Before(*chunk.ValidFrom) {
		return ErrInvalid
	}

	return nil
}

func validOptionalString(value string) bool {
	return len(value) <= maxIdentityBytes && !strings.ContainsAny(value, "\r\n\t\x00")
}

func validSortedIdentities(values []string) bool {
	for index, value := range values {
		if !validIdentity(value) || (index > 0 && values[index-1] >= value) {
			return false
		}
	}

	return true
}

func coverageFingerprint(coverage []CoverageRecord) string {
	hash := sha256.New()

	var number [8]byte

	for _, record := range coverage {
		writeHashField(hash, record.ItemID)
		writeHashField(hash, record.RevisionID)
		binary.BigEndian.PutUint64(number[:], record.Revision)
		_, _ = hash.Write(number[:])
		writeHashField(hash, record.ContentHash)
		binary.BigEndian.PutUint64(number[:], uint64(record.ChunkCount))
		_, _ = hash.Write(number[:])
	}

	return hex.EncodeToString(hash.Sum(nil))
}

type hashWriter interface {
	Write(data []byte) (written int, err error)
}

func writeHashField(hash hashWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(value))
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}

	return formatTime(*value)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timestamp", ErrCorrupt)
	}

	return parsed, nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	parsed, err := parseTime(value)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func cloneChunks(chunks []Chunk) []Chunk {
	cloned := make([]Chunk, len(chunks))
	for index := range chunks {
		cloned[index] = chunks[index]
		cloned[index].ValidFrom = cloneTime(chunks[index].ValidFrom)
		cloned[index].ObservedAt = cloneTime(chunks[index].ObservedAt)
		cloned[index].ValidUntil = cloneTime(chunks[index].ValidUntil)
		cloned[index].ExpiresAt = cloneTime(chunks[index].ExpiresAt)
		cloned[index].SourceIDs = append([]string(nil), chunks[index].SourceIDs...)
		cloned[index].ConversationIDs = append([]string(nil), chunks[index].ConversationIDs...)
		cloned[index].Vector = append([]float32(nil), chunks[index].Vector...)
	}

	return cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
