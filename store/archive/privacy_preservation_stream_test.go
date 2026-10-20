package archive_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/store/archive"
)

func TestSQLPrivacyPreservationNativeInputContracts(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(restoreStore)

		var stream *bytes.Buffer

		t.Run("typed-nil-reader", func(t *testing.T) {
			if err := s.RestoreCanonicalBackup(t.Context(), stream, 1); err == nil {
				t.Fatal("typed-nil native reader accepted")
			}
		})
		t.Run("empty-stream", func(t *testing.T) {
			if err := s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(nil), 1); err == nil {
				t.Fatal("zero-byte native stream accepted")
			}
		})
	})
}

func TestSQLPrivacyPreservationLogicalIncompleteRetry(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)

		good := logicalBytes(t, source.Store.(logicalStore))
		for _, fault := range []string{"footer", "cancel"} {
			t.Run(fault, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)

				data := good
				if fault == "footer" {
					lines := bytes.Split(bytes.TrimSuffix(good, []byte{'\n'}), []byte{'\n'})
					data = append(bytes.Join(lines[:len(lines)-1], []byte{'\n'}), '\n')
				}

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				options := archive.CanonicalLogicalImportOptions{BatchSize: 1}
				if fault == "cancel" {
					options.Progress = func(archive.CanonicalLogicalImportProgress) error {
						cancel()

						return nil
					}
				}

				_, err := d.Store.(logicalStore).ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(data), options)
				if err == nil {
					t.Fatal("incomplete logical source published")
				}

				if fault == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}

				privacyPreservationNoSpool(t)
				privacyPreservationUnavailable(t, d, m)
				privacyPreservationReopen(t, d)
				privacyPreservationUnavailable(t, d, m)

				if err := d.Store.(restoreStore).PrepareSourcePrivacy(t.Context()); err == nil {
					t.Fatal("incomplete source prepared")
				}

				if err := d.Store.(logicalStore).ImportCanonicalLogical(t.Context(), bytes.NewReader(good)); err != nil {
					t.Fatal("complete retry: ", err)
				}

				privacyPreservationTrace(t, d, m, "")
			})
		}
	})
}

type privacyPreservationEOFReader struct {
	*bytes.Reader

	cancel context.CancelFunc
}

func (r *privacyPreservationEOFReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if r.Reader.Len() == 0 {
		r.cancel()
	}

	return n, err
}

var _ io.Reader = (*privacyPreservationEOFReader)(nil)

func TestSQLPrivacyPreservationCopiedReadinessCanceledAtEOF(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)
		_, frames, data := backupFrames(t, source.Store.(backupStore), 0)
		privacyPreservationFrame(t, frames, canonicalsql.SourcePrivacyReady)
		d := restoreDestination(t, source.Backend)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		r := &privacyPreservationEOFReader{Reader: bytes.NewReader(data), cancel: cancel}

		err := d.Store.(restoreStore).RestoreCanonicalBackup(ctx, r, 1)
		if !errors.Is(err, context.Canceled) || r.Len() != 0 {
			t.Fatalf("EOF cancellation=%v remaining=%d", err, r.Len())
		}

		privacyPreservationNoSpool(t)
		privacyPreservationUnavailable(t, d, m)
		privacyPreservationReopen(t, d)
		privacyPreservationUnavailable(t, d, m)

		if err := d.Store.(restoreStore).PrepareSourcePrivacy(t.Context()); err == nil {
			t.Fatal("copied readiness certified canceled stream")
		}
	})
}

func TestSQLPrivacyPreservationLoadingValidatedRestart(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, source *storefixture.Fixture) {
		m, _ := privacyPreservationSeed(t, source)
		// This final payload is a real replay-batch failure target, after the
		// source fixture has passed public Trace and integrity validation.
		putArchiveRows(t, newArchiveProbe(t, source), []canonicalsql.Key{{Family: canonicalsql.Payload,
			Parts: [][]byte{[]byte("b")}}}, "data")

		_, _, data := backupFrames(t, source.Store.(backupStore), 0)
		for _, stage := range []string{"batch", "validated", "privacy", "remove"} {
			t.Run(stage, func(t *testing.T) {
				d := restoreDestination(t, source.Backend)
				remove := installRestoreFault(t, newArchiveProbe(t, d), stage)
				err := d.Store.(restoreStore).RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1)

				remove()

				if err == nil {
					t.Fatal("restore transition failure ignored")
				}

				privacyPreservationNoSpool(t)
				privacyPreservationUnavailable(t, d, m)
				privacyPreservationReopen(t, d)
				privacyPreservationUnavailable(t, d, m)
				s := d.Store.(restoreStore)

				switch stage {
				case "privacy":
					err = s.PrepareSourcePrivacy(t.Context())
				case "remove":
					err = s.ResumeRestoreBeforeEraseRecovery(t.Context())
				default:
					if s.PrepareSourcePrivacy(t.Context()) == nil {
						t.Fatal("loading transition prepared without whole retry")
					}

					err = s.RestoreCanonicalBackup(t.Context(), bytes.NewReader(data), 1)
				}

				if err != nil {
					t.Fatal("restore recovery: ", err)
				}

				privacyPreservationTrace(t, d, m, "")
			})
		}
	})
}
