//go:build integration

//nolint:cyclop,lll,funlen,gocognit // Real SQL callbacks cancel each publication boundary with one shared rollback oracle.
package sqlite

import (
	"context"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
	driver "modernc.org/sqlite"
)

func TestPublicationCancellationRollsBackSealSwitchCopyAndAbort(t *testing.T) {
	for _, operation := range []string{"seal", "switch", "copy", "abort"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			name := fmt.Sprintf("cancel_publication_%d", cancellationFunctionSequence.Add(1))
			if err := driver.RegisterScalarFunction(name, 0, func(*driver.FunctionContext, []sqldriver.Value) (sqldriver.Value, error) {
				cancel()

				return int64(0), nil
			}); err != nil {
				t.Fatal(err)
			}

			store, _ := openTestStore(t, false)
			c := &publicationCheckpoints{}
			record := applyRecord("large")
			record.ConversationIDs = []string{strings.Repeat("c", lexicalPageBytes+1)}

			a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"large": record}}, c)
			if err := a.Apply(t.Context(), lexicalTestScope, "old", nil, []memory.MemoryRevision{{RevisionID: "large"}}); err != nil {
				t.Fatal(err)
			}

			c.active = publicationManifest("old", 1)
			if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
				t.Fatal(err)
			}

			if err := lifecycle.Activate(t.Context(), lexicalTestScope, "old"); err != nil {
				t.Fatal(err)
			}

			var old string
			if err := store.reader.QueryRow(`SELECT physical_build_id FROM lexical_active`).Scan(&old); err != nil {
				t.Fatal(err)
			}

			manifest := publicationManifest("new", 2)

			if operation != "copy" {
				if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "new"); err != nil {
					t.Fatal(err)
				}

				if err := a.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "large"}}); err != nil {
					t.Fatal(err)
				}
			}

			trigger := `BEFORE UPDATE OF state ON lexical_build WHEN NEW.state='committed'`

			switch operation {
			case "switch":
				if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err != nil {
					t.Fatal(err)
				}

				c.active = manifest
				trigger = `BEFORE INSERT ON lexical_active`
			case "copy":
				trigger = `AFTER INSERT ON lexical_conversation`
			case "abort":
				trigger = `BEFORE DELETE ON lexical_record`
			}

			if _, err := store.writer.Exec(`CREATE TRIGGER cancel_operation ` + trigger + ` BEGIN SELECT ` + name + `(); END`); err != nil {
				t.Fatal(err)
			}

			var err error

			switch operation {
			case "seal":
				err = lifecycle.CommitProjectionManifest(ctx, manifest)
			case "switch":
				err = lifecycle.Activate(ctx, lexicalTestScope, "new")
			case "copy":
				err = lifecycle.CommitProjectionManifest(ctx, c.active)
			case "abort":
				err = lifecycle.AbortProjectionSync(ctx, "new")
			}

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s lost cancellation: %v", operation, err)
			}

			var current string
			if err := store.reader.QueryRow(`SELECT physical_build_id FROM lexical_active`).Scan(&current); err != nil || current != old {
				t.Fatalf("cancellation switched active: %s %v", current, err)
			}

			if operation == "copy" || operation == "seal" {
				var count int

				id := a.stages[lexicalTestScope].id
				if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=? AND state='committed'`, id).Scan(&count); err != nil || count != 0 {
					t.Fatalf("canceled work certified: %d %v", count, err)
				}
			}

			if _, err := store.writer.Exec(`DROP TRIGGER cancel_operation`); err != nil {
				t.Fatal(err)
			}

			generation := "new"
			if operation == "copy" {
				generation = "old"
			}

			if err := lifecycle.AbortProjectionSync(t.Context(), generation); err != nil {
				t.Fatal(err)
			}

			if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "retry"); err != nil {
				t.Fatal("canceled operation leaked ownership", err)
			}
		})
	}
}
