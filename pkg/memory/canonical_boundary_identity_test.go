package memory_test

import (
	"strings"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func certificateTestBoundary(t *testing.T) m.Boundary {
	t.Helper()

	boundary, err := m.NewBoundary(m.Scope{Key: "test:boundary-certificate", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent",
			LineageID: "lineage", PreviousConversationID: "previous"},
		"transition", m.BoundaryReasonClose, time.Date(2026, time.October, 13, 1, 2, 3, 456, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	return boundary
}

func TestBoundaryFingerprintBindsEveryAuthoritativeField(t *testing.T) {
	t.Parallel()
	original := certificateTestBoundary(t)

	expected, err := m.CanonicalBoundaryFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	changes := map[string]func(*m.Boundary){
		"scope_key":             func(b *m.Boundary) { b.Scope.Key = "test:other" },
		"scope_kind":            func(b *m.Boundary) { b.Scope.Kind = m.ScopeKindGroup },
		"conversation":          func(b *m.Boundary) { b.Conversation.ConversationID = "other" },
		"agent_conversation":    func(b *m.Boundary) { b.Conversation.AgentConversationID = "other-agent" },
		"lineage":               func(b *m.Boundary) { b.Conversation.LineageID = "other-lineage" },
		"previous_conversation": func(b *m.Boundary) { b.Conversation.PreviousConversationID = "other-previous" },
		"transition":            func(b *m.Boundary) { b.TransitionID = "other-transition" },
		"reason":                func(b *m.Boundary) { b.Reason = m.BoundaryReasonReset },
		"instant":               func(b *m.Boundary) { b.OccurredAt = b.OccurredAt.Add(time.Nanosecond) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := original
			change(&changed)

			changed.ExportID, err = m.BoundaryExportID(changed.Scope, changed.Conversation, changed.TransitionID)
			if err != nil {
				t.Fatal(err)
			}

			actual, fingerprintErr := m.CanonicalBoundaryFingerprint(changed)
			if fingerprintErr != nil || actual == expected {
				t.Fatalf("authoritative field omitted from fingerprint: %s, error=%v", name, fingerprintErr)
			}
		})
	}
}

func TestBoundaryFingerprintNormalizesOnlyEquivalentInstantAndTransition(t *testing.T) {
	t.Parallel()
	original := certificateTestBoundary(t)
	equivalent := original
	equivalent.TransitionID = " \ttransition\n"
	equivalent.OccurredAt = original.OccurredAt.In(time.FixedZone("offset", 6*60*60))

	first, err := m.NewCanonicalBoundaryIdentityCommit(original)
	if err != nil {
		t.Fatal(err)
	}

	second, err := m.NewCanonicalBoundaryIdentityCommit(equivalent)
	if err != nil {
		t.Fatal(err)
	}

	if first.Certificate != second.Certificate || second.Boundary.TransitionID != "transition" ||
		second.Boundary.OccurredAt.Location() != time.UTC || equivalent.TransitionID != " \ttransition\n" {
		t.Fatal("boundary normalization changed identity or mutated caller input")
	}
}

func TestBoundaryCertificateRevalidatesAuthoritativeInputAndScope(t *testing.T) {
	t.Parallel()
	original := certificateTestBoundary(t)

	identity, err := m.NewCanonicalBoundaryIdentityCommit(original)
	if err != nil {
		t.Fatal(err)
	}

	if err := identity.Validate(original.Scope); err != nil {
		t.Fatal(err)
	}

	identity.Boundary.Reason = m.BoundaryReasonReset
	if code, class, ok := m.ClassifyError(identity.Validate(original.Scope)); !ok ||
		code != m.CodeConflict || class != m.ErrorClassPermanent {
		t.Fatal("changed authoritative input was not a permanent identity conflict")
	}

	other := original.Scope

	other.Key = "test:other"
	if code, _, ok := m.ClassifyError(identity.Validate(other)); !ok || code != m.CodeScopeViolation {
		t.Fatal("cross-scope certificate was accepted")
	}

	identity.Certificate.Fingerprint = "private-invalid-fingerprint"
	if err := identity.Certificate.Validate(); err == nil || strings.Contains(err.Error(), "private-invalid") {
		t.Fatal("malformed certificate accepted or disclosed its fingerprint")
	}
}

func TestBoundaryFingerprintRejectsInvalidSchemaAndStableIdentity(t *testing.T) {
	t.Parallel()

	for _, change := range []string{"schema", "export", "time"} {
		boundary := certificateTestBoundary(t)

		switch change {
		case "schema":
			boundary.SchemaVersion = "unsupported"
		case "export":
			boundary.ExportID = "wrong-export"
		case "time":
			boundary.OccurredAt = time.Time{}
		}

		if _, err := m.CanonicalBoundaryFingerprint(boundary); err == nil {
			t.Fatalf("invalid %s produced a usable certificate", change)
		}
	}
}
