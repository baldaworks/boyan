package canonicalcore_test

import canonicalcore "github.com/baldaworks/boyan/internal/canonicalcore"

import (
	"testing"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestNewCanonicalTurnProcessorRequiresAllCutoverPorts(t *testing.T) {
	if _, err := canonicalcore.NewCanonicalTurnProcessor(nil, nil, PolicyRegistry{Version: "policy-v1"}); err == nil {
		t.Fatal("NewCanonicalTurnProcessor() accepted missing ports")
	}

	if _, err := canonicalcore.NewCanonicalTurnProcessor(
		testCanonicalStore{},
		testCanonicalExtractor{},
		PolicyRegistry{},
	); err == nil {
		t.Fatal("NewCanonicalTurnProcessor() accepted empty policy version")
	}
}

type testCanonicalStore struct{ CanonicalStore }
type testCanonicalExtractor struct{ CanonicalSemanticExtractor }
