package archive_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

func restoreDestination(t *testing.T, backend canonicalsqltest.Backend) *storefixture.Fixture {
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

func encodeRestoreFrames(t *testing.T, frames []map[string]any) []byte {
	t.Helper()

	var output bytes.Buffer

	h := sha256.New()

	for i, frame := range frames {
		if i == len(frames)-1 {
			frame["record_count"] = len(frames) - 2
			frame["sha256"] = hex.EncodeToString(h.Sum(nil))
		}

		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}

		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(len(data)))
		output.Write(prefix[:])
		output.Write(data)

		if i != len(frames)-1 {
			h.Write(prefix[:])
			h.Write(data)
		}
	}

	return output.Bytes()
}

//nolint:cyclop,gocognit,funlen // One chain checks exact keys, local clocks, deletion and reinsertion.
func TestSQLRestoreFullTwoDeltasExactKeysAndLocalCursor(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		s := source.Store.(restoreStore)
		p := newArchiveProbe(t, source)
		keys := []canonicalsql.Key{
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("10187")}},
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("159793")}},
			{
				Family: canonicalsql.ProvenanceForward,
				Scope: memory.Scope{
					Key:  "test:" + strings.Repeat("x", math.MaxUint16-5),
					Kind: memory.ScopeKindPersonal,
				},
				Parts: [][]byte{
					bytes.Repeat([]byte{'a'}, math.MaxUint16),
					bytes.Repeat([]byte{'b'}, math.MaxUint16),
				},
			},
		}
		putArchiveRows(t, p, keys, "original")
		base, _, full := backupFrames(t, s, 0)
		d := restoreDestination(t, source.Backend)
		ds := d.Store.(restoreStore)
		dp := newArchiveProbe(t, d)
		putArchiveRows(
			t,
			dp,
			[]canonicalsql.Key{{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("local")}}},
			"local",
		)

		_, _, _ = backupFrames(t, ds, 0)
		if err := ds.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 1); err != nil {
			t.Fatal(err)
		}

		local, _, _ := backupFrames(t, ds, 0)
		if local <= base {
			t.Fatal("source cursor copied over destination clock")
		}

		if err := p.c.Update(
			t.Context(),
			func(tx *canonicalsql.Tx) error { return tx.Delete(t.Context(), keys[0]) },
		); err != nil {
			t.Fatal(err)
		}

		end, _, delta := backupFrames(t, s, base)
		if err := ds.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		if err := dp.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
			_, err := tx.Get(t.Context(), keys[0])
			if err == nil {
				t.Fatal("delete lost")
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		putArchiveRows(t, p, keys[:1], "reinserted")

		_, _, delta = backupFrames(t, s, end)
		if err := d.Store.Close(); err != nil {
			t.Fatal(err)
		}

		var err error

		d.Store, err = d.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		ds = d.Store.(restoreStore)
		if err := ds.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 1); err != nil {
			t.Fatal(err)
		}

		_, frames, _ := backupFrames(t, ds, local)
		if len(frames) < 3 {
			t.Fatal("restored effects missing from destination incremental")
		}

		if err := dp.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
			for i, key := range keys {
				data, err := tx.Get(t.Context(), key)
				if err != nil {
					return err
				}

				var value string
				if err := canonicalsql.Decode(key.Family, data, &value); err != nil {
					return err
				}

				want := "original"
				if i == 0 {
					want = "reinserted"
				}

				if value != want {
					t.Fatalf("key %d aliased: %s", i, value)
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

//nolint:cyclop,gocognit,gocyclo,funlen // Every independent invalid artifact needs a reopened valid retry.
func TestSQLRestoreRejectsUntrustedFramingAndAllowsRetry(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		p := newArchiveProbe(t, source)
		putArchiveRows(
			t,
			p,
			[]canonicalsql.Key{
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("one")}},
				{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("two")}},
			},
			"data",
		)

		_, _, good := backupFrames(t, source.Store.(backupStore), 0)
		for _, kind := range []string{"checksum", "count", "truncated", "trailing", "oversize-frame", "foreign-backend",
			"schema", "base", "order", "duplicate", "duplicate-json", "unknown-field", "oversize-value", "oversize-key",
			"arity", "orphan-proof", "format", "footer-reader-error"} {
			t.Run(kind, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)
				ds := d.Store.(restoreStore)
				frames := decodeArchive(t, good)

				switch kind {
				case "format":
					frames[0]["format"] = "boyan-sql-logical/v1"
				case "foreign-backend":
					frames[0]["backend"] = "foreign"
				case "schema":
					frames[0]["schema_version"] = 99
				case "base":
					frames[0]["mode"] = "incremental"
					frames[0]["base"] = 1
				case "order":
					frames[1], frames[2] = frames[2], frames[1]
				case "duplicate":
					frames[2] = frames[1]
				case "unknown-field":
					frames[1]["private_row_id"] = 1
				case "oversize-value":
					frames[1]["envelope"] = strings.Repeat("A", (4*1024*1024/3+1)*4)
				case "oversize-key":
					frames[1]["components"] = []string{strings.Repeat("A", 87384)}
				case "arity":
					frames[1]["components"] = []string{"b25l", "dHdv"}
				case "orphan-proof":
					frames[1]["family"] = "source-privacy"
					frames[1]["components"] = [][]byte{
						[]byte("test:orphan"),
						[]byte("personal"),
						[]byte("orphan"),
					}

					value, err := canonicalsql.Encode(
						canonicalsql.SourcePrivacy,
						map[string]any{
							"version":          1,
							"coverage_version": 1,
							"scope": map[string]string{
								"key":  "test:orphan",
								"kind": "personal",
							},
							"source_id": "orphan",
							"payload": map[string]any{
								"id":        "blob",
								"digest":    strings.Repeat("a", 64),
								"byte_size": 1,
							},
							"state":     "unknown",
							"preparing": true,
						},
					)
					if err != nil {
						t.Fatal(err)
					}

					frames[1]["envelope"] = value
					frames = append(frames[:2], frames[len(frames)-1])
				}

				bad := encodeRestoreFrames(t, frames)

				switch kind {
				case "checksum":
					bad[len(bad)-4] ^= 1
				case "count":
					bad = bytes.Replace(
						bad,
						[]byte(`"record_count":2`),
						[]byte(`"record_count":9`),
						1,
					)
				case "truncated":
					bad = bad[:len(bad)-1]
				case "trailing":
					bad = append(bad, 0)
				case "oversize-frame":
					bad = []byte{0x10, 0, 0, 1}
				case "duplicate-json":
					first := int(binary.BigEndian.Uint32(bad))
					data := append([]byte(`{"kind":"header",`), bad[5:4+first]...)
					binary.BigEndian.PutUint32(bad[:4], uint32(len(data)))
					bad = append(append(bad[:4], data...), bad[4+first:]...)
				}

				var reader io.Reader = bytes.NewReader(bad)
				if kind == "footer-reader-error" {
					reader = io.MultiReader(reader, logicalFooterErrorReader{})
				}

				if err := ds.RestoreCanonicalBackup(
					t.Context(),
					reader,
					1,
				); err == nil {
					t.Fatal("invalid artifact accepted")
				} else if kind == "footer-reader-error" {
					code, class, ok := memory.ClassifyError(err)
					if !ok || code != memory.CodeStoreFailure || class != memory.ErrorClassRetryable ||
						strings.Contains(err.Error(), "PRIVATE") {
						t.Errorf("footer reader failure lost redacted retry classification: %v", err)
					}
				}

				if ds.PrepareSourcePrivacy(t.Context()) == nil {
					t.Fatal("failed loading prepared")
				}

				if _, _, err := d.Store.(memory.CanonicalScopeStatePager).
					ListCanonicalScopeStates(t.Context(), nil, 1); err == nil {
					t.Fatal("ordinary read allowed during loading")
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
				if ds.PrepareSourcePrivacy(t.Context()) == nil ||
					ds.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil {
					t.Fatal("loading restore trusted across reopen")
				}

				if _, _, err := d.Store.(memory.CanonicalScopeStatePager).
					ListCanonicalScopeStates(t.Context(), nil, 1); err == nil {
					t.Fatal("ordinary read allowed during loading after reopen")
				}

				if err := ds.RestoreCanonicalBackup(
					t.Context(),
					bytes.NewReader(good),
					1,
				); err != nil {
					t.Fatalf("valid retry: %v", err)
				}
			})
		}
	})
}
