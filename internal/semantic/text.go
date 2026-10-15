package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// PreparedText is normalized provider input plus explicit source coverage.
type PreparedText struct {
	Windows        []PreparedWindow
	TotalRunes     int
	OmittedRunes   int
	OmittedWindows int
}

// PreparedWindow binds one provider input to its normalized rune span and hash.
type PreparedWindow struct {
	StartRune   int
	EndRune     int
	Input       string
	ContentHash string
}

// PreparePassage prepares a complete canonical-record passage projection.
func PreparePassage(ctx context.Context, text, prefix string) (PreparedText, error) {
	prepared, err := prepareText(ctx, text, prefix, MaxRecordBytes, PassageWindowLimit)
	if err != nil {
		return PreparedText{}, err
	}

	if prepared.OmittedRunes != 0 || prepared.OmittedWindows != 0 {
		return PreparedText{}, newError(ReasonProjectionCapacity)
	}

	return prepared, nil
}

// PrepareQuery prepares at most four query windows and reports omitted coverage.
func PrepareQuery(ctx context.Context, text, prefix string) (PreparedText, error) {
	return prepareText(ctx, text, prefix, MaxQueryBytes, QueryWindowLimit)
}

func prepareText(
	ctx context.Context,
	text string,
	prefix string,
	maxTextBytes int,
	maxWindows int,
) (PreparedText, error) {
	normalized, err := normalizeTextInput(ctx, text, prefix, maxTextBytes)
	if err != nil {
		return PreparedText{}, err
	}

	return prepareWindows(ctx, []rune(normalized), prefix, maxWindows)
}

func normalizeTextInput(ctx context.Context, text, prefix string, maxTextBytes int) (string, error) {
	if ctx == nil || !utf8.ValidString(text) || !utf8.ValidString(prefix) || strings.ContainsRune(prefix, '\x00') {
		return "", newError(ReasonInvalidInput)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if len(text) > maxTextBytes || len(prefix) > MaxPrefixBytes {
		return "", newError(ReasonInputLimit)
	}

	normalized := norm.NFC.String(strings.ReplaceAll(text, "\r\n", "\n"))

	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return "", newError(ReasonInvalidInput)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return normalized, nil
}

func prepareWindows(ctx context.Context, runes []rune, prefix string, maxWindows int) (PreparedText, error) {
	prepared := PreparedText{
		Windows:    make([]PreparedWindow, 0, min(maxWindows, windowCountUpperBound(len(runes)))),
		TotalRunes: len(runes),
	}
	covered := 0

	for start := 0; start < len(runes); {
		if err := ctx.Err(); err != nil {
			return PreparedText{}, err
		}

		end := windowEnd(runes, start)
		chunk := strings.TrimSpace(string(runes[start:end]))

		if chunk != "" {
			if len(prepared.Windows) >= maxWindows {
				prepared.OmittedWindows++
			} else if err := appendPreparedWindow(&prepared, prefix, chunk, start, end); err != nil {
				return PreparedText{}, err
			} else {
				covered = end
			}
		}

		if end == len(runes) {
			break
		}

		start = max(start+1, end-WindowOverlapRunes)
	}

	prepared.OmittedRunes = max(0, len(runes)-covered)

	return prepared, nil
}

func appendPreparedWindow(prepared *PreparedText, prefix, chunk string, start, end int) error {
	input := prefix + chunk
	if len(input) > ProviderInputMaxBytes {
		return newError(ReasonInputLimit)
	}

	hash := sha256.Sum256([]byte(input))
	prepared.Windows = append(prepared.Windows, PreparedWindow{
		StartRune: start, EndRune: end, Input: input,
		ContentHash: hex.EncodeToString(hash[:]),
	})

	return nil
}

func windowEnd(runes []rune, start int) int {
	end := min(start+WindowRunes, len(runes))
	if end == len(runes) {
		return end
	}

	for index := end - 1; index >= end-WindowOverlapRunes; index-- {
		if unicode.IsSpace(runes[index]) {
			return index + 1
		}
	}

	return end
}

func windowCountUpperBound(runes int) int {
	if runes <= WindowRunes {
		return 1
	}

	return 1 + (runes-WindowRunes+WindowRunes-WindowOverlapRunes-1)/(WindowRunes-WindowOverlapRunes)
}
