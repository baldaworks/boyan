package archive_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
)

//nolint:cyclop,funlen // Native triggers cover the same finite transition stages on both engines.
func installRestoreFault(t *testing.T, p archiveProbe, stage string) func() {
	t.Helper()

	table, event, condition := "restore_control", "INSERT", "1=1"

	switch stage {
	case "batch":
		table = "payload"
		condition = `NEW.id=X'62' AND EXISTS(SELECT 1 FROM restore_control WHERE instr(CAST(control AS TEXT),'"header"')>0)`
	case "validated":
		event = "UPDATE"
		condition = `instr(CAST(NEW.control AS TEXT),'"phase":"validated"')>0`
	case "privacy":
		table = "source_privacy_ready"
	case "remove":
		event = "DELETE"
	}

	if p.table(table) == table {
		if err := p.exec(
			"CREATE TRIGGER task14_fault BEFORE " + event + " ON " + table + " WHEN " + condition +
				" BEGIN SELECT RAISE(ABORT,'task14 fault'); END",
		); err != nil {
			t.Fatal(err)
		}

		return func() {
			if err := p.exec("DROP TRIGGER task14_fault"); err != nil {
				t.Fatal(err)
			}
		}
	}

	switch stage {
	case "loading", "privacy", "remove":
		condition = "true"
	case "batch":
		condition = `NEW.id=decode('62','hex') AND EXISTS(SELECT 1 FROM boyan_canonical.restore_control `+
			`WHERE position('"header"' in convert_from(control,'UTF8'))>0)`
	case "validated":
		condition = `position('"phase":"validated"' in convert_from(NEW.control,'UTF8'))>0`
	}

	statement := `CREATE FUNCTION boyan_canonical.task14_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF ` +
		condition + ` THEN RAISE EXCEPTION 'task14 fault'; END IF; `+
		`IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW; END $$`
	if err := p.exec(statement); err != nil {
		t.Fatal(err)
	}

	if err := p.exec(
		"CREATE TRIGGER task14_fault BEFORE " + event + " ON " + p.table(
			table,
		) + " FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task14_fault()",
	); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := p.exec("DROP TRIGGER task14_fault ON " + p.table(table)); err != nil {
			t.Fatal(err)
		}

		if err := p.exec("DROP FUNCTION boyan_canonical.task14_fault()"); err != nil {
			t.Fatal(err)
		}
	}
}

//nolint:cyclop,gocognit,funlen,nestif // Every fault needs actual reopen and its applicable recovery path.
func TestSQLRestoreNativeTransitionFaults(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		p := newArchiveProbe(t, source)
		keys := []canonicalsql.Key{
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a")}},
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("b")}},
		}
		putArchiveRows(t, p, keys, "data")

		_, _, data := backupFrames(t, source.Store.(backupStore), 0)
		for _, stage := range []string{"loading", "batch", "validated", "privacy", "remove"} {
			t.Run(stage, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)
				dp := newArchiveProbe(t, d)
				ds := d.Store.(restoreStore)
				remove := installRestoreFault(t, dp, stage)
				read := false

				r := &restoreFirstRead{Reader: bytes.NewReader(data), read: func() { read = true }}
				if err := ds.RestoreCanonicalBackup(t.Context(), r, 1); err == nil {
					t.Fatal("native failure ignored")
				}

				remove()

				if stage == "loading" && read {
					t.Fatal("stream consumed before durable loading")
				}

				if stage != "loading" && ds.VerifyCanonicalIntegrity(t.Context()) == nil {
					t.Fatal("failed restore served")
				}

				if err := d.Store.Close(); err != nil {
					t.Fatal(err)
				}

				var err error

				d.Store, err = d.Open(t.Context())
				if err != nil {
					t.Fatal(err)
				}

				ds = d.Store.(restoreStore)
				if stage == "privacy" {
					if err := ds.PrepareSourcePrivacy(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else if stage == "remove" {
					if err := ds.ResumeRestoreBeforeEraseRecovery(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else {
					if stage != "loading" &&
						ds.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil {
						t.Fatal("loading resumed without complete artifact")
					}

					if err := ds.RestoreCanonicalBackup(
						t.Context(),
						bytes.NewReader(data),
						1,
					); err != nil {
						t.Fatal(err)
					}
				}

				if err := ds.VerifyCanonicalIntegrity(t.Context()); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

//nolint:cyclop // Inspect descriptors before cancellation closes the unlinked spools.
func TestSQLRestoreSpoolPermissionsAndCancellation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(restoreStore)
		_, _, data := backupFrames(t, s, 0)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		reader := &restoreFirstRead{Reader: bytes.NewReader(data), read: func() {
			entries, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}

			count := 0

			for _, entry := range entries {
				path := filepath.Join("/proc/self/fd", entry.Name())

				target, err := os.Readlink(path)
				if err != nil || !strings.Contains(target, "boyan-sql-restore-") {
					continue
				}

				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}

				if !strings.HasSuffix(target, " (deleted)") || info.Mode().Perm() != 0o600 {
					t.Fatal("spool is linked or not private")
				}

				count++
			}

			if count < 2 {
				t.Fatalf("spool and bounded index descriptors=%d", count)
			}

			cancel()
		}}
		if err := s.RestoreCanonicalBackup(ctx, reader, 1); err == nil {
			t.Fatal("cancel ignored")
		}

		if err := s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1); err != nil {
			t.Fatal(err)
		}
	})
}
