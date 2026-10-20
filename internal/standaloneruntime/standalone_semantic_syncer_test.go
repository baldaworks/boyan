package standaloneruntime //nolint:testpackage // Exercises lexical failure handling with fake ports.

import (
	"context"
	"errors"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

type failingLexicalSyncer struct {
	err    error
	during func()
}

func (syncer failingLexicalSyncer) Sync(context.Context, memory.Scope) error {
	if syncer.during != nil {
		syncer.during()
	}

	return syncer.err
}

func TestSemanticRefreshRequiresLexicalSuccess(t *testing.T) {
	failure := errors.New("lexical failed")
	syncer := semanticSyncer{lexical: failingLexicalSyncer{err: failure}}

	err := syncer.Sync(context.Background(), memory.Scope{Kind: memory.ScopeKindPersonal, Key: "lexical-failure"})
	if !errors.Is(err, failure) {
		t.Fatalf("lexical error=%v", err)
	}
}
