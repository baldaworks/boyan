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
	runPublicRestoreCertifiedMutation(t, true)
}

func runPublicRestoreCertifiedMutation(t *testing.T, logical bool) {
	forEachMutation(
		t,
		func(t *testing.T, f *storefixture.Fixture, s mutationStore, sourceProbe mutationProbe) {
			m := nativeMutation(t)
			certifyMutation(t, &m)

			outcome, err := s.ApplyCanonicalMutation(t.Context(), m)
			if err != nil {
				t.Fatal(err)
			}

			full, base := publicRestoreArtifact(t, f.Store, 0, logical)
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

			delta, _ := publicRestoreArtifact(t, f.Store, base, logical)
			assertPublicRestoreRestrictions(t, f, m, outcome, full, delta, logical)
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

			full, _ := publicRestoreArtifact(t, f.Store, 0, logical)
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

func publicRestoreArtifact(t *testing.T, s storefixture.Store, since uint64, logical bool) ([]byte, uint64) {
	t.Helper()

	if !logical {
		return publicRestoreBytes(t, s.(archive.CanonicalBackupWriter), since)
	}

	var output bytes.Buffer
	if err := s.(archive.CanonicalLogicalExporter).ExportCanonicalLogical(t.Context(), &output); err != nil {
		t.Fatal(err)
	}

	return output.Bytes(), 0
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
					runRestoreCompletionDelta(t, f, s, unrelated, false)
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
				runRestoreCompletionDelta(t, f, s, unrelated, true)
			})
		})
	}
}

//nolint:cyclop,funlen,gocognit,gocyclo // One chain completion must release only its owned recovery state.
func runRestoreCompletionDelta(
	t *testing.T,
	f *storefixture.Fixture,
	s mutationStore,
	unrelated bool,
	logical bool,
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

	full, base := publicRestoreArtifact(t, f.Store, 0, logical)

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

	if err := restorePublicArtifact(t.Context(), d.Store.(restoredStore), full, logical); err != nil {
		t.Fatal(err)
	}

	completed, err := f.Store.(memory.ScopeEraseStore).RunScopeErase(t.Context(), request)
	if err != nil || completed.Stage != memory.ScopeEraseComplete {
		t.Fatalf("source completion: %+v %v", completed, err)
	}

	delta, _ := publicRestoreArtifact(t, f.Store, base, logical)
	if err := restorePublicArtifact(t.Context(), d.Store.(restoredStore), delta, logical); err != nil {
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
		local, found, err := es.LoadScopeErase(
			t.Context(),
			localRequest.Scope,
			localRequest.OperationID,
		)
		if err != nil || !found || !reflect.DeepEqual(local, localJournal) {
			t.Fatalf("unrelated pending journal changed: %+v %v", local, err)
		}

		pending, _, err := es.ListPendingScopeErases(t.Context(), nil, 10)
		if err != nil || len(pending) != 1 || pending[0].Scope != localRequest.Scope {
			t.Fatalf("unrelated pending ownership: %+v %v", pending, err)
		}

		fresh := m.Payloads[0]

		fresh.Ref.ID = "still-globally-guarded"
		if err := es.PutPayload(t.Context(), fresh.Data, fresh.Ref); err == nil {
			t.Fatal("unrelated pending erase lost global payload guard")
		}
	}

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
