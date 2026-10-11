package badger_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStoreLogicalExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()

	source := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "source.badger"))

	scope := boyan.Scope{Key: "canonical:logical-export", Kind: boyan.ScopeKindPersonal}

	mutation := canonicalRevisionMutation(
		scope,
		0,
		"operation-logical",
		canonicalRevision("revision-logical", "item-logical"),
	)
	if _, err := source.ApplyCanonicalMutation(ctx, mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	var encoded bytes.Buffer
	if err := source.ExportCanonicalLogical(ctx, &encoded); err != nil {
		t.Fatalf("ExportCanonicalLogical() error = %v", err)
	}

	if encoded.Len() == 0 ||
		!bytes.Contains(encoded.Bytes(), []byte("boyan-logical-export/v1")) {
		t.Fatalf("logical export = %q", encoded.String())
	}

	closeCanonicalTestStore(t, source)

	destination := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "destination.badger"))

	t.Cleanup(func() { _ = destination.Close() })

	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("ImportCanonicalLogical() error = %v", err)
	}

	if err := destination.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	replayed, err := destination.ApplyCanonicalMutation(ctx, mutation)
	if err != nil || replayed.ScopeVersion != 1 || len(replayed.RevisionIDs) != 1 {
		t.Fatalf("logical replay = %+v, error = %v", replayed, err)
	}
}

func TestCanonicalStoreLogicalImportStreamsBeyondLegacyRecordCeiling(t *testing.T) {
	ctx := context.Background()

	source := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "source.badger"))

	scope := boyan.Scope{Key: "canonical:logical-large", Kind: boyan.ScopeKindPersonal}

	for index := 0; index < 900; index++ {
		revisionID := fmt.Sprintf("revision-large-%d", index)

		itemID := fmt.Sprintf("item-large-%d", index)
		if _, err := source.ApplyCanonicalMutation(
			ctx,
			canonicalRevisionMutation(
				scope,
				uint64(index),
				"operation-large-"+revisionID,
				canonicalRevision(revisionID, itemID),
			),
		); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%d) error = %v", index, err)
		}
	}

	var encoded bytes.Buffer
	if err := source.ExportCanonicalLogical(ctx, &encoded); err != nil {
		t.Fatalf("ExportCanonicalLogical() error = %v", err)
	}

	closeCanonicalTestStore(t, source)

	destination := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "destination.badger"))

	t.Cleanup(func() { _ = destination.Close() })

	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatalf("ImportCanonicalLogical() error = %v", err)
	}

	if err := destination.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	state, err := destination.LoadScopeState(ctx, scope)
	if err != nil {
		t.Fatalf("LoadScopeState() error = %v", err)
	}

	if state.Version != 900 || state.ChangeSeq != 900 {
		t.Fatalf("LoadScopeState() = %#v, want version/change_seq 900", state)
	}
}
