package canonicalsql

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func rewriteSourceControl(t *testing.T, owner *Owner, table string, alter func(*restoreControl)) {
	t.Helper()

	if err := owner.connection.run(t.Context(), true, false, func(tx *Tx) error {
		control, err := tx.readRestoreControl(t.Context(), table)
		if err != nil {
			return err
		}

		if control == nil {
			t.Fatal("missing test control")
		}

		alter(control)

		data, err := json.Marshal(control)
		if err != nil {
			return err
		}

		_, err = tx.q.exec(t.Context(), "UPDATE "+tx.connection.table(table)+" SET control=$1", data)

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSourceLegacyCompletedChainRequiresFullBase(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		sourceMarker(t, source, scope)
		full, base := sourceBackup(t, source, 0)

		destination, reopen := restoreSourceDestination(t, source)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		rewriteSourceControl(t, destination, "restore_chain", func(c *restoreControl) {
			c.Version, c.ActiveGeneration, c.SourceState = 1, "", ""
		})

		if err := destination.Close(); err != nil {
			t.Fatal(err)
		}

		destination = reopen()
		if err := destination.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal("legacy completed chain blocked ordinary use:", err)
		}

		sourceOperation(t, source, scope, "native-next", 0)

		delta, _ := sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err == nil ||
			!strings.Contains(err.Error(), "source provenance missing") {
			t.Fatalf("legacy continuation did not explain missing provenance: %v", err)
		}

		full, base = sourceBackup(t, source, 0)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal("validated full base did not initialize legacy source evidence:", err)
		}

		requireSourceCapability(t, destination, scope, true)
		sourceOperation(t, source, scope, "native-later", 1)

		delta, _ = sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}
	})
}

//nolint:cyclop,gocognit,funlen // Native corruptions share reopen, complete retry, and serving-fence checks.
func TestRestoreSourceMalformedControlsRemainFenced(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		sourceMarker(t, source, testScope())

		full, _ := sourceBackup(t, source, 0)
		for _, fault := range []string{"legacy-loading", "legacy-validated", "building-validated", "generation",
			"generation-alias", "active-lineage", "complete-loading-table", "unknown-version", "oversized"} {
			t.Run(fault, func(t *testing.T) {
				destination, reopen := restoreSourceDestination(t, source)

				spool, header, digest, err := stageRestore(t.Context(), bytes.NewReader(full), source.connection.pg != nil)
				if err != nil {
					t.Fatal(err)
				}

				if err := spool.Close(); err != nil {
					t.Fatal(err)
				}

				c := restoreControl{Version: 2, Phase: "validated", Header: &header, Digest: digest,
					PendingGeneration: strings.Repeat("1", 32), SourceState: "ready"}

				switch fault {
				case "legacy-loading", "legacy-validated":
					c.Version, c.PendingGeneration, c.SourceState = 1, "", ""
					if fault == "legacy-loading" {
						c.Phase = "loading"
					}
				case "building-validated":
					c.SourceState = "building"
				case "generation":
					c.PendingGeneration = "not-a-generation"
				case "generation-alias":
					c.ActiveGeneration = c.PendingGeneration
				case "active-lineage":
					c.ActiveGeneration = strings.Repeat("2", 32)
				case "complete-loading-table":
					c.Phase, c.ActiveGeneration, c.PendingGeneration = "complete", c.PendingGeneration, ""
				case "unknown-version":
					c.Version = 3
				case "oversized":
					c.Digest = strings.Repeat("a", 4096)
				}

				encoded, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}

				rawExec(t, destination.connection, "INSERT INTO "+destination.connection.table("restore_control")+
					" (singleton,control) VALUES(1,$1)", encoded)

				if err := destination.Close(); err != nil {
					t.Fatal(err)
				}

				destination = reopen()
				if err := destination.ResumeRestoreBeforeEraseRecovery(t.Context()); err == nil {
					t.Fatal("ambiguous control resumed without complete input")
				}

				if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err == nil {
					t.Fatal("malformed control was laundered by matching retry")
				}

				if err := destination.View(t.Context(), func(context.Context, *Tx) error { return nil }); err == nil {
					t.Fatal("malformed control lost loading fence")
				}
			})
		}
	})
}
