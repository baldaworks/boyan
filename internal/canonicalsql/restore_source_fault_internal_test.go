package canonicalsql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Rejection before any committed replay retires the attempt, not its immutable
// source rows. A corrected artifact gets a new generation behind the same fence.
//
//nolint:cyclop,gocognit,funlen // Native scenarios pair rejection, immutable rows, reopen and fresh retry.
func TestRestoreSourceRejectedInputReplacement(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		scope := testScope()
		sourceMarker(t, source, scope)
		sourceOperation(t, source, scope, "retained", 0)
		base, _ := sourceBackup(t, source, 0)
		sourceMarker(t, source, memory.Scope{Key: "test:new", Kind: memory.ScopeKindPersonal})

		good, _ := sourceBackup(t, source, 0)
		if err := source.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			key := Key{Family: Operation, Scope: scope, Parts: [][]byte{[]byte("retained")}}

			data, err := tx.Get(ctx, key)
			if err != nil {
				return err
			}

			var operation canonicalOperation
			if err := Decode(Operation, data, &operation); err != nil {
				return err
			}

			operation.Fingerprint = "conflicting-input"

			data, err = Encode(Operation, operation)
			if err != nil {
				return err
			}

			return tx.Put(ctx, key, data)
		}); err != nil {
			t.Fatal(err)
		}

		bad, _ := sourceBackup(t, source, 0)

		for _, accepted := range []bool{false, true} {
			for _, building := range []bool{false, true} {
				t.Run(fmt.Sprintf("chain=%t/building=%t", accepted, building), func(t *testing.T) {
					destination, reopen := restoreSourceDestination(t, source)
					if accepted {
						if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(base), 1); err != nil {
							t.Fatal(err)
						}
					} else {
						sourceMarker(t, destination, scope)
						sourceOperation(t, destination, scope, "retained", 0)
					}

					_, before := sourceControlSnapshot(t, destination)
					if building {
						remove := installSourceFault(t, destination, "ready")
						if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(bad), 1); err == nil {
							t.Fatal("ready fault ignored")
						}

						remove()
					}

					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(bad), 1); err == nil {
						t.Fatal("conflicting input accepted")
					}

					loading, chain := sourceControlSnapshot(t, destination)
					if loading == nil || loading.Header != nil || loading.PendingGeneration != "" {
						t.Fatal("rejected unused input remains bound")
					}

					if !sameSourceControl(before, chain) {
						t.Fatal("rejection changed accepted chain")
					}

					retired := sourceRowsSnapshot(t, destination)
					if len(retired) < 2 {
						t.Fatal("ready source rows were deleted")
					}

					if destination.SourcePrivacyReady(t.Context()) == nil {
						t.Fatal("rejection removed fence")
					}

					if err := destination.Close(); err != nil {
						t.Fatal(err)
					}

					destination = reopen()

					remove := installSourceFault(t, destination, "ready")
					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(good), 1); err == nil {
						t.Fatal("replacement ready fault ignored")
					}

					remove()

					loading, _ = sourceControlSnapshot(t, destination)
					for _, row := range retired {
						if fmt.Sprintf("%x", row.generation) == loading.PendingGeneration {
							t.Fatal("replacement reused ready generation")
						}
					}

					current := sourceRowsSnapshot(t, destination)
					for n, row := range retired {
						if n >= len(current) || row.id != current[n].id || row.scope != current[n].scope ||
							!bytes.Equal(row.generation, current[n].generation) {
							t.Fatal("replacement changed immutable ready rows")
						}
					}

					if destination.SourcePrivacyReady(t.Context()) == nil {
						t.Fatal("replacement removed fence early")
					}

					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(good), 1); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	})
}

func sameSourceControl(a, b *restoreControl) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	x, y := *a, *b
	x.Header, y.Header = nil, nil

	return x == y && ((a.Header == nil && b.Header == nil) ||
		(a.Header != nil && b.Header != nil && *a.Header == *b.Header))
}

func sourceRowsSnapshot(t *testing.T, owner *Owner) []restoreSourceRow {
	t.Helper()

	var rows []restoreSourceRow

	if err := owner.connection.run(t.Context(), false, false, func(tx *Tx) error {
		var err error

		rows, err = tx.restoreSourcePage(t.Context(), "1=1", nil)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return rows
}

func preparedSourceControl(t *testing.T, owner *Owner, data []byte) (restoreControl, *os.File) {
	t.Helper()

	spool, header, digest, err := stageRestore(t.Context(), bytes.NewReader(data), owner.connection.pg != nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})

	control, err := owner.prepareRestoreSource(t.Context(), spool, header, digest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	return control, spool
}

//nolint:cyclop,gocognit,gocyclo,funlen // Native failure modes share retained-binding and reopen assertions.
func TestRestoreSourceRejectionGuards(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		sourceMarker(t, source, testScope())

		full, _ := sourceBackup(t, source, 0)
		for _, mode := range []string{"ready-entry", "validated-entry", "canceled", "rollback-failure",
			"hidden-query", "rows-close", "stream-read", "stream-seek", "reset-failure", "changed-control", "changed-chain"} {
			t.Run(mode, func(t *testing.T) {
				destination, reopen := restoreSourceDestination(t, source)
				control, spool := preparedSourceControl(t, destination, full)

				var previous *restoreControl
				if mode == "ready-entry" || mode == "validated-entry" {
					previous = &control
					if mode == "validated-entry" {
						control.Phase = "validated"

						rewriteSourceControl(t, destination, "restore_control", func(c *restoreControl) { *c = control })
					}
				}

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				var failed bool

				base := destination.connection.q
				if mode == "rollback-failure" && destination.connection.pg == nil {
					destination.connection.q = restoreRollbackFaultQuery{base: base}
				}

				rejected := destination.restoreValidationTransaction(ctx, &failed, func(ctx context.Context, tx *Tx) error {
					switch mode {
					case "canceled":
						cancel()
					case "rollback-failure":
						if destination.connection.pg != nil {
							if err := destination.connection.pg.Close(ctx); err != nil {
								t.Fatal(err)
							}
						}
					case "hidden-query":
						observer := tx.q.(restoreValidationQuery)
						observer.base = restoreHiddenQueryFault{base: observer.base}
						tx.q = observer
						_, _, err := loadSourcePrivacy(ctx, tx, memory.CanonicalSourceRecord{Scope: testScope(), SourceID: "missing"})

						return err
					case "rows-close":
						observer := tx.q.(restoreValidationQuery)
						observer.base = restoreCloseFaultQuery{base: observer.base}
						tx.q = observer

						r, err := tx.q.query(ctx, "SELECT 1")
						if err != nil {
							return err
						}
						defer r.Close()
					}

					return invalidSourcePrivacy()
				})

				destination.connection.q = base
				if mode == "stream-read" || mode == "stream-seek" {
					rejected = destination.validateRestoreInput(ctx,
						restoreFaultStream{ReadSeeker: spool, mode: mode}, control.PendingGeneration)
				}

				if rejected == nil {
					t.Fatal("fault passed validation")
				}

				_, confirmed := rejected.(*restoreInputRejection)
				if mode == "canceled" || mode == "rollback-failure" || mode == "hidden-query" ||
					mode == "rows-close" || strings.HasPrefix(mode, "stream-") {
					if confirmed {
						t.Fatal("uncertain failure acquired replacement permission")
					}
				} else if !confirmed {
					t.Fatal("semantic rollback was not confirmed")
				}

				remove := func() {}

				switch mode {
				case "reset-failure":
					remove = installSourceFault(t, destination, "reset")
				case "changed-control":
					rewriteSourceControl(t, destination, "restore_control", func(c *restoreControl) {
						c.Digest = strings.Repeat("b", 64)
					})
				case "changed-chain":
					accepted := control
					accepted.Phase, accepted.ActiveGeneration = "complete", accepted.PendingGeneration
					accepted.PendingGeneration, accepted.SourceState = "", "ready"

					if err := destination.connection.run(ctx, true, false, func(tx *Tx) error {
						return tx.writeRestoreControl(ctx, "restore_chain", accepted)
					}); err != nil {
						t.Fatal(err)
					}
				}

				if err := destination.retireRejectedRestore(ctx, previous, control, nil, rejected); err == nil {
					t.Fatal("failure disappeared")
				}

				remove()

				if err := destination.Close(); err != nil {
					t.Fatal(err)
				}

				destination = reopen()
				loading, _ := sourceControlSnapshot(t, destination)

				if mode == "changed-control" {
					control.Digest = strings.Repeat("b", 64)
				}

				if !sameSourceControl(loading, &control) {
					t.Fatal("uncertain or previously replayable attempt lost its binding")
				}

				if destination.SourcePrivacyReady(t.Context()) == nil {
					t.Fatal("fault removed fence")
				}
			})
		}
	})
}

type restoreRollbackFaultQuery struct{ base query }

func (q restoreRollbackFaultQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	n, err := q.base.exec(ctx, statement, args...)
	if err == nil && statement == "ROLLBACK" {
		return n, errors.New("uncertain rollback acknowledgement")
	}

	return n, err
}

func (q restoreRollbackFaultQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	return q.base.query(ctx, statement, args...)
}

type restoreHiddenQueryFault struct{ base query }

func (q restoreHiddenQueryFault) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q restoreHiddenQueryFault) query(ctx context.Context, _ string, _ ...any) (rows, error) {
	return q.base.query(ctx, "SELECT unavailable_restore_column")
}

type restoreCloseFaultQuery struct{ base query }

func (q restoreCloseFaultQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q restoreCloseFaultQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	r, err := q.base.query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}

	return restoreCloseFaultRows{rows: r}, nil
}

type restoreCloseFaultRows struct{ rows }

func (r restoreCloseFaultRows) Close() error {
	return errors.Join(r.rows.Close(), errors.New("uncertain row cleanup"))
}

type restoreFaultStream struct {
	io.ReadSeeker

	mode string
}

func (r restoreFaultStream) Read(p []byte) (int, error) {
	if r.mode == "stream-read" {
		return 0, invalidSourcePrivacy()
	}

	return r.ReadSeeker.Read(p)
}

func (r restoreFaultStream) Seek(offset int64, whence int) (int64, error) {
	if r.mode == "stream-seek" {
		return 0, invalidSourcePrivacy()
	}

	return r.ReadSeeker.Seek(offset, whence)
}

//nolint:cyclop,funlen // Fixed native transition fault inventory keeps both dialects and cleanup together.
func installSourceFault(t *testing.T, owner *Owner, stage string) func() {
	t.Helper()

	c := owner.connection
	table, event, condition := "restore_source_capability", "INSERT", "1=1"

	switch stage {
	case "building", "capability":
		if stage == "capability" {
			table = "boundary_capability"
		}

		condition = "NEW.scope_key=$scope"
	case "ready":
		table, event = "restore_control", "UPDATE"
		condition = `instr(CAST(NEW.control AS TEXT),'"source_state":"ready"')>0 AND ` +
			`instr(CAST(NEW.control AS TEXT),'"phase":"loading"')>0`
	case "reset":
		table, event = "restore_control", "UPDATE"
		condition = `instr(CAST(NEW.control AS TEXT),'"header"')=0`
	case "operation":
		table = "operation"
		condition = "NEW.id=$operation"
	case "promotion":
		table = "restore_chain"
	case "remove":
		table, event = "restore_control", "DELETE"
	case "gc":
		event = "DELETE"
	}

	condition = strings.ReplaceAll(condition, "$scope", "X'746573743a6361703a303337'")

	condition = strings.ReplaceAll(condition, "$operation", "X'6e61746976652d7365636f6e64'")
	if c.pg == nil {
		rawExec(t, c, "CREATE TRIGGER source_fault BEFORE "+event+" ON "+table+" WHEN "+condition+
			" BEGIN SELECT RAISE(ABORT,'source fault'); END")

		return func() { rawExec(t, c, "DROP TRIGGER source_fault") }
	}

	condition = strings.ReplaceAll(condition, "X'746573743a6361703a303337'", "decode('746573743a6361703a303337','hex')")

	condition = strings.ReplaceAll(condition, "X'6e61746976652d7365636f6e64'",
		"decode('6e61746976652d7365636f6e64','hex')")
	if stage == "ready" {
		condition = `position('"source_state":"ready"' in convert_from(NEW.control,'UTF8'))>0 AND ` +
			`position('"phase":"loading"' in convert_from(NEW.control,'UTF8'))>0`
	}

	if stage == "reset" {
		condition = `position('"header"' in convert_from(NEW.control,'UTF8'))=0`
	}

	rawExec(t, c, "CREATE FUNCTION boyan_canonical.source_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF "+
		condition+" THEN RAISE EXCEPTION 'source fault'; END IF; "+
		"IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW; END $$")
	rawExec(t, c, "CREATE TRIGGER source_fault BEFORE "+event+" ON "+c.table(table)+
		" FOR EACH ROW EXECUTE FUNCTION boyan_canonical.source_fault()")

	return func() {
		rawExec(t, c, "DROP TRIGGER source_fault ON "+c.table(table))
		rawExec(t, c, "DROP FUNCTION boyan_canonical.source_fault()")
	}
}

func sourceControlSnapshot(t *testing.T, owner *Owner) (*restoreControl, *restoreControl) {
	t.Helper()

	var loading, chain *restoreControl

	if err := owner.connection.run(t.Context(), false, false, func(tx *Tx) error {
		var err error

		loading, err = tx.readRestoreControl(t.Context(), "restore_control")
		if err != nil {
			return err
		}

		chain, err = tx.readRestoreControl(t.Context(), "restore_chain")

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return loading, chain
}

func stageSourceReplayFault(t *testing.T, destination *Owner, data []byte, stage string) error {
	t.Helper()

	ctx, release, err := destination.admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	spool, header, digest, err := stageRestore(ctx, bytes.NewReader(data), destination.connection.pg != nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	}()

	control, err := destination.prepareRestoreSource(ctx, spool, header, digest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := destination.validateRestoreInput(ctx, spool, control.PendingGeneration); err != nil {
		t.Fatal(err)
	}

	if stage == "operation" {
		if err := destination.replayRestoreCapabilities(ctx, spool, 1); err != nil {
			t.Fatal(err)
		}
	}

	remove := installSourceFault(t, destination, stage)
	defer remove()

	if stage == "capability" {
		return destination.replayRestoreCapabilities(ctx, spool, 1)
	}

	return destination.replayRestoreProgress(ctx, spool, 1, nil, control.PendingGeneration)
}

//nolint:cyclop,gocognit,funlen,nestif // All native transition faults check reopen, retry, and atomic publication.
func TestRestoreSourceNativeTransitionsAndRetry(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		for n := range 40 {
			sourceMarker(t, source, memory.Scope{Key: fmt.Sprintf("test:cap:%03d", n), Kind: memory.ScopeKindPersonal})
		}

		scope := testScope()
		sourceCertifiedOperation(t, source, scope, legacyDerivedOperationPrefix+strings.Repeat("a", 64), 0)
		sourceOperation(t, source, scope, "native-second", 1)
		full, _ := sourceBackup(t, source, 0)
		sourceOperation(t, source, scope, "native-third", 2)

		changed, _ := sourceBackup(t, source, 0)
		for _, stage := range []string{"building", "ready", "capability", "operation", "promotion", "remove"} {
			t.Run(stage, func(t *testing.T) {
				destination, reopen := restoreSourceDestination(t, source)

				var err error
				if stage == "capability" || stage == "operation" {
					err = stageSourceReplayFault(t, destination, full, stage)
				} else {
					remove := installSourceFault(t, destination, stage)
					err = destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1)

					remove()
				}

				if err == nil {
					t.Fatal("native transition fault ignored")
				}

				loading, chain := sourceControlSnapshot(t, destination)
				if loading == nil || chain != nil {
					t.Fatal("failed transition published chain or cleared loading")
				}

				wantState := "ready"
				if stage == "building" || stage == "ready" {
					wantState = "building"
				}

				if loading.SourceState != wantState {
					t.Fatalf("source state=%s want %s", loading.SourceState, wantState)
				}

				if err := destination.VerifyCanonicalIntegrity(t.Context()); err == nil {
					t.Fatal("interrupted restore served")
				}

				if err := destination.Close(); err != nil {
					t.Fatal(err)
				}

				destination = reopen()
				if stage == "promotion" || stage == "remove" {
					if err := destination.ResumeRestoreBeforeEraseRecovery(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := destination.ResumeRestoreBeforeEraseRecovery(t.Context()); err == nil {
						t.Fatal("loading resumed without complete artifact")
					}
					// Truncated or changed input must not replace an accepted ready/building identity.
					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full[:len(full)-1]), 1); err == nil {
						t.Fatal("truncated retry accepted")
					}

					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(changed), 1); err == nil {
						t.Fatal("different complete input replaced interrupted restore")
					}

					if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
						t.Fatal(err)
					}
				}

				current, accepted := sourceControlSnapshot(t, destination)
				if current != nil || accepted == nil || accepted.ActiveGeneration != loading.PendingGeneration {
					t.Fatal("retry did not atomically promote the same pending generation")
				}

				if err := destination.VerifyCanonicalIntegrity(t.Context()); err != nil {
					t.Fatal(err)
				}

				requireSourceCapability(t, destination, scope, true)
			})
		}
	})
}

//nolint:cyclop // Compare active, pending, and unreferenced generations across a failed then successful cleanup.
func TestRestoreSourceGarbageCollectionRetainsReferences(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		sourceMarker(t, source, testScope())
		full, _ := sourceBackup(t, source, 0)

		destination, _ := restoreSourceDestination(t, source)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		_, old := sourceControlSnapshot(t, destination)

		remove := installSourceFault(t, destination, "gc")
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal("deferred GC affected completed restore:", err)
		}

		remove()

		_, active := sourceControlSnapshot(t, destination)
		if active.ActiveGeneration == old.ActiveGeneration {
			t.Fatal("full input reused immutable generation")
		}

		if err := destination.connection.run(t.Context(), true, false, func(tx *Tx) error {
			pending := *active
			pending.Phase, pending.PendingGeneration = "loading", old.ActiveGeneration

			return tx.writeRestoreControl(t.Context(), "restore_control", pending)
		}); err != nil {
			t.Fatal(err)
		}

		if err := destination.collectRestoreSources(t.Context()); err != nil {
			t.Fatal(err)
		}

		var count int
		if err := scanOne(t.Context(), destination.connection.q,
			"SELECT count(*) FROM "+destination.connection.table("restore_source_capability"),
			nil, &count); err != nil || count != 2 {
			t.Fatalf("GC removed active/pending rows: %d %v", count, err)
		}

		rawExec(t, destination.connection, "DELETE FROM "+destination.connection.table("restore_control"))

		if err := destination.collectRestoreSources(t.Context()); err != nil {
			t.Fatal(err)
		}

		if err := scanOne(t.Context(), destination.connection.q,
			"SELECT count(*) FROM "+destination.connection.table("restore_source_capability"),
			nil, &count); err != nil || count != 1 {
			t.Fatalf("GC did not remove only unreferenced rows: %d %v", count, err)
		}

		requireSourceCapability(t, destination, testScope(), true)
	})
}
