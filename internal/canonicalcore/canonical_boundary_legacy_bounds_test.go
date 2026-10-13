package canonicalcore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func TestLegacyBoundaryBoundsResolveBeforeModelAdmission(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"inherited_expiry",
		"superseded_ancestor",
		"missing_parent",
		"cycle",
		"budget",
		"future_ancestor"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newInheritanceFixture(t, false)
			outcome, err := f.processor.ProcessBoundary(context.Background(), f.export)
			requireNoError(t, err, "create fixture: %v")

			if mode == "superseded_ancestor" {
				supersedeLegacyAncestor(t, f)
			}

			records, header, footer := exportLegacyFixture(t, f.store)
			records = unproveLegacyBounds(t, records, outcome, mode)
			imported, reader := newModelRetryStore(t)
			requireNoError(t, imported.ImportCanonicalLogical(context.Background(), bytes.NewReader(
				marshalLegacyFixture(t, records, header, footer))), "import legacy fixture: %v")
			view, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), f.export.Scope, f.expiry)

			if legacyFixtureInvalid(mode) {
				want := m.CodeInvalidDerived
				if mode == "budget" {
					want = m.CodeLimitExceeded
				}

				if code, _, ok := m.ClassifyError(err); !ok || code != want {
					t.Errorf("legacy %s error=%v want %s", mode, err, want)
				}

				return
			}

			requireNoError(t, err, "resolve legacy bounds: %v")

			assertLegacyBoundsAdmission(t, f, reader, view, outcome)
		})
	}
}

func supersedeLegacyAncestor(t *testing.T, f *inheritanceFixture) {
	t.Helper()

	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
		error) {
		candidates, err := extractTestStateCandidate(ctx, request)
		if err == nil {
			candidates[0].Predicate = "Parent B"
		}

		return candidates, err
	})
	processor, err := core.NewCanonicalTurnProcessor(f.store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "native processor: %v")
	turn,
		err := m.NewTurn(f.export.Scope,
		f.export.Conversation,
		"Parent B update",
		f.now.Add(time.Hour),
		"Parent B updated",
		"OK")
	requireNoError(t, err, "updated native: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "supersede native ancestor: %v")
}

func exportLegacyFixture(t *testing.T, store *b.CanonicalStore) ([]b.CanonicalLogicalExportRecord,
	b.CanonicalLogicalExportHeader, b.CanonicalLogicalExportFooter) {
	t.Helper()

	var stream bytes.Buffer
	requireNoError(t, store.ExportCanonicalLogical(context.Background(), &stream), "export fixture: %v")
	lines := bytes.Split(bytes.TrimSpace(stream.Bytes()), []byte{'\n'})

	var (
		header b.CanonicalLogicalExportHeader
		footer b.CanonicalLogicalExportFooter
	)

	requireNoError(t, json.Unmarshal(lines[0], &header), "header: %v")
	requireNoError(t, json.Unmarshal(lines[len(lines)-1], &footer), "footer: %v")

	records := make([]b.CanonicalLogicalExportRecord, 0, len(lines)-2)
	for _, line := range lines[1 : len(lines)-1] {
		var record b.CanonicalLogicalExportRecord
		requireNoError(t, json.Unmarshal(line, &record), "record: %v")
		records = append(records, record)
	}

	return records, header, footer
}

func unproveLegacyBounds(t *testing.T, records []b.CanonicalLogicalExportRecord,
	outcome m.BoundaryOutcome, mode string) []b.CanonicalLogicalExportRecord {
	t.Helper()

	scenario := outcome.Scenarios.Revisions[0].RevisionID
	profile := outcome.Profile.Revisions[0].RevisionID

	for index, record := range records {
		if record.RecordType != "revision" {
			continue
		}

		envelope, err := m.UnmarshalRecordEnvelope(record.Envelope)
		requireNoError(t, err, "revision envelope: %v")

		var revision m.MemoryRevision
		requireNoError(t, json.Unmarshal(envelope.Payload, &revision), "revision: %v")

		revision = unprovedLegacyRevision(revision, scenario, profile, mode)

		records[index] = legacyRevisionRecord(t, record, revision)
		if mode == "budget" && revision.RevisionID == scenario {
			for n := range m.MaxSourcesPerRevision + 2 {
				ancestor := revision
				ancestor.RevisionID = fmt.Sprintf("legacy-budget-%04d", n)
				ancestor.Parents = []string{fmt.Sprintf("legacy-budget-%04d", n+1)}
				records = append(records, legacyRevisionRecord(t, record, ancestor))
			}
		}
	}

	return records
}

func legacyRevisionRecord(t *testing.T, record b.CanonicalLogicalExportRecord,
	revision m.MemoryRevision) b.CanonicalLogicalExportRecord {
	t.Helper()

	payload, err := json.Marshal(revision)
	requireNoError(t, err, "revision JSON: %v")
	record.Envelope, err = m.MarshalRecordEnvelope(record.RecordType, payload)
	requireNoError(t, err, "revision envelope: %v")

	record.Components = append([]string(nil), record.Components...)
	record.Components[len(record.Components)-1] = revision.RevisionID

	return record
}

func marshalLegacyFixture(t *testing.T, records []b.CanonicalLogicalExportRecord,
	header b.CanonicalLogicalExportHeader, footer b.CanonicalLogicalExportFooter) []byte {
	t.Helper()

	var stream bytes.Buffer

	encoder := json.NewEncoder(&stream)
	requireNoError(t, encoder.Encode(header), "header encode: %v")

	hash := sha256.New()

	for _, record := range records {
		line, err := json.Marshal(record)
		requireNoError(t, err, "record encode: %v")

		line = append(line, '\n')
		stream.Write(line)
		hash.Write(line)
	}

	footer.RecordCount = uint64(len(records))
	footer.SHA256 = hex.EncodeToString(hash.Sum(nil))
	requireNoError(t, encoder.Encode(footer), "footer encode: %v")

	return stream.Bytes()
}

func legacyFixtureInvalid(mode string) bool {
	return mode == "missing_parent" || mode == "cycle" || mode == "budget" || mode == "future_ancestor"
}

func assertLegacyBoundsAdmission(t *testing.T, f *inheritanceFixture, reader *b.CanonicalReader,
	view m.CanonicalBoundaryView, outcome m.BoundaryOutcome) {
	t.Helper()

	if len(view.Boundary.Eligible.Scenarios) != 0 || len(view.Boundary.Eligible.Profiles) != 0 {
		t.Fatal("legacy descendants outlived their actual ancestor in model context")
	}

	for _, ref := range []m.RevisionRef{outcome.Scenarios.Revisions[0], outcome.Profile.Revisions[0]} {
		parent := view.Boundary.Parents[ref]
		if parent.Temporal.ExpiresAt == nil || !parent.Temporal.ExpiresAt.Equal(f.expiry) {
			t.Errorf("legacy bound lost: %+v", parent)
		}
	}

	before, err := reader.LoadCanonicalBoundaryViewAt(context.Background(), f.export.Scope, f.expiry.Add(-time.Second))
	requireNoError(t, err, "before inherited expiry: %v")

	if len(before.Boundary.Eligible.Scenarios) != 1 || len(before.Boundary.Eligible.Profiles) != 1 {
		t.Fatal("superseded historical ancestor incorrectly excluded eligible descendant")
	}
}

func unprovedLegacyRevision(revision m.MemoryRevision, scenario, profile, mode string) m.MemoryRevision {
	if revision.GroundingBoundsVersion == 0 {
		if mode == "future_ancestor" {
			revision.Temporal.ObservedAt = time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC)
		}

		return revision
	}

	revision.GroundingBoundsVersion = 0
	revision.GroundingParents = nil
	revision.Temporal.ValidFrom = nil
	revision.Temporal.ValidUntil = nil

	revision.Temporal.ExpiresAt = nil
	if revision.RevisionID == scenario {
		switch mode {
		case "missing_parent":
			revision.Parents = []string{"unavailable-parent"}
		case "cycle":
			revision.Parents = []string{profile}
		case "budget":
			revision.Parents = []string{"legacy-budget-0000"}
		}
	}

	return revision
}
