package semantic

import (
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

const (
	// PreprocessingVersion invalidates vectors when text preparation changes.
	PreprocessingVersion = "boyan-semantic-nfc-window-v1"
	// NormalizationVersion identifies the vector normalization contract.
	NormalizationVersion = "unit-float32-v1"

	WindowRunes        = 384
	WindowOverlapRunes = 64
	PassageWindowLimit = 128
	QueryWindowLimit   = 4

	MaxRecordBytes        = memory.MaxDerivedTextBytes
	MaxQueryBytes         = memory.MaxSearchQueryBytes
	MaxPrefixBytes        = 256
	ProviderInputMaxBytes = 2048

	EmbeddingBatchLimit       = 16
	ProjectionChunkLimit      = 8192
	ProjectionScanRecordLimit = 8192
	ProjectionByteLimit       = 64 << 20
	SemanticCandidateLimit    = 100
	FusedCandidateLimit       = 200
	FusionRankConstant        = 60
	MinimumCandidateLimit     = 20
	CandidateLimitFactor      = 4
	SemanticWorkerLimit       = 2
	SemanticQueueLimit        = 64
	SemanticBuildTimeout      = 15 * time.Minute
	MaxModelIdentityBytes     = 256
	MaxEmbeddingDimensions    = 4096
	VectorUnitTolerance       = 0.001
)
