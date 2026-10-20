//go:build integration

package canonicalsql

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofLegacyAncestry(t *testing.T) {
	for _, kind := range []string{"inherit",
		"cycle",
		"future-parent",
		"missing-parent",
		"wrong-item",
		"versioned",
		"payload-budget",
		"structural-parent"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) { runProofAncestry(t, owner, kind) })
		})
	}
}

func runProofAncestry(t *testing.T, owner *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	expiry := h.turn.CompletedAt.Add(time.Hour)
	h.revision.Temporal.ExpiresAt = &expiry
	h.revision.Retention = memory.RetentionClassEphemeral

	h.revision.Sensitivity = memory.SensitivitySensitive
	if kind == "future-parent" {
		h.revision.Temporal.ObservedAt = h.turn.CompletedAt.Add(time.Minute)
	}

	seedPrivacyHistory(t, owner, h)
	child := h.revision
	child.ItemID = "child-item"
	child.RevisionID = "child"
	child.Parents = []string{h.revision.RevisionID}
	child.Temporal = memory.Temporal{ObservedAt: h.turn.CompletedAt}
	child.Retention = memory.RetentionClassStandard
	child.Sensitivity = memory.SensitivityStandard
	payload := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
		Kind:              memory.DerivedKindProfile,
		Text:              "profile",
		LegacyItemID:      "legacy-item",
		LegacyRevisionID:  "legacy-revision",
		LegacyOperationID: "legacy-operation"}

	alterLegacyAncestry(&child, &payload, expiry, kind)

	data, err := json.Marshal(payload)
	requireProofSuccess(t, err)

	child.Payload = privacyRef("child-payload", data)
	if kind == "payload-budget" {
		child.Payload.ByteSize = uint32(memory.MaxDerivedTurnTextBytes + 1)
	}

	err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		item := memory.MemoryItem{ItemID: child.ItemID,
			Scope:     h.scope,
			Kind:      memory.MemoryKindState,
			MemoryKey: "derived:profile"}
		for _, record := range []struct {
			f     Family
			id    string
			value any
		}{{Item, item.ItemID, item}, {Revision, child.RevisionID, child}} {
			key, _ := privacyKey(h.scope, record.f, record.id)
			if err := putPrivacyRecord(ctx, tx, key, record.f, record.value); err != nil {
				return err
			}
		}

		key := Key{Family: Payload, Parts: [][]byte{[]byte(child.Payload.ID)}}

		return putPrivacyRecord(ctx, tx, key, Payload, data)
	})
	requireProofSuccess(t, err)

	assertLegacyAncestry(t, owner, h, child, expiry, kind)
}

func TestProofRawStructuralValidation(t *testing.T) {
	for _, kind := range []string{"certificate-version",
		"certificate-key",
		"certificate-digest",
		"marker-version",
		"marker-scope",
		"marker-key",
		"grounding-version",
		"unversioned-parents",
		"missing-provenance"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := newPrivacyHistory(t)
				identity := proofBoundary(t, h.scope)
				family, id := BoundaryCertificate, identity.Certificate.ExportID

				var value any = identity.Certificate

				switch kind {
				case "certificate-version":
					identity.Certificate.Version++
					value = identity.Certificate
				case "certificate-key":
					id += "x"
				case "certificate-digest":
					identity.Certificate.Fingerprint = "private-invalid"
					value = identity.Certificate
				case "marker-version", "marker-scope", "marker-key":
					family = BoundaryCapability
					value, id = corruptCapabilityFixture(h.scope, kind)

				default:
					family, id = Revision, h.revision.RevisionID
					value = corruptGroundingFixture(h.revision, kind)
				}

				key, _ := privacyKey(h.scope, family, id)

				requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					return tx.Put(ctx, key, envelope(t, family, value))
				}))

				err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
					var value any

					return getPrivacyRecord(ctx, tx, key, family, &value)
				})
				assertCode(t, err, memory.CodeInvalidDerived)
			})
		})
	}
}

func alterLegacyAncestry(child *memory.MemoryRevision, payload *memory.CanonicalCompatibilityPayload,
	expiry time.Time, kind string) {
	if kind == "cycle" {
		child.Parents = []string{"child"}
	}

	if kind == "missing-parent" {
		child.Parents = []string{"missing"}
	}

	if kind == "structural-parent" {
		payload.Supersedes = &memory.RevisionRef{ItemID: "missing", RevisionID: "missing"}
	}

	if kind == "versioned" {
		child.GroundingBoundsVersion = 1
		child.GroundingParents = child.Parents
		child.Temporal.ExpiresAt = &expiry
		child.Retention = memory.RetentionClassEphemeral
		child.Sensitivity = memory.SensitivitySensitive
	}
}

func assertLegacyAncestry(t *testing.T, owner *Owner, h privacyHistory, child memory.MemoryRevision,
	expiry time.Time, kind string) {
	t.Helper()

	err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		resolver := newBoundaryBoundsResolver(ctx, tx, h.scope, h.turn.CompletedAt)

		ref := memory.RevisionRef{ItemID: child.ItemID, RevisionID: child.RevisionID}
		if kind == "wrong-item" {
			ref.ItemID = "other"
		}

		result, err := resolver.resolve(ref)
		if kind == "inherit" || kind == "versioned" {
			if err != nil {
				return err
			}

			if result.Temporal.ExpiresAt == nil ||
				!result.Temporal.ExpiresAt.Equal(expiry) ||
				result.Retention != memory.RetentionClassEphemeral ||
				result.Sensitivity != memory.SensitivitySensitive {
				t.Fatalf("weakened inherited constraints: %+v", result)
			}

			return nil
		}

		if err == nil {
			t.Fatal("invalid legacy ancestry accepted")
		}

		return nil
	})
	requireProofSuccess(t, err)
}

func corruptCapabilityFixture(scope memory.Scope, kind string) (boundaryCapability, string) {
	id := "v1"

	marker := boundaryCapability{Version: 1, Scope: scope}
	if kind == "marker-version" {
		marker.Version++
	}

	if kind == "marker-scope" {
		marker.Scope.Kind = memory.ScopeKindGroup
	}

	if kind == "marker-key" {
		id = "other"
	}

	return marker, id
}

func corruptGroundingFixture(revision memory.MemoryRevision, kind string) memory.MemoryRevision {
	revision.Parents = []string{"parent"}
	revision.GroundingParents = []string{"parent"}

	revision.GroundingBoundsVersion = 2
	if kind == "unversioned-parents" {
		revision.GroundingBoundsVersion = 0
	}

	if kind == "missing-provenance" {
		revision.GroundingBoundsVersion = 1
		revision.Parents = nil
	}

	return revision
}
