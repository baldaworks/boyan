package archive_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type restoreStore interface {
	backupStore
	RestoreCanonicalBackup(ctx context.Context, source io.Reader, pending int) error
	ResumeRestoreBeforeEraseRecovery(ctx context.Context) error
	PrepareSourcePrivacy(ctx context.Context) error
}

//nolint:cyclop // Check loading, reopen and complete retry in one lifecycle.
func TestSQLRestorePublicRoundtripAndDurableLoading(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s, ok := f.Store.(restoreStore)
		if !ok {
			t.Fatal("SQL owner lacks actual restore and preflight contract")
		}

		p := newArchiveProbe(t, f)
		key := canonicalsql.Key{
			Family: canonicalsql.Payload,
			Parts:  [][]byte{[]byte("restore-blob")},
		}
		putArchiveRows(t, p, []canonicalsql.Key{key}, "preserved")

		_, _, data := backupFrames(t, s, 0)
		if err := s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1); err != nil {
			t.Fatal(err)
		}

		if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
			t.Fatal(err)
		}

		reader := &restoreFirstRead{read: func() {
			if err := p.c.View(
				t.Context(),
				func(*canonicalsql.Tx) error { return nil },
			); err == nil {
				t.Error("loading was not committed before first reader byte")
			}
		}, Reader: bytes.NewReader(data[:len(data)-1])}
		if err := s.RestoreCanonicalBackup(t.Context(), reader, 1); err == nil {
			t.Fatal("truncated archive accepted")
		} else if code, class, ok := memory.ClassifyError(err); !ok || code != memory.CodeStoreFailure ||
			class != memory.ErrorClassRetryable {
			t.Fatalf("stream error lost maintenance classification: %v", err)
		}

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()

		s = reopened.(restoreStore)
		if s.PrepareSourcePrivacy(t.Context()) == nil ||
			s.ResumeRestoreBeforeEraseRecovery(t.Context()) == nil ||
			s.VerifyCanonicalIntegrity(t.Context()) == nil {
			t.Fatal("loading restore became available across reopen")
		}

		if err := s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1); err != nil {
			t.Fatalf("complete retry: %v", err)
		}
	})
}

type restoreFirstRead struct {
	io.Reader

	read func()
}

func (r *restoreFirstRead) Read(p []byte) (int, error) {
	if r.read != nil {
		r.read()
		r.read = nil
	}

	return r.Reader.Read(p)
}
