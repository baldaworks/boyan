package canonicalsql

import (
	"slices"
	"testing"
)

// Independent expected schema grounded in store/badger/canonical_keys_{core,
// provenance,projection,change,ingest}.go and canonical_erase_blob_locks.go.
// Single-ID rows use canonicalKey; forget outcome splits its old NUL composite.
// Payload/blob globals remove the old synthetic payloadScope; readiness and
// erase guard are global singletons (canonical_source_privacy_readiness.go,
// canonical_erase_fence.go). This fixture must not be built from familyDefinition.
var expectedShapes = []struct {
	family Family
	table  string
	scoped bool
	parts  []string
}{
	{ScopeState, "scope_state", true, nil},
	{Operation, "operation", true, []string{"id"}},
	{Source, "source", true, []string{"id"}},
	{Message, "message", true, []string{"id"}},
	{Item, "item", true, []string{"id"}},
	{Revision, "revision", true, []string{"id"}},
	{Lifecycle, "lifecycle", true, []string{"id"}},
	{Head, "head", true, []string{"id"}},
	{ScopeChange, "scope_change", true, []string{"sequence_u64"}},
	{Delivery, "delivery", true, []string{"id"}},
	{DeliveryClaim, "delivery_claim", true, []string{"id"}},
	{Payload, "payload", false, []string{"id"}},
	{ProvenanceForward, "provenance_forward", true, []string{"from_id", "to_id"}},
	{ProvenanceReverse, "provenance_reverse", true, []string{"from_id", "to_id"}},
	{SourceRevision, "source_revision", true, []string{"from_id", "to_id"}},
	{DeniedSource, "denied_source", true, []string{"id"}},
	{DeniedRevision, "denied_revision", true, []string{"id"}},
	{ProjectionManifest, "projection_manifest", true, []string{"projection_id", "generation_id"}},
	{ProjectionActive, "projection_active", true, []string{"projection_id"}},
	{ProjectionRetention, "projection_retention", true, []string{"projection_id"}},
	{ProjectionCheckpoint, "projection_checkpoint", true, []string{"kind"}},
	{ForgetOutcome, "forget_outcome", true, []string{"kind", "id"}},
	{IngestSequence, "ingest_sequence", true, []string{"producer_id", "sequence_u64"}},
	{IngestCursor, "ingest_cursor", true, []string{"producer_id"}},
	{TurnCertificate, "turn_certificate", true, []string{"id"}},
	{BoundaryCertificate, "boundary_certificate", true, []string{"id"}},
	{BoundaryCapability, "boundary_capability", true, []string{"id"}},
	{SourcePrivacy, "source_privacy", true, []string{"id"}},
	{SourcePrivacyScope, "source_privacy_scope", true, []string{"id"}},
	{EraseJournal, "erase_journal", true, []string{"id"}},
	{EraseFence, "erase_fence", true, []string{"id"}},
	{EraseSourceIdentity, "erase_source_identity", true, []string{"id"}},
	{SourcePrivacyReady, "source_privacy_ready", false, nil},
	{EraseGuard, "erase_guard", false, nil},
	{EraseSourceRef, "erase_source_ref", true, []string{"operation_id", "id"}},
	{EraseLockRef, "erase_lock_ref", true, []string{"operation_id", "id"}},
	{EraseBlobLock, "erase_blob_lock", false, []string{"id"}},
	{ErasedBlob, "erased_blob", false, []string{"id"}},
}

func TestIndependentFamilyShapes(t *testing.T) {
	if len(expectedShapes) != 38 {
		t.Fatal("independent inventory must contain exactly 38 families")
	}

	seen := map[Family]bool{}
	globals := 0

	for _, want := range expectedShapes {
		if seen[want.family] {
			t.Fatal("duplicate expected family")
		}

		seen[want.family] = true
		if !want.scoped {
			globals++
		}

		assertExpectedShape(t, want.family, want.table, want.scoped, want.parts)
	}

	if globals != 5 {
		t.Fatal("global family classification drift")
	}

	for _, family := range durableFamilies {
		if !seen[family] {
			t.Fatalf("unrecognized implementation family %s", family)
		}
	}
}

func assertExpectedShape(t *testing.T, family Family, table string, scoped bool, parts []string) {
	t.Helper()

	got, err := familyDefinition(family)
	if err != nil {
		t.Fatal(err)
	}

	if got.table != table || got.scoped != scoped || !slices.Equal(got.parts, parts) {
		t.Errorf("%s tuple shape differs: %#v", family, got)
	}
}
