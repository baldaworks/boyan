package archive_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type archiveProbe struct {
	c     *canonicalsql.Connection
	exec  func(string, ...any) error
	table func(string) string
}

func newArchiveProbe(t *testing.T, f *storefixture.Fixture) archiveProbe {
	t.Helper()

	if f.Database.Postgres != nil {
		c := f.Database.Postgres

		return archiveProbe{
			canonicalsql.NewPostgres(c),
			func(q string, args ...any) error {
				_, err := c.Exec(t.Context(), q, args...)

				return err
			},
			func(s string) string { return "boyan_canonical." + s },
		}
	}

	db, err := sql.Open("sqlite", f.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	c, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Close() })

	return archiveProbe{
		canonicalsql.NewSQLite(c),
		func(q string, args ...any) error {
			_, err := c.ExecContext(t.Context(), q, args...)

			return err
		},
		func(s string) string { return s },
	}
}

func putArchiveRows(t *testing.T, p archiveProbe, keys []canonicalsql.Key, value string) {
	t.Helper()

	if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		for _, key := range keys {
			encoded, err := canonicalsql.Encode(key.Family, value)
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
}

func backupFrames(t *testing.T, s backupStore, since uint64) (uint64, []map[string]any, []byte) {
	t.Helper()

	var b bytes.Buffer

	end, err := s.WriteCanonicalBackup(t.Context(), &b, since)
	if err != nil {
		t.Fatal(err)
	}

	return end, decodeArchive(t, b.Bytes()), b.Bytes()
}

//nolint:cyclop,gocognit,funlen // Successive snapshots retain the exact cursor and erasure assertions.
func TestSQLBackupSuccessiveDeltasLongKeysAndErasure(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(backupStore)
		p := newArchiveProbe(t, f)
		scope := memory.Scope{
			Key:  "test:" + strings.Repeat("s", math.MaxUint16-5),
			Kind: memory.ScopeKindPersonal,
		}
		long := strings.Repeat("x", math.MaxUint16)
		keys := []canonicalsql.Key{
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a\x00|b")}},
			{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("a|\x00b")}},
			{
				Family: canonicalsql.ProjectionManifest,
				Scope:  scope,
				Parts:  [][]byte{[]byte(long), []byte(long)},
			},
		}
		putArchiveRows(t, p, keys, "old plaintext must disappear")

		base, full, _ := backupFrames(t, s, 0)
		if base != 1 || len(full) != 5 {
			t.Fatalf("full cursor=%d frames=%d", base, len(full))
		}

		putArchiveRows(t, p, keys[:1], "replacement")

		if err := p.c.Update(
			t.Context(),
			func(tx *canonicalsql.Tx) error { return tx.Delete(t.Context(), keys[1]) },
		); err != nil {
			t.Fatal(err)
		}

		end, delta, data := backupFrames(t, s, base)
		if end != 3 || len(delta) != 4 || delta[0]["base"] != float64(base) ||
			delta[0]["source_store_uuid"] != full[0]["source_store_uuid"] {
			t.Fatalf("delta=%v", delta)
		}

		if bytes.Contains(data, []byte("old plaintext")) {
			t.Fatal("old plaintext retained")
		}

		deleted := 0

		for _, row := range delta[1 : len(delta)-1] {
			if encoded, ok := row["envelope"].(string); ok {
				value, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil || bytes.Contains(value, []byte("old plaintext")) {
					t.Fatal("incremental retains old plaintext")
				}
			}

			if row["deleted"] == true {
				deleted++

				if row["envelope"] != nil {
					t.Fatal("tombstone retains value")
				}
			}
		}

		if deleted != 1 {
			t.Fatalf("deletions=%d", deleted)
		}

		putArchiveRows(t, p, keys[1:2], "reinserted")

		last, third, _ := backupFrames(t, s, end)
		if last != 4 || len(third) != 3 || third[1]["deleted"] != false {
			t.Fatalf("third=%v", third)
		}

		_, empty, _ := backupFrames(t, s, last)
		if len(empty) != 2 {
			t.Fatal("cursor repeated a record")
		}

		if _, err := s.WriteCanonicalBackup(t.Context(), io.Discard, last+1); err == nil {
			t.Fatal("future cursor accepted")
		}
	})
}

//nolint:cyclop,gocognit,funlen // Native corruption cases exercise the same fail-closed archive boundary.
func TestSQLBackupEnvelopeLimitCorruptionAndLoadingFence(t *testing.T) {
	for _, fault := range []string{"checksum", "oversize", "loading", "proof", "privacy", "outcome"} {
		t.Run(fault, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				p := newArchiveProbe(t, f)
				key := canonicalsql.Key{
					Family: canonicalsql.Payload,
					Parts:  [][]byte{[]byte("value")},
				}
				putArchiveRows(t, p, []canonicalsql.Key{key}, "value")

				switch fault {
				case "checksum":
					if err := p.exec(
						"UPDATE "+p.table("payload")+" SET envelope=$1",
						[]byte("corrupt"),
					); err != nil {
						t.Fatal(err)
					}
				case "oversize":
					query := "UPDATE payload SET envelope=zeroblob(4194305)"
					if f.Database.Postgres != nil {
						query = "UPDATE boyan_canonical.payload SET envelope=decode(repeat('00',4194305),'hex')"
					}

					if err := p.exec(query); err == nil {
						t.Fatal("native envelope cap accepted oversized value")
					}

					if _, err := canonicalsql.Encode(
						canonicalsql.Payload,
						strings.Repeat("x", 4*1024*1024),
					); err == nil {
						t.Fatal("encoded envelope cap accepted oversized value")
					}

					return
				case "loading":
					if err := p.exec(
						"INSERT INTO "+p.table(
							"restore_control",
						)+" (singleton,control) VALUES(1,$1)",
						[]byte("loading"),
					); err != nil {
						t.Fatal(err)
					}
				default:
					family := canonicalsql.Operation
					if fault == "proof" {
						family = canonicalsql.BoundaryCertificate
					}

					if fault == "privacy" {
						family = canonicalsql.SourcePrivacyReady
					}

					bad := canonicalsql.Key{Family: family}
					if family != canonicalsql.SourcePrivacyReady {
						bad.Scope = memory.Scope{
							Key:  "test:corrupt",
							Kind: memory.ScopeKindPersonal,
						}
						bad.Parts = [][]byte{[]byte("id")}
					}

					putArchiveRows(t, p, []canonicalsql.Key{bad}, "invalid authority")
				}

				var out bytes.Buffer
				if _, err := f.Store.(backupStore).WriteCanonicalBackup(
					t.Context(),
					&out,
					0,
				); err == nil ||
					out.Len() != 0 {
					t.Fatalf("%s admitted: %v bytes=%d", fault, err, out.Len())
				}
			})
		})
	}
}

func TestSQLBackupClockLimitAndWriterFailure(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(backupStore)

		p := newArchiveProbe(t, f)
		if err := p.exec(
			"UPDATE "+p.table("store_meta")+" SET mutation_clock=$1",
			canonicalsql.Uint64(math.MaxUint64-1),
		); err != nil {
			t.Fatal(err)
		}

		key := canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("max")}}
		putArchiveRows(t, p, []canonicalsql.Key{key}, "value")

		end, frames, _ := backupFrames(t, s, math.MaxUint64-1)
		if end != math.MaxUint64 || len(frames) != 3 {
			t.Fatalf("end=%d", end)
		}

		_, empty, _ := backupFrames(t, s, end)
		if len(empty) != 2 {
			t.Fatal("end cursor overflowed")
		}

		if err := p.c.Update(
			t.Context(),
			func(tx *canonicalsql.Tx) error { return tx.Delete(t.Context(), key) },
		); err == nil {
			t.Fatal("clock overflow accepted")
		}

		failure := errors.New("archive destination failed")
		if end, err := s.WriteCanonicalBackup(
			t.Context(),
			failureWriter{failure},
			0,
		); end != 0 ||
			!errors.Is(err, failure) {
			t.Fatalf("failed write=%d %v", end, err)
		}

		if _, err := s.WriteCanonicalBackup(
			t.Context(),
			shortWriter{},
			0,
		); !errors.Is(
			err,
			io.ErrShortWrite,
		) {
			t.Fatalf("short write=%v", err)
		}

		if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal("writer error leaked admission", err)
		}
	})
}

type failureWriter struct{ err error }

func (w failureWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestSQLBackupRejectsTypedNilStream(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("typed nil writer panicked: %v", recovered)
			}
		}()

		var stream *bytes.Buffer
		if _, err := f.Store.(backupStore).WriteCanonicalBackup(
			t.Context(),
			stream,
			0,
		); err == nil {
			t.Fatal("typed nil accepted")
		}
	})
}

func TestSQLBackupContextPrecedesStreamValidation(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(backupStore)
		canceled, cancel := context.WithCancel(t.Context())
		cancel()

		var typedNil *bytes.Buffer
		for _, writer := range []io.Writer{nil, typedNil, io.Discard} {
			_, err := s.WriteCanonicalBackup(canceled, writer, 0)

			code, _, _ := memory.ClassifyError(err)
			if code != memory.CodeTimeout || !memory.IsRetryable(err) ||
				!errors.Is(err, context.Canceled) {
				t.Errorf("canceled precedence: %v", err)
			}

			_, err = s.WriteCanonicalBackup(nil, writer, 0)

			code, _, _ = memory.ClassifyError(err)
			if code != memory.CodeInvalidDerived {
				t.Errorf("nil context: %v", err)
			}
		}

		err := s.VerifyCanonicalIntegrity(canceled)

		code, _, _ := memory.ClassifyError(err)
		if code != memory.CodeTimeout || !memory.IsRetryable(err) {
			t.Errorf("integrity cancellation: %v", err)
		}

		if err := s.VerifyCanonicalIntegrity(nil); err == nil {
			t.Fatal("nil integrity context accepted")
		}
	})
}

func TestSQLBackupDuplicateDeletedStampFailsClosed(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		p := newArchiveProbe(t, f)
		key := canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte("deleted")}}

		if err := p.c.Update(
			t.Context(),
			func(tx *canonicalsql.Tx) error { return tx.Delete(t.Context(), key) },
		); err != nil {
			t.Fatal(err)
		}

		err := p.exec(
			"INSERT INTO " + p.table(
				"row_stamp",
			) + " (family,binary_key,last_seq,deleted) SELECT family,binary_key,last_seq,deleted FROM " + p.table(
				"row_stamp",
			),
		)
		if f.Database.Postgres == nil {
			if err == nil {
				t.Fatal("SQLite duplicate constraint missing")
			}

			return
		}

		if err != nil {
			t.Fatal(err)
		}

		if err := f.Store.(backupStore).VerifyCanonicalIntegrity(t.Context()); err == nil {
			t.Fatal("duplicate deleted stamp accepted")
		}
	})
}

type blockingWriter struct {
	entered chan struct{}
	ctx     context.Context
}

func (w blockingWriter) Write([]byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}

	<-w.ctx.Done()

	return 0, w.ctx.Err()
}

//nolint:funlen // Admission, cancellation and subsequent writer recovery form one lifecycle.
func TestSQLBackupCancellationSerializesMaintenance(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(backupStore)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		entered := make(chan struct{}, 1)
		done := make(chan error, 1)

		go func() { _, err := s.WriteCanonicalBackup(ctx, blockingWriter{entered, ctx}, 0); done <- err }()

		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("backup never entered writer")
		}

		waiting, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer stop()

		if err := s.VerifyCanonicalIntegrity(waiting); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("maintenance escaped stream ownership: %v", err)
		}

		writeContext, stopWrite := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer stopWrite()

		scope := memory.Scope{Key: "test:archive-gate", Kind: memory.ScopeKindPersonal}

		writer := f.Store.(memory.ProjectionRetentionFloorWriter)
		if err := writer.SetProjectionRetentionFloor(
			writeContext,
			scope,
			"projection",
			1,
		); !errors.Is(
			err,
			context.DeadlineExceeded,
		) {
			t.Fatalf("normal write escaped snapshot: %v", err)
		}

		cancel()

		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}

		if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal("cancel leaked admission", err)
		}

		if err := writer.SetProjectionRetentionFloor(
			t.Context(),
			scope,
			"projection",
			1,
		); err != nil {
			t.Fatal(err)
		}

		end, rows, _ := backupFrames(t, s, 0)
		if end != 1 || len(rows) != 3 {
			t.Fatal("canceled writer changed snapshot clock")
		}
	})
}
