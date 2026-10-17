package semantic_test

import (
	"math"
	"strings"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// The persisted v1 encoder is an independent oracle: sizes must match its
// escaping, omitempty, numeric widths and base64 padding exactly.
//
//nolint:cyclop,funlen // Exercise independent encoder outputs across escaping and dimension boundaries.
func TestEncodedChunkBytesMatchesV1Encoder(t *testing.T) {
	values := []string{"", "plain", "<>&\"\\\b\f\n\r\t\x00", "Я中😀\u2028\u2029", "\xff\xfe\xc0\x80"}
	for b := 0; b < 256; b++ {
		values = append(values, string([]byte{byte(b)}))
	}

	at := time.Date(2020, 1, 2, 3, 4, 5, 6789, time.FixedZone("offset", 3600))

	for _, value := range values {
		for _, dimensions := range []int{1, 2, 3, 4096} {
			chunk := semantic.ProjectionChunk{
				Scope: memory.Scope{Kind: memory.ScopeKindPersonal, Key: value}, BuildID: value,
				LexicalGenerationID: value, LexicalWatermark: math.MaxUint64, ModelSpace: value,
				Dimensions: dimensions, ItemID: value, RevisionID: value, Revision: 10,
				Ordinal: math.MaxUint32, ContentHash: value, InputHash: value, ScopeChangeSeq: 100,
				Kind: memory.MemoryKindEvent, Category: memory.AtomCategory(value), MemoryKey: memory.MemoryKey(value),
				CreatedAt: at, ObservedAt: &at, ValidFrom: &at, ValidUntil: &at, ExpiresAt: &at,
				SourceIDs: []string{value, "other"}, ConversationIDs: []string{value},
				Vector: make([]float32, dimensions),
			}

			encoded, err := semantic.EncodeChunk(chunk)
			if err != nil {
				t.Fatal(err)
			}

			chunk.Vector = nil // Search never needs stored components for accounting.

			size, err := semantic.EncodedChunkBytes(chunk)
			if err != nil || size != len(encoded) {
				t.Fatalf("value=%q dimensions=%d size=%d want=%d err=%v", value, dimensions, size, len(encoded), err)
			}
		}
	}

	chunk := semantic.ProjectionChunk{Dimensions: 1, Vector: []float32{1}}

	encoded, err := semantic.EncodeChunk(chunk)
	if err != nil {
		t.Fatal(err)
	}

	size, err := semantic.EncodedChunkBytes(chunk)
	if err != nil || size != len(encoded) {
		t.Fatalf("omitted fields size=%d want=%d err=%v", size, len(encoded), err)
	}
	// Worst-case portable membership expansion remains exact near admission bounds.
	chunk.SourceIDs = make([]string, semantic.MaxMetadataIDs)

	chunk.ConversationIDs = make([]string, semantic.MaxMetadataIDs)
	for i := range chunk.SourceIDs {
		chunk.SourceIDs[i] = strings.Repeat("<", semantic.MaxIdentityBytes)
		chunk.ConversationIDs[i] = chunk.SourceIDs[i]
	}

	encoded, err = semantic.EncodeChunk(chunk)
	if err != nil {
		t.Fatal(err)
	}

	size, err = semantic.EncodedChunkBytes(chunk)
	if err != nil || size != len(encoded) {
		t.Fatalf("maximum memberships size=%d want=%d err=%v", size, len(encoded), err)
	}
}
