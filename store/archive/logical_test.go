package archive_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type logicalStore interface {
	restoreStore
	archive.CanonicalLogicalExporter
	archive.CanonicalLogicalImporter
	archive.CanonicalLogicalScanner
}

func logicalBytes(t *testing.T, s logicalStore) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := s.ExportCanonicalLogical(t.Context(), &out); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func logicalFrames(t *testing.T, data []byte) []map[string]any {
	t.Helper()

	var result []map[string]any

	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
		var frame map[string]any
		if err := json.Unmarshal(line, &frame); err != nil {
			t.Fatal(err)
		}

		result = append(result, frame)
	}

	return result
}

func encodeLogicalFrames(t *testing.T, frames []map[string]any) []byte {
	t.Helper()

	var out bytes.Buffer

	digest := sha256.New()

	for i, frame := range frames {
		if i == len(frames)-1 {
			frame["record_count"] = len(frames) - 2
			frame["sha256"] = hex.EncodeToString(digest.Sum(nil))
		}

		line, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}

		line = append(line, '\n')
		out.Write(line)

		if i != len(frames)-1 {
			digest.Write(line)
		}
	}

	return out.Bytes()
}

// A malformed header or individual record must fail both free scanners and
// actual import, leaving an unavailable store that accepts a full good retry.
//
//nolint:cyclop,gocognit,gocyclo,funlen // Each malformed stream needs rejection, reopen and good retry.
func TestSQLLogicalMalformedStreamRetry(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)
		putArchiveRows(
			t,
			newArchiveProbe(t, f),
			[]canonicalsql.Key{
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("one")}},
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("two")}},
			},
			"body",
		)

		good := logicalBytes(t, s)
		for _, fault := range []string{
			"format", "backend", "schema", "privacy", "proof", "uuid", "base", "mode", "end", "unknown", "family",
			"arity", "key", "suffix", "duplicate", "order", "checksum", "count", "truncated", "trailing",
			"duplicate-json", "line-limit", "value-limit", "bad-envelope",
		} {
			t.Run(fault, func(t *testing.T) {
				frames := logicalFrames(t, good)

				switch fault {
				case "format":
					frames[0]["schema_version"] = "boyan-logical-export/v1"
				case "backend":
					frames[0]["backend"] = "foreign"
				case "schema":
					frames[0]["store_schema_version"] = 99
				case "privacy":
					frames[0]["privacy_protocol"] = 99
				case "proof":
					frames[0]["proof_protocol"] = 99
				case "uuid":
					frames[0]["source_store_uuid"] = "PRIVATE"
				case "base":
					frames[0]["base"] = 1
				case "mode":
					frames[0]["mode"] = "incremental"
				case "end":
					frames[0]["end"] = 0
				case "unknown":
					frames[1]["row_id"] = 1
				case "family":
					frames[1]["record_type"] = "restore_control"
				case "arity":
					frames[1]["components"] = []string{"b25l", "dHdv"}
				case "key":
					frames[1]["components"] = []string{
						base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'a'}, math.MaxUint16+1)),
					}
				case "suffix":
					frames[1]["suffix"] = "YQ=="
				case "duplicate":
					frames[2] = frames[1]
				case "order":
					frames[1], frames[2] = frames[2], frames[1]
				case "value-limit":
					frames[1]["envelope"] = strings.Repeat("A", (archive.MaxValueBytes/3+1)*4)
				case "bad-envelope":
					frames[1]["envelope"] = "UFJJVkFURQ=="
				}

				bad := encodeLogicalFrames(t, frames)

				switch fault {
				case "checksum":
					bad = bytes.Replace(bad, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)
				case "count":
					bad = bytes.Replace(bad, []byte(`"record_count":2`), []byte(`"record_count":3`), 1)
				case "truncated":
					bad = bad[:len(bad)-1]
				case "trailing":
					bad = append(bad, '\n')
				case "duplicate-json":
					bad = bytes.Replace(bad, []byte(`"kind":"header"`), []byte(`"kind":"header","kind":"header"`), 1)
				case "line-limit":
					bad = bytes.Repeat([]byte{' '}, archive.MaxLogicalLineBytes+1)
				}

				for _, scan := range []func(context.Context, io.Reader, int,
					func([]archive.CanonicalLogicalExportRecord) error,
				) (archive.CanonicalLogicalExportSummary, error){
					sqlite.ScanCanonicalLogicalExport, postgres.ScanCanonicalLogicalExport,
				} {
					if _, err := scan(
						t.Context(),
						bytes.NewReader(bad),
						1,
						nil,
					); err == nil ||
						strings.Contains(err.Error(), "PRIVATE") {
						t.Fatalf("scanner accepted/leaked malformed %s: %v", fault, err)
					}
				}

				d := restoreDestination(t, f.Backend)

				ds := d.Store.(logicalStore)
				if err := ds.ImportCanonicalLogical(t.Context(), bytes.NewReader(bad)); err == nil {
					t.Fatal("invalid import accepted")
				}

				if ds.PrepareSourcePrivacy(t.Context()) == nil {
					t.Fatal("loading trusted")
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
				if ds.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil {
					t.Fatal("loading resumed")
				}

				if err := ds.ImportCanonicalLogical(t.Context(), bytes.NewReader(good)); err != nil {
					t.Fatal("complete retry", err)
				}
			})
		}
	})
}

func TestSQLLogicalBoundsAndExactKeys(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		p := newArchiveProbe(t, f)
		s := f.Store.(logicalStore)
		scope := memory.Scope{Key: "test:" + strings.Repeat("s", math.MaxUint16-5), Kind: memory.ScopeKindPersonal}
		keys := []canonicalsql.Key{
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("10187")}},
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("159793")}},
			{
				Family: canonicalsql.ProvenanceForward,
				Scope:  scope,
				Parts:  [][]byte{bytes.Repeat([]byte{'a'}, math.MaxUint16), bytes.Repeat([]byte{'b'}, math.MaxUint16)},
			},
			{
				Family: canonicalsql.Payload,
				Parts:  [][]byte{[]byte("a\x00|\xff")},
			},
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a|\x00\xff")}},
		}
		putArchiveRows(t, p, keys, "original")
		good := logicalBytes(t, s)
		d := restoreDestination(t, f.Backend)

		ds := d.Store.(logicalStore)
		if _, err := ds.ImportCanonicalLogicalWithOptions(
			t.Context(),
			bytes.NewReader(good),
			archive.CanonicalLogicalImportOptions{BatchSize: 1},
		); err != nil {
			t.Fatal(err)
		}

		dp := newArchiveProbe(t, d)
		if err := dp.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
			for _, key := range keys {
				if _, err := tx.Get(t.Context(), key); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// This valid persistence envelope cannot fit in a logical base64 line.
		putArchiveRows(
			t,
			p,
			[]canonicalsql.Key{{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("large")}}},
			strings.Repeat("x", 3*1024*1024+1024),
		)

		if err := s.ExportCanonicalLogical(t.Context(), io.Discard); err == nil {
			t.Fatal("self-unimportable line exported")
		}
	})
}

//nolint:cyclop,gocognit,funlen // Check callback, committed state, fence, reopen and complete retry together.
func TestSQLLogicalProgressFailureAndCancellation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(logicalStore)
		putArchiveRows(
			t,
			newArchiveProbe(t, f),
			[]canonicalsql.Key{
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a")}},
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("b")}},
			},
			"body",
		)

		data := logicalBytes(t, s)
		for _, cancelProgress := range []bool{false, true} {
			d := restoreDestination(t, f.Backend)
			ds := d.Store.(logicalStore)
			ctx, cancel := context.WithCancel(t.Context())

			var progress uint64

			_, err := ds.ImportCanonicalLogicalWithOptions(
				ctx,
				bytes.NewReader(data),
				archive.CanonicalLogicalImportOptions{
					BatchSize: 1,
					Progress: func(p archive.CanonicalLogicalImportProgress) error {
						progress = p.ImportedRecords

						if cancelProgress {
							cancel()

							return ctx.Err()
						}

						return errors.New("PRIVATE callback")
					},
				},
			)

			cancel()

			if err == nil || progress != 1 || (cancelProgress && !errors.Is(err, context.Canceled)) {
				t.Fatalf("callback=%v progress=%d", err, progress)
			}
			// First batch was committed, but readiness remains fenced.
			var count int
			if d.Database.Postgres != nil {
				err = d.Database.Postgres.QueryRow(t.Context(), "SELECT count(*) FROM boyan_canonical.payload").
					Scan(&count)
			} else {
				db, openErr := sql.Open("sqlite", d.Database.DSN)
				if openErr != nil {
					t.Fatal(openErr)
				}

				err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM payload").Scan(&count)
				if closeErr := db.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}

			if err != nil || count != 1 {
				t.Fatalf("progress before durable commit count=%d err=%v", count, err)
			}

			if ds.PrepareSourcePrivacy(t.Context()) == nil {
				t.Fatal("callback published readiness")
			}

			if err := d.Store.Close(); err != nil {
				t.Fatal(err)
			}

			d.Store, err = d.Open(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
		}
	})
}

//nolint:cyclop // Public roundtrip includes exact scan and durable batch progress assertions.
func TestSQLLogicalRoundtripAndDurableProgress(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s, ok := f.Store.(logicalStore)
		if !ok {
			t.Fatal("SQL owner lacks actual logical export/scan/import")
		}

		p := newArchiveProbe(t, f)
		key := canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("logical-blob\x00\xff")}}
		putArchiveRows(t, p, []canonicalsql.Key{key}, "preserved")

		var out bytes.Buffer
		if err := s.ExportCanonicalLogical(t.Context(), &out); err != nil {
			t.Fatal(err)
		}

		var scanned uint64

		summary, err := s.ScanCanonicalLogicalExport(
			t.Context(),
			bytes.NewReader(out.Bytes()),
			1,
			func(records []archive.CanonicalLogicalExportRecord) error {
				scanned += uint64(len(records))

				return nil
			},
		)
		if err != nil || scanned == 0 || summary.RecordCount != scanned {
			t.Fatalf("scan=%+v %v count=%d", summary, err, scanned)
		}

		var progress uint64

		got, err := s.ImportCanonicalLogicalWithOptions(
			t.Context(),
			bytes.NewReader(out.Bytes()),
			archive.CanonicalLogicalImportOptions{
				BatchSize: 1,
				Progress: func(p archive.CanonicalLogicalImportProgress) error {
					if p.ImportedRecords <= progress {
						t.Error("progress did not advance")
					}

					progress = p.ImportedRecords

					return nil
				},
			},
		)
		if err != nil || got.RecordCount != scanned || progress != scanned {
			t.Fatalf("import=%+v %v progress=%d", got, err, progress)
		}

		if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}
