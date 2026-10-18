package archive_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

//nolint:cyclop,gocognit,funlen // Every fault includes reopen and its loading/validated recovery route.
func TestSQLLogicalNativeTransitionFaults(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		putArchiveRows(
			t,
			newArchiveProbe(t, source),
			[]canonicalsql.Key{
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a")}},
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("b")}},
			},
			"data",
		)

		data := logicalBytes(t, source.Store.(logicalStore))
		for _, stage := range []string{"loading", "batch", "validated", "privacy", "remove"} {
			t.Run(stage, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)
				ds := d.Store.(logicalStore)
				remove := installRestoreFault(t, newArchiveProbe(t, d), stage)
				read := false

				r := &restoreFirstRead{Reader: bytes.NewReader(data), read: func() { read = true }}
				if _, err := ds.ImportCanonicalLogicalWithOptions(
					t.Context(),
					r,
					archive.CanonicalLogicalImportOptions{BatchSize: 1},
				); err == nil {
					t.Fatal("fault ignored")
				}

				remove()

				if stage == "loading" && read {
					t.Fatal("read before durable loading")
				}

				if stage != "loading" && ds.VerifyCanonicalIntegrity(t.Context()) == nil {
					t.Fatal("failed import served")
				}

				if err := d.Store.Close(); err != nil {
					t.Fatal(err)
				}

				var err error

				d.Store, err = d.Open(t.Context())
				if err != nil {
					t.Fatal(err)
				}

				ds = d.Store.(logicalStore)

				switch stage {
				case "privacy":
					err = ds.PrepareSourcePrivacy(t.Context())
				case "remove":
					err = ds.ResumeRestoreBeforeEraseRecovery(t.Context())
				default:
					if stage != "loading" && ds.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil {
						t.Fatal("loading resumed")
					}

					err = ds.ImportCanonicalLogical(t.Context(), bytes.NewReader(data))
				}

				if err != nil {
					t.Fatal(err)
				}

				if err := ds.VerifyCanonicalIntegrity(t.Context()); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

//nolint:cyclop,gocognit,funlen // Original malformed proofs exercise scan, import and separate unfenced export.
func TestSQLLogicalProofRecordValidationAndMaintenanceRoutes(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)
		empty := logicalFrames(t, logicalBytes(t, s))
		id := "boyan:v1:boundary:" + strings.Repeat("a", 64)
		scope := memory.Scope{Key: "test:logical-proof", Kind: memory.ScopeKindPersonal}

		for _, fault := range []string{
			"certificate-version", "certificate-key", "certificate-digest", "certificate-content",
			"marker-version", "marker-scope", "marker-key", "grounding-version",
			"unversioned-parents", "missing-provenance",
		} {
			t.Run(fault, func(t *testing.T) {
				family, keyID := canonicalsql.BoundaryCertificate, id
				payload := map[string]any{"version": 1, "export_id": id, "fingerprint": strings.Repeat("b", 64)}

				var validGroundingEnvelope []byte

				switch fault {
				case "certificate-version":
					payload["version"] = 2
				case "certificate-key":
					keyID += "x"
				case "certificate-digest":
					payload["fingerprint"] = "PRIVATE invalid digest"
				case "certificate-content":
					payload["boundary"] = "PRIVATE boundary text"
				case "marker-version", "marker-scope", "marker-key":
					family, keyID = canonicalsql.BoundaryCapability, "v1"

					payload = map[string]any{"version": 1, "scope": scope}
					if fault == "marker-version" {
						payload["version"] = 2
					}

					if fault == "marker-scope" {
						payload["scope"] = memory.Scope{Key: "test:other", Kind: memory.ScopeKindPersonal}
					}

					if fault == "marker-key" {
						keyID = "unknown"
					}
				default:
					family, keyID = canonicalsql.Revision, "revision"

					revision := memory.MemoryRevision{
						SchemaVersion: memory.MemorySchemaVersion,
						RevisionID:    keyID,
						ItemID:        "item",
						Revision:      1,
						Temporal:      memory.Temporal{ObservedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
						Evidence: []memory.EvidenceRef{
							{SourceID: "source", MessageID: "message", Role: memory.MessageRoleUser,
								StartByte: 1, EndByte: 2, AssertionMode: memory.AssertionModeUser},
						},
						Sensitivity:            memory.SensitivityStandard,
						Retention:              memory.RetentionClassStandard,
						Payload:                memory.PayloadRef{ID: "payload", Digest: "digest", ByteSize: 1},
						GroundingBoundsVersion: 1,
						GroundingParents:       []string{"parent"},
						Parents:                []string{"parent"},
					}
					if err := revision.Validate(); err != nil {
						t.Fatal("invalid grounding baseline: ", err)
					}

					var err error

					validGroundingEnvelope, err = canonicalsql.Encode(family, revision)
					if err != nil {
						t.Fatal(err)
					}

					body, err := json.Marshal(revision)
					if err != nil {
						t.Fatal(err)
					}

					payload = nil
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatal(err)
					}

					if fault == "grounding-version" {
						payload["grounding_bounds_version"] = 2
					}

					if fault == "unversioned-parents" {
						payload["grounding_bounds_version"] = 0
					}

					if fault == "missing-provenance" {
						payload["grounding_bounds_version"] = 1
						delete(payload, "parents")
					}
				}

				value, err := canonicalsql.Encode(family, payload)
				if err != nil {
					t.Fatal(err)
				}

				empty[0]["end"] = 1
				frames := []map[string]any{
					empty[0],
					{
						"kind":        "record",
						"record_type": string(family),
						"components": []string{
							base64.StdEncoding.EncodeToString([]byte(scope.Key)),
							base64.StdEncoding.EncodeToString([]byte(scope.Kind)),
							base64.StdEncoding.EncodeToString([]byte(keyID)),
						},
						"envelope": value,
						"sequence": 1,
					},
					empty[len(empty)-1],
				}

				data := encodeLogicalFrames(t, frames)

				for _, scan := range []func(context.Context, io.Reader, int,
					func([]archive.CanonicalLogicalExportRecord) error,
				) (archive.CanonicalLogicalExportSummary, error){
					sqlite.ScanCanonicalLogicalExport, postgres.ScanCanonicalLogicalExport,
				} {
					if validGroundingEnvelope != nil {
						frames[1]["envelope"] = validGroundingEnvelope
						good := encodeLogicalFrames(t, frames)
						frames[1]["envelope"] = value

						if summary, err := scan(
							t.Context(),
							bytes.NewReader(good),
							1,
							nil,
						); err != nil ||
							summary.RecordCount != 1 {
							t.Fatalf("valid grounding baseline rejected by per-record scan: %+v %v", summary, err)
						}
					}

					if _, err := scan(
						t.Context(),
						bytes.NewReader(data),
						1,
						nil,
					); err == nil ||
						strings.Contains(err.Error(), "PRIVATE") {
						t.Fatalf("proof accepted/leaked: %v", err)
					}
				}

				d := restoreDestination(t, f.Backend)
				if err := d.Store.(logicalStore).ImportCanonicalLogical(
					t.Context(),
					bytes.NewReader(data),
				); err == nil {
					t.Fatal("logical import accepted malformed proof")
				}
				// A separate, unfenced store proves export's proof checks rather
				// than merely observing the previous import's durable fence.
				d = restoreDestination(t, f.Backend)

				p := newArchiveProbe(t, d)
				if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
					return tx.Put(
						t.Context(),
						canonicalsql.Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(keyID)}},
						value,
					)
				}); err != nil {
					t.Fatal(err)
				}

				var out bytes.Buffer
				if err := d.Store.(logicalStore).ExportCanonicalLogical(
					t.Context(),
					&out,
				); err == nil ||
					out.Len() != 0 {
					t.Fatal("logical export accepted malformed proof")
				}
			})
		}
	})
}

func TestSQLLogicalInputContractAndScanCancellation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)

		data := logicalBytes(t, s)
		if err := s.ExportCanonicalLogical(
			t.Context(),
			logicalFailWriter{},
		); err == nil ||
			strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("writer diagnostics leaked: %v", err)
		}

		var (
			nilReader *bytes.Reader
			nilWriter *bytes.Buffer
		)

		for _, r := range []io.Reader{nil, nilReader} {
			if s.ImportCanonicalLogical(t.Context(), r) == nil {
				t.Fatal("nil reader accepted")
			}
		}

		if s.ExportCanonicalLogical(t.Context(), nilWriter) == nil || s.ExportCanonicalLogical(nil, io.Discard) == nil {
			t.Fatal("invalid export accepted")
		}

		if _, err := s.ImportCanonicalLogicalWithOptions(
			t.Context(),
			bytes.NewReader(data),
			archive.CanonicalLogicalImportOptions{BatchSize: 4097},
		); err == nil {
			t.Fatal("oversize batch accepted")
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := s.ImportCanonicalLogical(ctx, bytes.NewReader(data)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel lost: %v", err)
		}
		// Reentrant callbacks must use a separate cancellable context. Admission
		// cannot be recursively acquired, and a canceled attempt cannot certify.
		ctx, cancel = context.WithCancel(t.Context())
		cancel()

		if _, err := s.ImportCanonicalLogicalWithOptions(
			ctx,
			bytes.NewReader(data),
			archive.CanonicalLogicalImportOptions{BatchSize: 4097},
		); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf("context-first contract lost: %v", err)
		}
	})
}

type logicalFailWriter struct{}

func (logicalFailWriter) Write([]byte) (int, error) { return 0, errors.New("PRIVATE destination") }

type logicalBoundReader struct{ count int }

func (r *logicalBoundReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}

	r.count += len(p)

	return len(p), nil
}

//nolint:cyclop,gocognit,funlen // Summary ordering, footer failure, cancellation and bounded reads share a stream.
func TestSQLLogicalScanSummaryBatchAndBounds(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)
		p := newArchiveProbe(t, f)
		want := []memory.Scope{
			{Key: "test:a", Kind: memory.ScopeKindGroup},
			{Key: "test:a", Kind: memory.ScopeKindPersonal},
			{Key: "test:z", Kind: memory.ScopeKindPersonal},
		}

		if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
			for i := len(want) - 1; i >= 0; i-- {
				data, err := canonicalsql.Encode(
					canonicalsql.ScopeState,
					memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: want[i]},
				)
				if err != nil {
					return err
				}

				if err := tx.Put(
					t.Context(),
					canonicalsql.Key{Family: canonicalsql.ScopeState, Scope: want[i]},
					data,
				); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		data := logicalBytes(t, s)

		for _, batch := range []int{-1, 1, 4096} {
			var count int

			summary, err := s.ScanCanonicalLogicalExport(
				t.Context(),
				bytes.NewReader(data),
				batch,
				func(records []archive.CanonicalLogicalExportRecord) error {
					count += len(records)
					if batch == 1 && len(records) != 1 {
						t.Error("unbounded batch")
					}

					return nil
				},
			)
			if err != nil || count != 3 || len(summary.Scopes) != 3 {
				t.Fatalf("summary=%+v %v", summary, err)
			}

			for i := range want {
				if summary.Scopes[i] != want[i] {
					t.Fatalf("scope order=%+v", summary.Scopes)
				}
			}
		}

		bad := bytes.Replace(data, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)

		summary, err := s.ScanCanonicalLogicalExport(t.Context(), bytes.NewReader(bad), 1, nil)
		if err == nil || summary.RecordCount != 0 || len(summary.Scopes) != 0 || summary.SHA256 != "" {
			t.Fatalf("failed scan returned trusted summary %+v %v", summary, err)
		}

		ctx, cancel := context.WithCancel(t.Context())

		_, err = s.ScanCanonicalLogicalExport(
			ctx,
			bytes.NewReader(data),
			1,
			func([]archive.CanonicalLogicalExportRecord) error {
				cancel()

				return nil
			},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("scan cancellation lost: %v", err)
		}

		r := &logicalBoundReader{}
		if _, err := s.ScanCanonicalLogicalExport(
			t.Context(),
			r,
			1,
			nil,
		); err == nil ||
			r.count > archive.MaxLogicalLineBytes+32*1024 {
			t.Fatalf("unbounded line read count=%d err=%v", r.count, err)
		}
	})
}

func TestSQLLogicalClockOverflowAndReentrantAdmission(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)
		p := newArchiveProbe(t, f)
		putArchiveRows(
			t,
			p,
			[]canonicalsql.Key{{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("blob")}}},
			"body",
		)
		data := logicalBytes(t, s)

		_, err := s.ImportCanonicalLogicalWithOptions(
			t.Context(),
			bytes.NewReader(data),
			archive.CanonicalLogicalImportOptions{
				BatchSize: 1,
				Progress: func(archive.CanonicalLogicalImportProgress) error {
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
					defer cancel()

					err := s.PrepareSourcePrivacy(ctx)
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("recursive admission bypassed held owner: %v", err)
					}

					return errors.New("stop callback")
				},
			},
		)
		if err == nil {
			t.Fatal("callback failure ignored")
		}

		if err := s.ImportCanonicalLogical(t.Context(), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}

		d := restoreDestination(t, f.Backend)

		dp := newArchiveProbe(t, d)
		if err := dp.exec(
			"UPDATE "+dp.table("store_meta")+" SET mutation_clock=$1",
			canonicalsql.Uint64(math.MaxUint64),
		); err != nil {
			t.Fatal(err)
		}

		if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(data)); err == nil {
			t.Fatal("local clock wrapped")
		}

		if d.Store.(logicalStore).VerifyCanonicalIntegrity(t.Context()) == nil {
			t.Fatal("overflow released fence")
		}
	})
}
