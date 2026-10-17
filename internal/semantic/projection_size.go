package semantic

import (
	"encoding/base64"
	"unicode/utf8"
)

// EncodedChunkBytes returns the exact v1 EncodeChunk length for validated
// metadata and a vector of Dimensions float32s. Base64 length is independent of
// component values. It neither allocates vector placeholders nor serializes
// chunk data; callers must validate metadata and actual vectors separately.
//
//nolint:cyclop // Keep v1 field names, omission and numeric-width accounting auditable together.
func EncodedChunkBytes(chunk ProjectionChunk) (int, error) {
	if chunk.Dimensions < 1 || chunk.Dimensions > MaxEmbeddingDimensions {
		return 0, ErrInvalid
	}
	// Opening/closing braces. Each field adds a trailing comma; remove the last.
	size := 1

	for _, field := range []struct {
		name, value string
		optional    bool
	}{
		{"schema_version", ProjectionSchemaVersion, false},
		{"scope_key", chunk.Scope.Key, false}, {"scope_kind", string(chunk.Scope.Kind), false},
		{"build_id", chunk.BuildID, false}, {"lexical_generation_id", chunk.LexicalGenerationID, false},
		{"model_space", chunk.ModelSpace, false}, {"item_id", chunk.ItemID, false},
		{"revision_id", chunk.RevisionID, false}, {"content_hash", chunk.ContentHash, false},
		{"input_hash", chunk.InputHash, false}, {"kind", string(chunk.Kind), false},
		{"category", string(chunk.Category), true}, {"memory_key", string(chunk.MemoryKey), true},
		{"created_at", FormatTime(chunk.CreatedAt), false}, {"observed_at", FormatOptionalTime(chunk.ObservedAt), true},
		{"valid_from", FormatOptionalTime(chunk.ValidFrom), true},
		{"valid_until", FormatOptionalTime(chunk.ValidUntil), true},
		{"expires_at", FormatOptionalTime(chunk.ExpiresAt), true},
	} {
		if !field.optional || field.value != "" {
			size += len(field.name) + len(`"":,`) + jsonStringBytes(field.value)
		}
	}

	for _, field := range []struct {
		name  string
		value uint64
	}{
		{"lexical_watermark", chunk.LexicalWatermark}, {"dimensions", uint64(chunk.Dimensions)},
		{"revision", chunk.Revision}, {"ordinal", uint64(chunk.Ordinal)}, {"scope_change_seq", chunk.ScopeChangeSeq},
	} {
		digits := 1
		for value := field.value; value >= 10; value /= 10 {
			digits++
		}

		size += len(field.name) + len(`"":,`) + digits
	}

	for _, field := range []struct {
		name string
		ids  []string
	}{
		{"source_ids", chunk.SourceIDs}, {"conversation_ids", chunk.ConversationIDs},
	} {
		if len(field.ids) == 0 {
			continue
		}

		size += len(field.name) + len(`"":,`) + 1
		for _, id := range field.ids {
			size += jsonStringBytes(id) + 1
		}
	}

	size += len(`"vector":"",`) + base64.StdEncoding.EncodedLen(chunk.Dimensions*Float32Bytes)

	return size, nil
}

// Match encoding/json's default HTML-safe UTF-8 string encoding, including
// replacement of each invalid UTF-8 byte and escaping of U+2028/U+2029.
//
//nolint:cyclop // Each JSON escaping category must preserve the encoder's exact byte count.
func jsonStringBytes(value string) int {
	size := 2

	for i := 0; i < len(value); {
		b := value[i]
		if b < utf8.RuneSelf {
			switch b {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				size += 2
			case '<', '>', '&':
				size += 6
			default:
				if b < ' ' {
					size += 6
				} else {
					size++
				}
			}

			i++

			continue
		}

		r, width := utf8.DecodeRuneInString(value[i:])
		if (r == utf8.RuneError && width == 1) || r == '\u2028' || r == '\u2029' {
			size += 6
		} else {
			size += width
		}

		i += width
	}

	return size
}
