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

	"github.com/baldaworks/boyan/pkg/memory"
)

const (
	MaxIdentityBytes = 512
	MaxMetadataIDs   = 256
	MaxScopeKeyBytes = 4096
)

func CheckProjectionBounds(chunkCount int, encodedBytes uint64) error {
	if chunkCount < 0 || chunkCount > ProjectionChunkLimit ||
		encodedBytes > uint64(ProjectionByteLimit) {
		return ErrCapacity
	}

	return nil
}

func ValidateGeneration(generation Generation) error {
	if err := ValidateView(generation.View()); err != nil {
		return ErrInvalid
	}

	if !ValidProjectionIdentity(generation.BuildID) || generation.ReadyAt.IsZero() {
		return ErrInvalid
	}

	return nil
}

func ValidateView(view View) error {
	if err := ValidateScope(view.Scope); err != nil {
		return err
	}

	if !ValidProjectionIdentity(view.LexicalGenerationID) || !ValidHash(view.ModelSpace) {
		return ErrInvalid
	}

	if view.Dimensions < 1 || view.Dimensions > MaxEmbeddingDimensions {
		return ErrInvalid
	}

	return nil
}

func ValidateScope(scope memory.Scope) error {
	if len(scope.Key) > MaxScopeKeyBytes {
		return ErrInvalid
	}

	if err := scope.Validate(); err != nil {
		return ErrInvalid
	}

	return nil
}

func ValidProjectionIdentity(value string) bool {
	return value != "" && len(value) <= MaxIdentityBytes && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\r\n\t\x00")
}

func ValidHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func ValidateAndOrder(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []ProjectionChunk,
) ([]CoverageRecord, []ProjectionChunk, string, error) {
	orderedCoverage, coverageByRevision, err := OrderCoverage(ctx, coverage)
	if err != nil {
		return nil, nil, "", err
	}

	orderedChunks, err := OrderChunks(ctx, chunks)
	if err != nil {
		return nil, nil, "", err
	}

	if err := ValidateChunkCoverage(ctx, generation, coverageByRevision, orderedChunks); err != nil {
		return nil, nil, "", err
	}

	return orderedCoverage, orderedChunks, CoverageFingerprint(orderedCoverage), nil
}

func OrderCoverage(
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

		if err := ValidateCoverageRecord(record); err != nil {
			return nil, nil, err
		}

		if index > 0 && SameCoverageIdentity(ordered[index-1], record) {
			return nil, nil, ErrInvalid
		}

		if _, duplicate := byRevision[record.RevisionID]; duplicate {
			return nil, nil, ErrInvalid
		}

		byRevision[record.RevisionID] = record
	}

	return ordered, byRevision, nil
}

func ValidateCoverageRecord(record CoverageRecord) error {
	if !ValidProjectionIdentity(record.ItemID) || !ValidProjectionIdentity(record.RevisionID) || record.Revision == 0 {
		return ErrInvalid
	}

	if !ValidHash(record.ContentHash) || record.ChunkCount == 0 {
		return ErrInvalid
	}

	return nil
}

func SameCoverageIdentity(left, right CoverageRecord) bool {
	return left.ItemID == right.ItemID && left.RevisionID == right.RevisionID
}

func OrderChunks(ctx context.Context, chunks []ProjectionChunk) ([]ProjectionChunk, error) {
	ordered := CloneChunks(chunks)
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

func ValidateChunkCoverage(
	ctx context.Context,
	generation Generation,
	coverageByRevision map[string]CoverageRecord,
	chunks []ProjectionChunk,
) error {
	seen := make(map[string]uint32, len(coverageByRevision))
	firstChunk := make(map[string]ProjectionChunk, len(coverageByRevision))

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		manifest, found := coverageByRevision[chunk.RevisionID]
		if !ChunkMatchesCoverage(chunk, manifest, found, seen[chunk.RevisionID]) {
			return ErrInvalid
		}

		if err := ValidateChunk(generation, chunk); err != nil {
			return err
		}

		if first, exists := firstChunk[chunk.RevisionID]; exists {
			if !SameRecordMetadata(first, chunk) {
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
func SameRecordMetadata(left, right ProjectionChunk) bool {
	return left.Scope == right.Scope && left.BuildID == right.BuildID &&
		left.LexicalGenerationID == right.LexicalGenerationID &&
		left.LexicalWatermark == right.LexicalWatermark && left.ModelSpace == right.ModelSpace &&
		left.Dimensions == right.Dimensions && left.ItemID == right.ItemID &&
		left.RevisionID == right.RevisionID && left.Revision == right.Revision &&
		left.ContentHash == right.ContentHash && left.ScopeChangeSeq == right.ScopeChangeSeq &&
		left.Kind == right.Kind && left.Category == right.Category && left.MemoryKey == right.MemoryKey &&
		left.CreatedAt.Equal(right.CreatedAt) && SameOptionalTime(left.ObservedAt, right.ObservedAt) &&
		SameOptionalTime(left.ValidFrom, right.ValidFrom) &&
		SameOptionalTime(left.ValidUntil, right.ValidUntil) && SameOptionalTime(left.ExpiresAt, right.ExpiresAt) &&
		slices.Equal(left.SourceIDs, right.SourceIDs) && slices.Equal(left.ConversationIDs, right.ConversationIDs)
}

func SameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return left.Equal(*right)
}

func ChunkMatchesCoverage(chunk ProjectionChunk, manifest CoverageRecord, found bool, nextOrdinal uint32) bool {
	return found && chunk.ItemID == manifest.ItemID && chunk.Revision == manifest.Revision &&
		chunk.ContentHash == manifest.ContentHash && chunk.Ordinal == nextOrdinal
}

func ValidateChunk(generation Generation, chunk ProjectionChunk) error {
	if !ChunkMatchesGeneration(generation, chunk) || !ValidChunkIdentity(chunk) {
		return ErrInvalid
	}

	if !ValidChunkBounds(generation, chunk) {
		return ErrInvalid
	}

	if chunk.Kind != memory.MemoryKindState && chunk.Kind != memory.MemoryKindEvent {
		return ErrInvalid
	}

	if err := ValidateOptionalMetadata(chunk); err != nil {
		return err
	}

	if err := ValidateVector(chunk.Vector, generation.Dimensions); err != nil {
		return ErrInvalid
	}

	return nil
}

func ChunkMatchesGeneration(generation Generation, chunk ProjectionChunk) bool {
	return chunk.Scope == generation.Scope && chunk.BuildID == generation.BuildID &&
		chunk.LexicalGenerationID == generation.LexicalGenerationID &&
		chunk.LexicalWatermark == generation.LexicalWatermark &&
		chunk.ModelSpace == generation.ModelSpace && chunk.Dimensions == generation.Dimensions
}

func ValidChunkIdentity(chunk ProjectionChunk) bool {
	return ValidProjectionIdentity(chunk.ItemID) && ValidProjectionIdentity(chunk.RevisionID) && chunk.Revision != 0 &&
		ValidHash(chunk.ContentHash) && ValidHash(chunk.InputHash)
}

func ValidChunkBounds(generation Generation, chunk ProjectionChunk) bool {
	return chunk.ScopeChangeSeq != 0 && chunk.ScopeChangeSeq <= generation.LexicalWatermark &&
		!chunk.CreatedAt.IsZero() && len(chunk.SourceIDs) <= MaxMetadataIDs &&
		len(chunk.ConversationIDs) <= MaxMetadataIDs
}

func ValidateOptionalMetadata(chunk ProjectionChunk) error {
	if chunk.ObservedAt != nil && chunk.ObservedAt.IsZero() {
		return ErrInvalid
	}

	if !ValidOptionalString(string(chunk.Category)) || !ValidOptionalString(string(chunk.MemoryKey)) {
		return ErrInvalid
	}

	if !ValidSortedIdentities(chunk.SourceIDs) || !ValidSortedIdentities(chunk.ConversationIDs) {
		return ErrInvalid
	}

	if chunk.ValidFrom != nil && chunk.ValidUntil != nil && chunk.ValidUntil.Before(*chunk.ValidFrom) {
		return ErrInvalid
	}

	return nil
}

func ValidOptionalString(value string) bool {
	return len(value) <= MaxIdentityBytes && !strings.ContainsAny(value, "\r\n\t\x00")
}

func ValidSortedIdentities(values []string) bool {
	for index, value := range values {
		if !ValidProjectionIdentity(value) || (index > 0 && values[index-1] >= value) {
			return false
		}
	}

	return true
}

func CoverageFingerprint(coverage []CoverageRecord) string {
	hash := sha256.New()

	var number [8]byte

	for _, record := range coverage {
		WriteHashField(hash, record.ItemID)
		WriteHashField(hash, record.RevisionID)
		binary.BigEndian.PutUint64(number[:], record.Revision)
		_, _ = hash.Write(number[:])
		WriteHashField(hash, record.ContentHash)
		binary.BigEndian.PutUint64(number[:], uint64(record.ChunkCount))
		_, _ = hash.Write(number[:])
	}

	return hex.EncodeToString(hash.Sum(nil))
}

type projectionHashWriter interface {
	Write(data []byte) (written int, err error)
}

func WriteHashField(hash projectionHashWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(value))
}

func FormatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func FormatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}

	return FormatTime(*value)
}

func ParseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timestamp", ErrCorrupt)
	}

	return parsed, nil
}

func ParseOptionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	parsed, err := ParseTime(value)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func CloneChunks(chunks []ProjectionChunk) []ProjectionChunk {
	cloned := make([]ProjectionChunk, len(chunks))
	for index := range chunks {
		cloned[index] = chunks[index]
		cloned[index].ValidFrom = CloneTime(chunks[index].ValidFrom)
		cloned[index].ObservedAt = CloneTime(chunks[index].ObservedAt)
		cloned[index].ValidUntil = CloneTime(chunks[index].ValidUntil)
		cloned[index].ExpiresAt = CloneTime(chunks[index].ExpiresAt)
		cloned[index].SourceIDs = append([]string(nil), chunks[index].SourceIDs...)
		cloned[index].ConversationIDs = append([]string(nil), chunks[index].ConversationIDs...)
		cloned[index].Vector = append([]float32(nil), chunks[index].Vector...)
	}

	return cloned
}

func CloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
