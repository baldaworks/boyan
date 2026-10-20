package storefixture_test

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/store/archive"
)

func TestPublicPrivacyIntegrityMaintenanceCorruption(t *testing.T) {
	for _, kind := range []string{"version", "state", "scope", "source", "payload", "member",
		"complete-empty", "stage", "scope-flag", "ready", "native-header", "message-payload", "certificate-subset",
		"embedded-subset"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if kind == "native-header" || kind == "message-payload" || kind == "certificate-subset" {
					certifyMutation(t, &m)
				}

				_, err := store.ApplyCanonicalMutation(t.Context(), m)
				requireProjectionNoError(t, err)

				if kind == "embedded-subset" {
					var privacy map[string]any
					p.read(t, m.Scope, canonicalsql.SourcePrivacy, m.Sources[0].SourceID, &privacy)
					installReaderEmbeddedSource(t, p, &m, privacy, true)
					p.write(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacy, Scope: m.Scope,
						Parts: [][]byte{[]byte(m.Sources[0].SourceID)}}, privacy)
				}

				verifier := f.Store.(archive.CanonicalIntegrityVerifier)
				requireProjectionNoError(t, verifier.VerifyCanonicalIntegrity(t.Context()))
				corruptPublicPrivacyIntegrity(t, p, m, kind)

				if err := verifier.VerifyCanonicalIntegrity(t.Context()); err == nil {
					t.Fatal("invalid privacy passed integrity")
				}

				var output bytes.Buffer
				if err := f.Store.(archive.CanonicalLogicalExporter).ExportCanonicalLogical(t.Context(), &output); err == nil {
					t.Fatal("invalid privacy exported")
				}
			})
		})
	}
}
