package storefixture_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicReaderLargeSourceBatch(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, _ mutationStore, p mutationProbe) {
		scope := memory.Scope{Key: "test:" + strings.Repeat("s", 65530), Kind: memory.ScopeKindPersonal}
		source := strings.Repeat("a", 65535)

		for i := range 300 {
			id := fmt.Sprintf("revision:%03d", i)
			seedReaderSourceEdge(t, p, scope, source, id)
		}

		longID := strings.Repeat("z", 65535)
		seedReaderSourceEdge(t, p, scope, source, longID)
		seedReaderSourceEdge(t, p, scope, source[:65534]+"b", "foreign")

		store := f.Store.(activeReaderStore)

		ids, next, err := store.SourceRevisionBatch(t.Context(), scope, source, "", 512)
		if err != nil || len(ids) != 301 || ids[0] != "revision:000" || ids[299] != "revision:299" || next != longID {
			t.Fatalf("large batch count=%d next bytes=%d err=%v", len(ids), len(next), err)
		}

		ids, next, err = store.SourceRevisionBatch(t.Context(), scope, source, longID, 1)
		if err != nil || len(ids) != 0 || next != "" {
			t.Fatalf("terminal source page=%v next=%q err=%v", ids, next, err)
		}
	})
}

func seedReaderSourceEdge(t *testing.T, p mutationProbe, scope memory.Scope, source, id string) {
	t.Helper()
	p.write(t, canonicalsql.Key{Family: canonicalsql.SourceRevision, Scope: scope,
		Parts: [][]byte{[]byte(source), []byte(id)}},
		map[string]string{"parent_revision_id": source, "child_revision_id": id})
}
