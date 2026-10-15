package semantic_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/baldaworks/boyan/internal/semantic"
)

func TestPreparePassageNormalizesTextAndHashesPreparedInput(t *testing.T) {
	t.Parallel()

	prepared, err := semantic.PreparePassage(context.Background(), "  Cafe\u0301\r\nПривет 😀  ", "passage: ")
	if err != nil {
		t.Fatalf("PreparePassage() error = %v", err)
	}

	if prepared.TotalRunes != 13 || prepared.OmittedRunes != 0 || prepared.OmittedWindows != 0 {
		t.Fatalf("PreparePassage() coverage = %+v", prepared)
	}

	if len(prepared.Windows) != 1 {
		t.Fatalf("PreparePassage() windows = %d, want 1", len(prepared.Windows))
	}

	window := prepared.Windows[0]
	if window.StartRune != 0 || window.EndRune != 13 {
		t.Fatalf("PreparePassage() span = [%d,%d), want [0,13)", window.StartRune, window.EndRune)
	}

	if window.Input != "passage: Café\nПривет 😀" {
		t.Fatalf("PreparePassage() input = %q", window.Input)
	}

	if window.ContentHash != "01d02270ee0b7df266232c62dc7095fd25526b5c0804a8d48d7ea90bc080717f" {
		t.Fatalf("PreparePassage() hash = %q", window.ContentHash)
	}
}

func TestPreparePassageSplitsOnRunesWithDeterministicOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rune rune
	}{
		{name: "ASCII", rune: 'a'},
		{name: "Russian", rune: 'я'},
		{name: "four-byte", rune: '😀'},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertRuneWindows(t, test.rune)
		})
	}
}

func assertRuneWindows(t *testing.T, character rune) {
	t.Helper()

	text := strings.Repeat(string(character), semantic.WindowRunes+1)

	prepared, err := semantic.PreparePassage(context.Background(), text, "passage: ")
	if err != nil {
		t.Fatalf("PreparePassage() error = %v", err)
	}

	if len(prepared.Windows) != 2 {
		t.Fatalf("PreparePassage() windows = %d, want 2", len(prepared.Windows))
	}

	if got := prepared.Windows[0]; got.StartRune != 0 || got.EndRune != semantic.WindowRunes {
		t.Fatalf("first span = [%d,%d)", got.StartRune, got.EndRune)
	}

	second := prepared.Windows[1]

	wantStart := semantic.WindowRunes - semantic.WindowOverlapRunes
	if second.StartRune != wantStart || second.EndRune != semantic.WindowRunes+1 {
		t.Fatalf("second span = [%d,%d)", second.StartRune, second.EndRune)
	}

	for _, window := range prepared.Windows {
		if !utf8.ValidString(window.Input) || len(window.Input) > semantic.ProviderInputMaxBytes {
			t.Fatalf("invalid provider input: bytes=%d valid=%t", len(window.Input), utf8.ValidString(window.Input))
		}
	}
}

func TestPreparePassageUsesStableWhitespaceBoundary(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("a", 320) + " " + strings.Repeat("b", 80)

	prepared, err := semantic.PreparePassage(context.Background(), text, "")
	if err != nil {
		t.Fatalf("PreparePassage() error = %v", err)
	}

	if len(prepared.Windows) != 2 {
		t.Fatalf("PreparePassage() windows = %d, want 2", len(prepared.Windows))
	}

	if got := prepared.Windows[0]; got.EndRune != 321 {
		t.Fatalf("first end = %d, want 321", got.EndRune)
	}

	if got := prepared.Windows[1]; got.StartRune != 257 || got.EndRune != 401 {
		t.Fatalf("second span = [%d,%d), want [257,401)", got.StartRune, got.EndRune)
	}
}

func TestPreparePassageCoversEveryValidRecordByte(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("a", semantic.MaxRecordBytes)

	prepared, err := semantic.PreparePassage(context.Background(), text, strings.Repeat("p", semantic.MaxPrefixBytes))
	if err != nil {
		t.Fatalf("PreparePassage() error = %v", err)
	}

	if len(prepared.Windows) > semantic.PassageWindowLimit {
		t.Fatalf("PreparePassage() windows = %d, cap = %d", len(prepared.Windows), semantic.PassageWindowLimit)
	}

	lastEnd := prepared.Windows[len(prepared.Windows)-1].EndRune
	if prepared.OmittedRunes != 0 || prepared.OmittedWindows != 0 || lastEnd != semantic.MaxRecordBytes {
		t.Fatalf("PreparePassage() incomplete coverage = %+v", prepared)
	}

	for _, window := range prepared.Windows {
		if len(window.Input) > semantic.ProviderInputMaxBytes {
			t.Fatalf("prepared input bytes = %d, max = %d", len(window.Input), semantic.ProviderInputMaxBytes)
		}
	}
}

func TestPreparePassageCoversWorstCaseValidRecordAtExactWindowCap(t *testing.T) {
	t.Parallel()

	runes := []rune(strings.Repeat("a", semantic.MaxRecordBytes))
	for index := 320; index < len(runes)-1; index += 257 {
		runes[index] = ' '
	}

	prepared, err := semantic.PreparePassage(context.Background(), string(runes), "passage: ")
	if err != nil {
		t.Fatalf("PreparePassage() error = %v", err)
	}

	if len(prepared.Windows) != semantic.PassageWindowLimit {
		t.Fatalf("PreparePassage() windows = %d, want exact cap %d", len(prepared.Windows), semantic.PassageWindowLimit)
	}

	if prepared.OmittedRunes != 0 || prepared.Windows[len(prepared.Windows)-1].EndRune != semantic.MaxRecordBytes {
		t.Fatalf("PreparePassage() coverage = %+v", prepared)
	}
}

func TestPreparePassageRejectsInvalidAndOverLimitInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		prefix string
		code   semantic.Reason
	}{
		{name: "empty", text: " \r\n ", code: semantic.ReasonInvalidInput},
		{name: "invalid text UTF-8", text: "bad\xff", code: semantic.ReasonInvalidInput},
		{name: "invalid prefix UTF-8", text: "ok", prefix: "bad\xff", code: semantic.ReasonInvalidInput},
		{name: "text one byte over", text: strings.Repeat("a", semantic.MaxRecordBytes+1), code: semantic.ReasonInputLimit},
		{
			name: "prefix one byte over", text: "ok",
			prefix: strings.Repeat("p", semantic.MaxPrefixBytes+1), code: semantic.ReasonInputLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := semantic.PreparePassage(context.Background(), test.text, test.prefix)
			assertReason(t, err, test.code)
		})
	}
}

func TestPrepareQueryReportsOmittedCoverage(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("я", semantic.MaxQueryBytes/2)

	prepared, err := semantic.PrepareQuery(context.Background(), text, "query: ")
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}

	if len(prepared.Windows) != semantic.QueryWindowLimit {
		t.Fatalf("PrepareQuery() windows = %d, want %d", len(prepared.Windows), semantic.QueryWindowLimit)
	}

	if prepared.OmittedRunes != 704 || prepared.OmittedWindows != 3 {
		t.Fatalf("PrepareQuery() omission = %d runes/%d windows, want 704/3", prepared.OmittedRunes, prepared.OmittedWindows)
	}

	if prepared.TotalRunes != semantic.MaxQueryBytes/2 {
		t.Fatalf("PrepareQuery() total runes = %d", prepared.TotalRunes)
	}
}

func TestPrepareQueryReportsExactAndOneOverWindowCoverage(t *testing.T) {
	t.Parallel()

	const progress = semantic.WindowRunes - semantic.WindowOverlapRunes

	const exactRunes = semantic.WindowRunes + (semantic.QueryWindowLimit-1)*progress

	exact, err := semantic.PrepareQuery(context.Background(), strings.Repeat("a", exactRunes), "")
	if err != nil {
		t.Fatalf("PrepareQuery(exact) error = %v", err)
	}

	if len(exact.Windows) != semantic.QueryWindowLimit || exact.OmittedRunes != 0 || exact.OmittedWindows != 0 {
		t.Fatalf("PrepareQuery(exact) = %+v", exact)
	}

	oneOver, err := semantic.PrepareQuery(context.Background(), strings.Repeat("a", exactRunes+1), "")
	if err != nil {
		t.Fatalf("PrepareQuery(one over) error = %v", err)
	}

	if len(oneOver.Windows) != semantic.QueryWindowLimit || oneOver.OmittedRunes != 1 || oneOver.OmittedWindows != 1 {
		t.Fatalf("PrepareQuery(one over) = %+v", oneOver)
	}
}

func TestPrepareQueryHonorsByteLimitAndCancellation(t *testing.T) {
	t.Parallel()

	_, err := semantic.PrepareQuery(context.Background(), strings.Repeat("a", semantic.MaxQueryBytes+1), "")
	assertReason(t, err, semantic.ReasonInputLimit)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = semantic.PrepareQuery(ctx, "query", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareQuery() error = %v, want context.Canceled", err)
	}
}

func assertReason(t *testing.T, err error, want semantic.Reason) {
	t.Helper()

	got, ok := semantic.Classify(err)
	if !ok || got != want {
		t.Fatalf("Classify(%v) = %q, %t; want %q, true", err, got, ok, want)
	}
}
