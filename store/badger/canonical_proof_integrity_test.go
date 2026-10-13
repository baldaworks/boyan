package badger //nolint:testpackage // Injects checksummed malformed proof records into private storage.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func proofLogicalRecord(t *testing.T, scope memory.Scope, kind, id string, payload any) CanonicalLogicalExportRecord {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := memory.MarshalRecordEnvelope(kind, body)
	if err != nil {
		t.Fatal(err)
	}

	return CanonicalLogicalExportRecord{Kind: canonicalLogicalRecordKind, RecordType: kind,
		Components: []string{kind, scope.Key, string(scope.Kind), id}, Envelope: envelope}
}

func TestCanonicalProofLogicalRecordValidation(t *testing.T) {
	_, boundary := legacyBoundaryFixture(t)

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	cert := identity.Certificate
	invalidVersion := cert
	invalidVersion.Version++
	invalidDigest := cert
	invalidDigest.Fingerprint = "malformed-PRIVATE-proof"
	revision := canonicalRevision("trusted-revision", "trusted-item")
	revision.GroundingBoundsVersion = 2
	revision.Parents = []string{"parent"}
	revision.GroundingParents = []string{"parent"}
	unversioned := revision
	unversioned.GroundingBoundsVersion = 0
	missingParent := revision
	missingParent.GroundingBoundsVersion = 1
	missingParent.Parents = nil

	tests := []struct {
		name, kind, id string
		payload        any
	}{
		{name: "certificate version", kind: badgerRecordBoundaryCertificate, id: cert.ExportID, payload: invalidVersion},
		{name: "certificate key", kind: badgerRecordBoundaryCertificate, id: cert.ExportID + "x", payload: cert},
		{name: "certificate digest", kind: badgerRecordBoundaryCertificate, id: cert.ExportID, payload: invalidDigest},
		{name: "certificate content", kind: badgerRecordBoundaryCertificate, id: cert.ExportID, payload: map[string]any{
			"version": cert.Version, "export_id": cert.ExportID, "fingerprint": cert.Fingerprint,
			"boundary": "PRIVATE boundary text"}},
		{name: "marker version", kind: badgerRecordBoundaryCapability, id: boundaryCapabilityID,
			payload: boundaryCapability{Version: 2, Scope: boundary.Scope}},
		{name: "marker scope", kind: badgerRecordBoundaryCapability, id: boundaryCapabilityID,
			payload: boundaryCapability{Version: 1,
				Scope: memory.Scope{Key: "other", Kind: memory.ScopeKindPersonal}}},
		{name: "marker key", kind: badgerRecordBoundaryCapability, id: "unknown",
			payload: boundaryCapability{Version: 1, Scope: boundary.Scope}},
		{name: "grounding version", kind: badgerRecordRevision, id: revision.RevisionID, payload: revision},
		{name: "unversioned grounding parents", kind: badgerRecordRevision, id: revision.RevisionID, payload: unversioned},
		{name: "grounding absent provenance", kind: badgerRecordRevision, id: revision.RevisionID, payload: missingParent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := proofLogicalRecord(t, boundary.Scope, test.kind, test.id, test.payload)

			err := validateCanonicalLogicalRecord(record)
			if err == nil {
				t.Fatal("malformed checksummed proof accepted")
			}

			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("proof content disclosed: %v", err)
			}
		})
	}
}

func proofLogicalStream(t *testing.T, record CanonicalLogicalExportRecord) []byte {
	t.Helper()

	var stream bytes.Buffer
	if err := writeCanonicalLogicalJSON(&stream, CanonicalLogicalExportHeader{
		SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalHeaderKind}); err != nil {
		t.Fatal(err)
	}

	line, err := marshalCanonicalLogicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}

	line = append(line, '\n')
	digest := sha256.Sum256(line)
	stream.Write(line)

	if err := writeCanonicalLogicalJSON(&stream, CanonicalLogicalExportFooter{
		SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalFooterKind, RecordCount: 1,
		SHA256: hex.EncodeToString(digest[:])}); err != nil {
		t.Fatal(err)
	}

	return stream.Bytes()
}

func TestCanonicalProofCorruptionRejectedByMaintenanceRoutes(t *testing.T) {
	store, boundary := legacyBoundaryFixture(t)

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	identity.Certificate.Version++
	record := proofLogicalRecord(t, boundary.Scope, badgerRecordBoundaryCertificate,
		identity.Certificate.ExportID, identity.Certificate)
	stream := proofLogicalStream(t, record)

	_, err = ScanCanonicalLogicalExport(context.Background(), bytes.NewReader(stream), 1, nil)
	if err == nil {
		t.Error("logical scan accepted corrupt proof")
	}

	if err := store.ImportCanonicalLogical(context.Background(), bytes.NewReader(stream)); err == nil {
		t.Error("logical import accepted corrupt proof")
	}

	key, err := encodeCanonicalLogicalKey(record.Components, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Set(key, record.Envelope) }); err != nil {
		t.Fatal(err)
	}

	if err := store.VerifyCanonicalIntegrity(context.Background()); err == nil {
		t.Error("integrity accepted corrupt proof")
	}

	var logical bytes.Buffer
	if err := store.ExportCanonicalLogical(context.Background(), &logical); err == nil {
		t.Error("logical export accepted corrupt proof")
	}

	assertCorruptProofRestoreRejected(t, store)
}

func assertCorruptProofRestoreRejected(t *testing.T, source *CanonicalStore) {
	t.Helper()

	var backup bytes.Buffer
	if _, err := source.WriteCanonicalBackup(context.Background(), &backup, 0); err != nil {
		t.Fatal(err)
	}

	restored, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = restored.Close() })

	if err := restored.RestoreCanonicalBackup(context.Background(), bytes.NewReader(backup.Bytes()), 0); err == nil {
		t.Fatal("native restore reported success for corrupt proof")
	}
}
