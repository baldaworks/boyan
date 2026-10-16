package semantic

import (
	"context"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	"time"
)

type hashWriter = interface {
	Write(data []byte) (written int, err error)
}

func CheckProjectionBounds(chunkCount int, encodedBytes uint64) error {
	return semanticcore.CheckProjectionBounds(chunkCount, encodedBytes)
}

func validateGeneration(generation Generation) error {
	return semanticcore.ValidateGeneration(generation)
}

func validateView(view View) error { return semanticcore.ValidateView(view) }

func validateScope(scope memory.Scope) error { return semanticcore.ValidateScope(scope) }

func validIdentity(value string) bool { return semanticcore.ValidProjectionIdentity(value) }

func validHash(value string) bool { return semanticcore.ValidHash(value) }

func validateAndOrder(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) ([]CoverageRecord, []Chunk, string, error) {
	return semanticcore.ValidateAndOrder(ctx, generation, coverage, chunks)
}

func orderCoverage(
	ctx context.Context,
	coverage []CoverageRecord,
) ([]CoverageRecord, map[string]CoverageRecord, error) {
	return semanticcore.OrderCoverage(ctx, coverage)
}

func validateCoverageRecord(record CoverageRecord) error {
	return semanticcore.ValidateCoverageRecord(record)
}

func sameCoverageIdentity(left, right CoverageRecord) bool {
	return semanticcore.SameCoverageIdentity(left, right)
}

func orderChunks(ctx context.Context, chunks []Chunk) ([]Chunk, error) {
	return semanticcore.OrderChunks(ctx, chunks)
}

func validateChunkCoverage(
	ctx context.Context,
	generation Generation,
	coverageByRevision map[string]CoverageRecord,
	chunks []Chunk,
) error {
	return semanticcore.ValidateChunkCoverage(ctx, generation, coverageByRevision, chunks)
}

func sameRecordMetadata(left, right Chunk) bool { return semanticcore.SameRecordMetadata(left, right) }

func sameOptionalTime(left, right *time.Time) bool { return semanticcore.SameOptionalTime(left, right) }

func chunkMatchesCoverage(chunk Chunk, manifest CoverageRecord, found bool, nextOrdinal uint32) bool {
	return semanticcore.ChunkMatchesCoverage(chunk, manifest, found, nextOrdinal)
}

func validateChunk(generation Generation, chunk Chunk) error {
	return semanticcore.ValidateChunk(generation, chunk)
}

func chunkMatchesGeneration(generation Generation, chunk Chunk) bool {
	return semanticcore.ChunkMatchesGeneration(generation, chunk)
}

func validChunkIdentity(chunk Chunk) bool { return semanticcore.ValidChunkIdentity(chunk) }

func validChunkBounds(generation Generation, chunk Chunk) bool {
	return semanticcore.ValidChunkBounds(generation, chunk)
}

func validateOptionalMetadata(chunk Chunk) error { return semanticcore.ValidateOptionalMetadata(chunk) }

func validOptionalString(value string) bool { return semanticcore.ValidOptionalString(value) }

func validSortedIdentities(values []string) bool { return semanticcore.ValidSortedIdentities(values) }

func coverageFingerprint(coverage []CoverageRecord) string {
	return semanticcore.CoverageFingerprint(coverage)
}

func writeHashField(hash hashWriter, value string) {
	semanticcore.WriteHashField(hash, value)
}

func formatTime(value time.Time) string { return semanticcore.FormatTime(value) }

func formatOptionalTime(value *time.Time) string { return semanticcore.FormatOptionalTime(value) }

func parseTime(value string) (time.Time, error) { return semanticcore.ParseTime(value) }

func parseOptionalTime(value string) (*time.Time, error) {
	return semanticcore.ParseOptionalTime(value)
}

func cloneChunks(chunks []Chunk) []Chunk { return semanticcore.CloneChunks(chunks) }

func cloneTime(value *time.Time) *time.Time { return semanticcore.CloneTime(value) }

const (
	maxIdentityBytes = semanticcore.MaxIdentityBytes
	maxMetadataIDs   = semanticcore.MaxMetadataIDs
	maxScopeKeyBytes = semanticcore.MaxScopeKeyBytes
)
