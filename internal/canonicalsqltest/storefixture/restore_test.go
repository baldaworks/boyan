package storefixture_test

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/forgetops"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type restoredStore interface {
	mutationStore
	archive.CanonicalBackupWriter
	archive.CanonicalBackupReader
	archive.CanonicalIntegrityVerifier
	ResumeRestoreBeforeEraseRecovery(ctx context.Context) error
}

func publicRestoreDestination(
	t *testing.T,
	backend canonicalsqltest.Backend,
) *storefixture.Fixture {
	t.Helper()

	f := &storefixture.Fixture{Backend: backend}
	if backend == canonicalsqltest.SQLite {
		path := filepath.Join(t.TempDir(), "restore.sqlite")
		f.Database = &canonicalsqltest.Database{DSN: path, Backend: backend}
		f.Open = func(ctx context.Context) (storefixture.Store, error) {
			return sqlite.Open(ctx, sqlite.Config{Path: path})
		}
	} else {
		f.Database = canonicalsqltest.PostgresDatabase(t)
		f.Open = func(ctx context.Context) (storefixture.Store, error) {
			return postgres.Open(
				ctx,
				postgres.Config{DSN: f.Database.DSN, AllowLocalInsecure: true},
			)
		}
	}

	var err error

	f.Store, err = f.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := f.Store.Close(); err != nil {
			t.Error(err)
		}
	})

	return f
}

func publicRestoreBytes(
	t *testing.T,
	s archive.CanonicalBackupWriter,
	since uint64,
) ([]byte, uint64) {
	t.Helper()

	var out bytes.Buffer

	end, err := s.WriteCanonicalBackup(t.Context(), &out, since)
	if err != nil {
		t.Fatal(err)
	}

	return out.Bytes(), end
}

func TestPublicRestoreCertifiedMutationReplayAndRestrictions(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, s mutationStore, sourceProbe mutationProbe) {
		m := nativeMutation(t)
		certifyMutation(t, &m)

		outcome, err := s.ApplyCanonicalMutation(t.Context(), m)
		if err != nil {
			t.Fatal(err)
		}

		full, base := publicRestoreBytes(t, f.Store.(archive.CanonicalBackupWriter), 0)
		if err := sourceProbe.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for _, family := range []canonicalsql.Family{
				canonicalsql.SourcePrivacy, canonicalsql.SourcePrivacyScope, canonicalsql.TurnCertificate,
			} {
				id := m.Sources[0].SourceID
				if family == canonicalsql.SourcePrivacyScope {
					id = "coverage"
				}

				if err := tx.Delete(t.Context(), canonicalsql.Key{
					Family: family, Scope: m.Scope, Parts: [][]byte{[]byte(id)},
				}); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		delta, _ := publicRestoreBytes(t, f.Store.(archive.CanonicalBackupWriter), base)
		assertPublicRestoreRestrictions(t, f, m, outcome, full, delta, false)
	})
}

func TestPublicLogicalCertifiedMutationReplayAndRestrictions(t *testing.T) {
	forEachMutation(
		t,
		func(t *testing.T, f *storefixture.Fixture, s mutationStore, sourceProbe mutationProbe) {
			m := nativeMutation(t)
			certifyMutation(t, &m)

			outcome, err := s.ApplyCanonicalMutation(t.Context(), m)
			if err != nil {
				t.Fatal(err)
			}

			full := publicRestoreFull(t, f.Store, true)
			if err := sourceProbe.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
				for _, family := range []canonicalsql.Family{canonicalsql.SourcePrivacy,
					canonicalsql.SourcePrivacyScope, canonicalsql.TurnCertificate} {
					id := m.Sources[0].SourceID
					if family == canonicalsql.SourcePrivacyScope {
						id = "coverage"
					}

					if err := tx.Delete(
						t.Context(),
						canonicalsql.Key{
							Family: family,
							Scope:  m.Scope,
							Parts:  [][]byte{[]byte(id)},
						},
					); err != nil {
						return err
					}
				}

				return nil
			}); err != nil {
				t.Fatal(err)
			}

			updated := publicRestoreFull(t, f.Store, true)
			assertPublicRestoreRestrictions(t, f, m, outcome, full, updated, true)
		},
	)
}

//nolint:cyclop,gocognit,funlen // Both routes retain identical restriction and replay assertions.
func assertPublicRestoreRestrictions(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation,
	outcome memory.CanonicalMutationOutcome, full, delta []byte, logical bool,
) {
	for _, restriction := range []string{"standard", "sensitive", "unknown", "incomplete", "denied"} {
		t.Run(restriction, func(t *testing.T) {
			d := publicRestoreDestination(t, f.Backend)

			ds := d.Store.(restoredStore)
			if err := restorePublicArtifact(t.Context(), ds, full, logical); err != nil {
				t.Fatal(err)
			}

			probe := newMutationProbe(t, d)

			if restriction == "denied" {
				request := memory.ScopeEraseRequest{
					Scope:                m.Scope,
					OperationID:          "restore-forget",
					Fingerprint:          "restore-forget",
					ExpectedScopeVersion: 1,
					ForgottenAt:          m.Operation.CommittedAt.Add(time.Hour),
				}
				if _, err := d.Store.(memory.ScopeEraseStore).RunScopeErase(
					t.Context(),
					request,
				); err != nil {
					t.Fatal(err)
				}
			} else if restriction != "standard" {
				var privacy map[string]any
				probe.read(
					t,
					m.Scope,
					canonicalsql.SourcePrivacy,
					m.Sources[0].SourceID,
					&privacy,
				)

				if restriction == "incomplete" {
					probe.write(
						t,
						canonicalsql.Key{
							Family: canonicalsql.SourcePrivacyScope,
							Scope:  m.Scope,
							Parts:  [][]byte{[]byte("coverage")},
						},
						map[string]any{"version": 1, "scope": m.Scope, "incomplete": true},
					)
				} else {
					privacy["state"] = restriction
					probe.write(
						t,
						canonicalsql.Key{
							Family: canonicalsql.SourcePrivacy,
							Scope:  m.Scope,
							Parts:  [][]byte{[]byte(m.Sources[0].SourceID)},
						},
						privacy,
					)
				}
			}

			if err := restorePublicArtifact(t.Context(), ds, full, logical); err != nil {
				t.Fatalf("stricter %s restore: %v", restriction, err)
			}

			if err := restorePublicArtifact(t.Context(), ds, delta, logical); err != nil {
				t.Fatal("restrictive tombstone merge: ", err)
			}

			if restriction == "denied" {
				assertPublicErasePayloads(t, d.Store.(forgetStore), m)

				state, err := ds.LoadScopeState(t.Context(), m.Scope)
				if err != nil || state.Version != 2 || state.ChangeSeq != 2 {
					t.Fatalf("destination erase baseline regressed: %+v %v", state, err)
				}
			} else {
				want := restriction
				if restriction == "incomplete" {
					want = "unknown"
				}

				if got := privacyState(
					t,
					probe,
					m.Scope,
					m.Sources[0].SourceID,
				); got != want {
					t.Fatalf("privacy=%s want %s", got, want)
				}
			}

			got, err := ds.ApplyCanonicalMutation(t.Context(), m)
			if err != nil || !reflect.DeepEqual(got, outcome) {
				t.Fatalf("restored replay=%+v %v", got, err)
			}

			if restriction == "denied" {
				assertPublicErasePayloads(t, d.Store.(forgetStore), m)
			}
		})
	}
}

func TestPublicRestorePendingEraseUnchangedUntilRecovery(t *testing.T) {
	runPublicRestorePendingErase(t, false)
}

func TestPublicLogicalPendingEraseUnchangedUntilRecovery(t *testing.T) {
	runPublicRestorePendingErase(t, true)
}

//nolint:cyclop,funlen // Both real formats retain pending journal authority until explicit recovery.
func runPublicRestorePendingErase(t *testing.T, logical bool) {
	forEachMutation(
		t,
		func(t *testing.T, f *storefixture.Fixture, s mutationStore, _ mutationProbe) {
			m := nativeMutation(t)
			certifyMutation(t, &m)

			if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
				t.Fatal(err)
			}

			request := memory.ScopeEraseRequest{
				Scope:                m.Scope,
				OperationID:          "pending-restore",
				Fingerprint:          "pending-restore",
				ExpectedScopeVersion: 1,
				ForgottenAt:          m.Operation.CommittedAt.Add(time.Hour),
			}

			original, err := f.Store.(memory.ScopeEraseStore).BeginScopeErase(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}

			full := publicRestoreFull(t, f.Store, logical)
			d := publicRestoreDestination(t, f.Backend)

			ds := d.Store.(restoredStore)
			if err := restorePublicArtifact(t.Context(), ds, full[:len(full)-1], logical); err == nil {
				t.Fatal("truncation accepted")
			}

			if _, err := d.Store.(memory.ScopeEraseStore).AdvanceScopeErase(
				t.Context(),
				m.Scope,
				request.OperationID,
			); err == nil {
				t.Fatal("erase advanced during loading")
			}

			if err := d.Store.Close(); err != nil {
				t.Fatal(err)
			}

			d.Store, err = d.Open(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			ds = d.Store.(restoredStore)
			if ds.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil {
				t.Fatal("loading preflight succeeded")
			}

			probe := newMutationProbe(t, d)

			remove := probe.failInserts(t, "source_privacy_ready")
			if err := restorePublicArtifact(t.Context(), ds, full, logical); err == nil {
				t.Fatal("validated preparation fault ignored")
			}

			remove()

			if _, err := d.Store.(memory.ScopeEraseStore).AdvanceScopeErase(
				t.Context(),
				m.Scope,
				request.OperationID,
			); err == nil {
				t.Fatal("pending erase advanced before validated preparation finished")
			}

			if err := d.Store.Close(); err != nil {
				t.Fatal(err)
			}

			d.Store, err = d.Open(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			ds = d.Store.(restoredStore)
			if err := ds.ResumeRestoreBeforeEraseRecovery(t.Context()); err != nil {
				t.Fatal(err)
			}

			store := d.Store.(memory.ScopeEraseStore)

			got, found, err := store.LoadScopeErase(t.Context(), m.Scope, request.OperationID)
			if err != nil || !found || !reflect.DeepEqual(got, original) {
				t.Fatalf("pending journal changed: %+v %v", got, err)
			}

			got, err = store.RunScopeErase(t.Context(), request)
			if err != nil || got.Stage != memory.ScopeEraseComplete || got.SourceCount != 1 ||
				got.RevisionCount != 1 {
				t.Fatalf("recovery=%+v %v", got, err)
			}

			assertPublicErasePayloads(t, d.Store.(forgetStore), m)
		},
	)
}

func publicRestoreFull(t *testing.T, s storefixture.Store, logical bool) []byte {
	t.Helper()

	if !logical {
		full, _ := publicRestoreBytes(t, s.(archive.CanonicalBackupWriter), 0)

		return full
	}

	var output bytes.Buffer
	if err := s.(archive.CanonicalLogicalExporter).ExportCanonicalLogical(t.Context(), &output); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

func restorePublicArtifact(ctx context.Context, s archive.CanonicalBackupReader, data []byte, logical bool) error {
	if logical {
		_, err := s.(archive.CanonicalLogicalImporter).ImportCanonicalLogicalWithOptions(
			ctx,
			bytes.NewReader(data),
			archive.CanonicalLogicalImportOptions{BatchSize: 1},
		)

		return err
	}

	return s.RestoreCanonicalBackup(ctx, bytes.NewReader(data), 1)
}

func TestPublicRestorePendingEraseCompletionDelta(t *testing.T) {
	for _, unrelated := range []bool{false, true} {
		name := "source-only"
		if unrelated {
			name = "unrelated-pending"
		}

		t.Run(name, func(t *testing.T) {
			forEachMutation(
				t,
				func(t *testing.T, f *storefixture.Fixture, s mutationStore, _ mutationProbe) {
					runRestoreCompletionDelta(t, f, s, unrelated)
				},
			)
		})
	}
}

func TestPublicLogicalPendingEraseCompletion(t *testing.T) {
	for _, unrelated := range []bool{false, true} {
		name := "source-only"
		if unrelated {
			name = "unrelated-pending"
		}

		t.Run(name, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, s mutationStore, _ mutationProbe) {
				runLogicalEraseCompletion(t, f, s, unrelated)
			})
		})
	}
}

func runLogicalEraseCompletion(t *testing.T, f *storefixture.Fixture, s mutationStore, unrelated bool) {
	t.Helper()

	m := nativeMutation(t)
	certifyMutation(t, &m)

	if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	request := memory.ScopeEraseRequest{Scope: m.Scope, OperationID: "logical-erase",
		Fingerprint: "logical-erase", ExpectedScopeVersion: 1,
		ForgottenAt: m.Operation.CommittedAt.Add(time.Hour)}

	original, err := f.Store.(memory.ScopeEraseStore).BeginScopeErase(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	full := publicRestoreFull(t, f.Store, true)
	d := publicRestoreDestination(t, f.Backend)

	if err := restorePublicArtifact(t.Context(), d.Store.(restoredStore), full, true); err != nil {
		t.Fatal("pending full logical import: ", err)
	}

	es := d.Store.(forgetStore)
	localRequest := memory.ScopeEraseRequest{
		Scope:       memory.Scope{Key: "test:unrelated-local-pending", Kind: memory.ScopeKindPersonal},
		OperationID: "local-erase", Fingerprint: "local-erase", ForgottenAt: request.ForgottenAt,
	}

	var localJournal memory.ScopeEraseJournal

	if unrelated {
		localJournal, err = es.BeginScopeErase(t.Context(), localRequest)
		if err != nil {
			t.Fatal(err)
		}
	}

	assertPublicLogicalImportRefused(t, d, full, original)
	completePublicLogicalErase(t, d, request)

	if unrelated {
		assertPublicUnrelatedErase(t, d.Store.(forgetStore), localJournal, m)
	}

	assertPublicRestoreEraseRecovered(t, d, m)

	if _, err := f.Store.(memory.ScopeEraseStore).RunScopeErase(t.Context(), request); err != nil {
		t.Fatal("complete source for matching-lineage full snapshot: ", err)
	}

	current := publicRestoreFull(t, f.Store, true)
	if err := restorePublicArtifact(t.Context(), d.Store.(restoredStore), current, true); err != nil {
		t.Fatal("logical import after completed recovery: ", err)
	}

	assertPublicErasePayloads(t, d.Store.(forgetStore), m)
	assertPublicEraseReplay(t, d, d.Store.(mutationStore), d.Store.(forgetStore), m)
}

func assertPublicLogicalImportRefused(t *testing.T, d *storefixture.Fixture, full []byte,
	original memory.ScopeEraseJournal,
) {
	t.Helper()

	before, _ := publicRestoreBytes(t, d.Store.(restoredStore), 0)
	input := bytes.NewReader(full)

	err := d.Store.(archive.CanonicalLogicalImporter).ImportCanonicalLogical(t.Context(), input)
	if code, _, ok := memory.ClassifyError(err); !ok || code != memory.CodeConflict {
		t.Fatalf("fresh logical import while erase pending: %v", err)
	}

	after, _ := publicRestoreBytes(t, d.Store.(restoredStore), 0)
	if input.Len() != len(full) || !bytes.Equal(before, after) {
		t.Fatal("refused import consumed input or changed canonical state")
	}

	assertPublicRestoreJournal(t, d.Store.(forgetStore), original)
}

func completePublicLogicalErase(t *testing.T, d *storefixture.Fixture, request memory.ScopeEraseRequest) {
	t.Helper()

	completed, err := d.Store.(forgetStore).RunScopeErase(t.Context(), request)
	if err != nil || completed.Stage != memory.ScopeEraseComplete || completed.SourceCount != 1 ||
		completed.RevisionCount != 1 || completed.Outcome.ScopeVersion != 2 {
		t.Fatalf("destination logical recovery: %+v %v", completed, err)
	}

	if err := d.Store.Close(); err != nil {
		t.Fatal(err)
	}

	d.Store, err = d.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	assertPublicRestoreJournal(t, d.Store.(forgetStore), completed)
}

func assertPublicRestoreJournal(t *testing.T, es forgetStore, want memory.ScopeEraseJournal) {
	t.Helper()

	got, found, err := es.LoadScopeErase(t.Context(), want.Scope, want.OperationID)
	if err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("restore journal changed: %+v %v", got, err)
	}
}

//nolint:cyclop,funlen // One chain completion must release only its owned recovery state.
func runRestoreCompletionDelta(
	t *testing.T,
	f *storefixture.Fixture,
	s mutationStore,
	unrelated bool,
) {
	t.Helper()
	m := nativeMutation(t)
	certifyMutation(t, &m)

	if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	request := memory.ScopeEraseRequest{Scope: m.Scope, OperationID: "chain-erase",
		Fingerprint: "chain-erase", ExpectedScopeVersion: 1,
		ForgottenAt: m.Operation.CommittedAt.Add(time.Hour)}

	journal, err := f.Store.(memory.ScopeEraseStore).BeginScopeErase(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	for n := 0; n < 10 && journal.Stage != memory.ScopeEraseCheckSources; n++ {
		journal, err = f.Store.(memory.ScopeEraseStore).AdvanceScopeErase(
			t.Context(),
			m.Scope,
			request.OperationID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if journal.Stage != memory.ScopeEraseCheckSources {
		t.Fatal("source did not reach locked pending state")
	}

	full, base := publicRestoreBytes(t, f.Store.(archive.CanonicalBackupWriter), 0)

	d := publicRestoreDestination(t, f.Backend)
	localRequest := memory.ScopeEraseRequest{
		Scope: memory.Scope{
			Key:  "test:unrelated-local-pending",
			Kind: memory.ScopeKindPersonal,
		},
		OperationID: "local-erase",
		Fingerprint: "local-erase",
		ForgottenAt: request.ForgottenAt,
	}

	var localJournal memory.ScopeEraseJournal

	if unrelated {
		var err error

		localJournal, err = d.Store.(memory.ScopeEraseStore).BeginScopeErase(
			t.Context(),
			localRequest,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := d.Store.(restoredStore).RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
		t.Fatal(err)
	}

	completed, err := f.Store.(memory.ScopeEraseStore).RunScopeErase(t.Context(), request)
	if err != nil || completed.Stage != memory.ScopeEraseComplete {
		t.Fatalf("source completion: %+v %v", completed, err)
	}

	delta, _ := publicRestoreBytes(t, f.Store.(archive.CanonicalBackupWriter), base)
	if err := d.Store.(restoredStore).RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
		t.Fatalf("completion delta restore: %v", err)
	}

	if err := d.Store.Close(); err != nil {
		t.Fatal(err)
	}

	d.Store, err = d.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	es := d.Store.(forgetStore)

	got, found, err := es.LoadScopeErase(t.Context(), m.Scope, request.OperationID)
	if err != nil || !found || !reflect.DeepEqual(got, completed) {
		t.Fatalf("completed journal: %+v %v", got, err)
	}

	if unrelated {
		assertPublicUnrelatedErase(t, es, localJournal, m)
	}

	assertPublicRestoreEraseRecovered(t, d, m)
}

func assertPublicUnrelatedErase(t *testing.T, es forgetStore, original memory.ScopeEraseJournal,
	m memory.CanonicalMutation,
) {
	t.Helper()

	local, found, err := es.LoadScopeErase(t.Context(), original.Scope, original.OperationID)
	if err != nil || !found || !reflect.DeepEqual(local, original) {
		t.Fatalf("unrelated pending journal changed: %+v %v", local, err)
	}

	pending, _, err := es.ListPendingScopeErases(t.Context(), nil, 10)
	if err != nil || len(pending) != 1 || pending[0].Scope != original.Scope {
		t.Fatalf("unrelated pending ownership: %+v %v", pending, err)
	}

	fresh := m.Payloads[0]

	fresh.Ref.ID = "still-globally-guarded"
	if err := es.PutPayload(t.Context(), fresh.Data, fresh.Ref); err == nil {
		t.Fatal("unrelated pending erase lost global payload guard")
	}
}

func assertPublicRestoreEraseRecovered(t *testing.T, d *storefixture.Fixture, m memory.CanonicalMutation) {
	t.Helper()

	es := d.Store.(forgetStore)
	if err := forgetops.RecoverPendingScopeErases(t.Context(), es); err != nil {
		t.Fatal(err)
	}

	probe := newMutationProbe(t, d)
	if err := probe.connection.View(t.Context(), func(tx *canonicalsql.Tx) error {
		for _, family := range []canonicalsql.Family{canonicalsql.EraseFence, canonicalsql.EraseGuard,
			canonicalsql.EraseLockRef, canonicalsql.EraseBlobLock} {
			rows, err := tx.Scan(t.Context(), canonicalsql.Range{Family: family, Limit: 1})
			if err != nil {
				return err
			}

			if len(rows) != 0 {
				t.Errorf("completed erase retained %s", family)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	fresh := m.Payloads[0]

	fresh.Ref.ID = "after-chain-completion"
	if err := es.PutPayload(t.Context(), fresh.Data, fresh.Ref); err != nil {
		t.Fatalf("ordinary payload write remained fenced: %v", err)
	}

	assertPublicErasePayloads(t, es, m)
	assertPublicEraseReplay(t, d, d.Store.(mutationStore), es, m)
}
