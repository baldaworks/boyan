package semantic_test

import (
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/semantic"
)

func TestSpaceFingerprintIsCanonicalAndDeterministic(t *testing.T) {
	t.Parallel()

	space := semantic.ModelSpace{
		Model: "model", Revision: "revision", Dimensions: 3,
		QueryPrefix: "query: ", PassagePrefix: "passage: ",
	}

	const want = "0384e2ee7b8bbc549c1a607f6adc79e5306302983a3f1979341a89e073d1e5c4"

	for range 100 {
		got, err := semantic.SpaceFingerprint(space)
		if err != nil {
			t.Fatalf("SpaceFingerprint() error = %v", err)
		}

		if got != want {
			t.Fatalf("SpaceFingerprint() = %q, want %q", got, want)
		}
	}
}

func TestSpaceFingerprintChangesForEveryModelSpaceField(t *testing.T) {
	t.Parallel()

	base := semantic.ModelSpace{
		Model: "model", Revision: "revision", Dimensions: 3,
		QueryPrefix: "query: ", PassagePrefix: "passage: ",
	}

	baseFingerprint, err := semantic.SpaceFingerprint(base)
	if err != nil {
		t.Fatalf("SpaceFingerprint(base) error = %v", err)
	}

	mutations := map[string]func(*semantic.ModelSpace){
		"model":          func(space *semantic.ModelSpace) { space.Model = "model-2" },
		"revision":       func(space *semantic.ModelSpace) { space.Revision = "revision-2" },
		"dimensions":     func(space *semantic.ModelSpace) { space.Dimensions++ },
		"query prefix":   func(space *semantic.ModelSpace) { space.QueryPrefix = "search: " },
		"passage prefix": func(space *semantic.ModelSpace) { space.PassagePrefix = "document: " },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			changed := base
			mutate(&changed)

			got, fingerprintErr := semantic.SpaceFingerprint(changed)
			if fingerprintErr != nil {
				t.Fatalf("SpaceFingerprint() error = %v", fingerprintErr)
			}

			if got == baseFingerprint {
				t.Fatalf("SpaceFingerprint() unchanged after %s mutation", name)
			}
		})
	}
}

func TestSpaceFingerprintRejectsInvalidModelSpace(t *testing.T) {
	t.Parallel()

	valid := semantic.ModelSpace{Model: "model", Revision: "revision", Dimensions: 3}

	tests := []struct {
		name   string
		mutate func(*semantic.ModelSpace)
	}{
		{name: "empty model", mutate: func(space *semantic.ModelSpace) { space.Model = "" }},
		{name: "model over limit", mutate: func(space *semantic.ModelSpace) {
			space.Model = strings.Repeat("m", semantic.MaxModelIdentityBytes+1)
		}},
		{name: "invalid revision UTF-8", mutate: func(space *semantic.ModelSpace) { space.Revision = "bad\xff" }},
		{name: "control in revision", mutate: func(space *semantic.ModelSpace) { space.Revision = "bad\nrevision" }},
		{name: "zero dimensions", mutate: func(space *semantic.ModelSpace) { space.Dimensions = 0 }},
		{name: "dimensions over limit", mutate: func(space *semantic.ModelSpace) {
			space.Dimensions = semantic.MaxEmbeddingDimensions + 1
		}},
		{name: "query prefix over limit", mutate: func(space *semantic.ModelSpace) {
			space.QueryPrefix = strings.Repeat("q", semantic.MaxPrefixBytes+1)
		}},
		{name: "passage prefix NUL", mutate: func(space *semantic.ModelSpace) { space.PassagePrefix = "passage:\x00" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			space := valid
			test.mutate(&space)
			_, fingerprintErr := semantic.SpaceFingerprint(space)
			assertReason(t, fingerprintErr, semantic.ReasonInvalidInput)
		})
	}
}
