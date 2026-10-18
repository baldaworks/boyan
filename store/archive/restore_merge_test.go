package archive_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,funlen,gocognit // Valid checksums cannot mask journal baseline or receipt mismatches.
func TestSQLRestoreRejectsMismatchedEraseOutcome(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "receipt"
		if pending {
			name = "pending-baseline"
		}

		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				request := memory.ScopeEraseRequest{
					Scope: memory.Scope{
						Key:  "test:erase-outcome-binding",
						Kind: memory.ScopeKindPersonal,
					},
					OperationID: "erase-binding",
					Fingerprint: "erase-binding",
					ForgottenAt: time.Now().UTC(),
				}

				var (
					journal memory.ScopeEraseJournal
					err     error
				)
				if pending {
					journal, err = f.Store.(memory.ScopeEraseStore).BeginScopeErase(
						t.Context(),
						request,
					)
				} else {
					journal, err = f.Store.(memory.ScopeEraseStore).RunScopeErase(
						t.Context(),
						request,
					)
				}

				if err != nil {
					t.Fatal(err)
				}

				want, err := canonicalsql.Encode(canonicalsql.EraseJournal, journal)
				if err != nil {
					t.Fatal(err)
				}

				_, frames, full := backupFrames(t, f.Store.(backupStore), 0)

				if pending {
					journal.BaselineVersion++
					journal.BaselineChangeSeq++
				} else {
					journal.Outcome.ScopeVersion++
				}

				if err := journal.Validate(); err != nil {
					t.Fatal("control must retain valid DTO identity: ", err)
				}

				changed := false

				for _, frame := range frames {
					if frame["family"] == string(canonicalsql.EraseJournal) {
						value, err := canonicalsql.Encode(canonicalsql.EraseJournal, journal)
						if err != nil {
							t.Fatal(err)
						}

						frame["envelope"] = value
						changed = true
					}
				}

				if !changed {
					t.Fatal("genuine journal missing from archive")
				}

				d := restoreDestination(t, f.Backend)

				ds := d.Store.(restoreStore)
				if err := ds.RestoreCanonicalBackup(
					t.Context(),
					bytes.NewReader(encodeRestoreFrames(t, frames)),
					1,
				); err == nil {
					t.Fatal("erase journal binding diverged without rejection")
				}

				if ds.PrepareSourcePrivacy(t.Context()) == nil ||
					ds.VerifyCanonicalIntegrity(t.Context()) == nil {
					t.Fatal("mismatched journal published trusted state")
				}

				if err := d.Store.Close(); err != nil {
					t.Fatal(err)
				}

				d.Store, err = d.Open(t.Context())
				if err != nil {
					t.Fatal(err)
				}

				ds = d.Store.(restoreStore)
				if err := ds.RestoreCanonicalBackup(
					t.Context(),
					bytes.NewReader(full),
					1,
				); err != nil {
					t.Fatal("valid whole retry after mismatch: ", err)
				}

				got, found, err := d.Store.(memory.ScopeEraseStore).LoadScopeErase(
					t.Context(),
					request.Scope,
					request.OperationID,
				)
				if err != nil || !found {
					t.Fatalf("restored original journal: %+v %v", got, err)
				}

				gotBytes, err := canonicalsql.Encode(canonicalsql.EraseJournal, got)
				if err != nil || !bytes.Equal(want, gotBytes) {
					t.Fatalf("restored journal changed: %+v %v", got, err)
				}
			})
		})
	}
}

//nolint:cyclop,gocognit,funlen // One full/delta chain checks all retained authority tombstones together.
func TestSQLRestoreRestrictiveTombstonesAndImmutableProof(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		p := newArchiveProbe(t, f)
		s := f.Store.(restoreStore)
		scope := memory.Scope{Key: "test:restriction", Kind: memory.ScopeKindPersonal}
		certificateID := "boyan:v1:boundary:" + strings.Repeat("a", 64)
		keys := []canonicalsql.Key{
			{Family: canonicalsql.DeniedSource, Scope: scope, Parts: [][]byte{[]byte("source")}},
			{
				Family: canonicalsql.DeniedRevision,
				Scope:  scope,
				Parts:  [][]byte{[]byte("revision")},
			},
			{Family: canonicalsql.ErasedBlob, Parts: [][]byte{[]byte("blob")}},
			{
				Family: canonicalsql.SourcePrivacyScope,
				Scope:  scope,
				Parts:  [][]byte{[]byte("coverage")},
			},
			{
				Family: canonicalsql.BoundaryCertificate,
				Scope:  scope,
				Parts:  [][]byte{[]byte(certificateID)},
			},
		}
		values := []any{
			map[string]bool{"denied": true},
			map[string]bool{"denied": true},
			true,
			map[string]any{"version": 1, "scope": scope, "incomplete": true},
			memory.CanonicalBoundaryCertificate{
				Version:     1,
				ExportID:    certificateID,
				Fingerprint: strings.Repeat("b", 64),
			},
		}

		if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for i, key := range keys {
				encoded, err := canonicalsql.Encode(key.Family, values[i])
				if err != nil {
					return err
				}

				if err := tx.Put(t.Context(), key, encoded); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		base, _, full := backupFrames(t, s, 0)
		d := restoreDestination(t, f.Backend)
		ds := d.Store.(restoreStore)

		dp := newArchiveProbe(t, d)
		if err := ds.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for _, key := range keys {
				if err := tx.Delete(t.Context(), key); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		_, _, delta := backupFrames(t, s, base)
		if err := ds.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		if err := dp.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
			for _, key := range keys {
				if _, err := tx.Get(t.Context(), key); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal("restriction deletion applied: ", err)
		}

		frames := decodeArchive(t, full)
		for _, frame := range frames {
			if frame["family"] == "boundary-certificate" {
				value, err := canonicalsql.Encode(
					canonicalsql.BoundaryCertificate,
					memory.CanonicalBoundaryCertificate{
						Version:     1,
						ExportID:    certificateID,
						Fingerprint: strings.Repeat("c", 64),
					},
				)
				if err != nil {
					t.Fatal(err)
				}

				frame["envelope"] = value
			}
		}
		// Use the current cursor in a new full artifact so lineage rejection
		// cannot mask the immutable-certificate conflict.
		frames[0]["end"] = float64(base + 1)
		if err := ds.RestoreCanonicalBackup(
			t.Context(),
			bytes.NewReader(encodeRestoreFrames(t, frames)),
			1,
		); err == nil {
			t.Fatal("immutable proof overwritten")
		}

		for _, frame := range frames {
			if frame["family"] == "boundary-certificate" {
				value, err := canonicalsql.Encode(canonicalsql.BoundaryCertificate, values[4])
				if err != nil {
					t.Fatal(err)
				}

				frame["envelope"] = value
			}
		}

		if err := ds.RestoreCanonicalBackup(
			t.Context(),
			bytes.NewReader(encodeRestoreFrames(t, frames)),
			1,
		); err != nil {
			t.Fatal("valid retry after immutable conflict: ", err)
		}
	})
}

func TestSQLRestoreClockOverflowLeavesDurableFence(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(restoreStore)
		_, _, data := backupFrames(t, s, 0)

		p := newArchiveProbe(t, f)
		if err := p.exec(
			"UPDATE "+p.table("store_meta")+" SET mutation_clock=$1",
			canonicalsql.Uint64(math.MaxUint64),
		); err != nil {
			t.Fatal(err)
		}

		if err := s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1); err == nil {
			t.Fatal("maximum local clock wrapped")
		}

		if s.VerifyCanonicalIntegrity(t.Context()) == nil {
			t.Fatal("overflow released fence")
		}
	})
}

func TestSQLRestoreProofCorruptionMaintenanceRoutes(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(restoreStore)
		_, frames, _ := backupFrames(t, s, 0)
		id := "boyan:v1:boundary:" + strings.Repeat("a", 64)

		encoded, err := canonicalsql.Encode(
			canonicalsql.BoundaryCertificate,
			map[string]any{"version": 2, "export_id": id, "fingerprint": strings.Repeat("b", 64)},
		)
		if err != nil {
			t.Fatal(err)
		}

		record := map[string]any{
			"kind":       "record",
			"family":     "boundary-certificate",
			"components": [][]byte{[]byte("test:proof"), []byte("personal"), []byte(id)},
			"sequence":   1,
			"envelope":   encoded,
		}
		frames[0]["end"] = 1

		frames = []map[string]any{frames[0], record, frames[1]}
		if err := s.RestoreCanonicalBackup(
			t.Context(),
			bytes.NewReader(encodeRestoreFrames(t, frames)),
			1,
		); err == nil {
			t.Fatal("restore accepted corrupt proof")
		}
		// Separate unfenced native corruption proves writer and verifier compose
		// the same proof validation, independent of the restore fence.
		d := restoreDestination(t, f.Backend)
		ds := d.Store.(restoreStore)

		dp := newArchiveProbe(t, d)
		if err := dp.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			return tx.Put(
				t.Context(),
				canonicalsql.Key{
					Family: canonicalsql.BoundaryCertificate,
					Scope:  memory.Scope{Key: "test:proof", Kind: memory.ScopeKindPersonal},
					Parts:  [][]byte{[]byte(id)},
				},
				encoded,
			)
		}); err != nil {
			t.Fatal(err)
		}

		if ds.VerifyCanonicalIntegrity(t.Context()) == nil {
			t.Fatal("integrity accepted corrupt proof")
		}

		var out bytes.Buffer
		if _, err := ds.WriteCanonicalBackup(t.Context(), &out, 0); err == nil || out.Len() != 0 {
			t.Fatal("writer exported corrupt proof")
		}
	})
}
