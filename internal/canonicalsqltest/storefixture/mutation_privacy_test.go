package storefixture_test

import (
	"context"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	m "github.com/baldaworks/boyan/pkg/memory"
	"strconv"
	"testing"
)

func TestSourcePrivacyUnclassifiedInitialCannotPromoteFromSubset(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		initial := mutation
		initial.Revisions = nil
		initial.Heads = nil

		initial.Items = nil
		if _, err := store.ApplyCanonicalMutation(context.Background(), initial); err != nil {
			t.Fatal(err)
		}

		if got := privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
			t.Fatalf("source default certified: %s", got)
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "subset"
		mutation.Operation.Fingerprint = "subset"
		mutation.Sources = nil
		mutation.Messages = nil

		mutation.Payloads = nil
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		if got := privacyState(t, probe, mutation.Scope, initial.Sources[0].SourceID); got != "unknown" {
			t.Fatalf("historic subset promoted: %s", got)
		}
	})
}

func TestSourcePrivacyIncompleteEvidenceCannotBeHiddenByStandardSibling(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		for _, reverse := range []bool{false, true} {
			t.Run(map[bool]string{false: "incomplete-first", true: "incomplete-last"}[reverse], func(t *testing.T) {
				mutation := nativeMutation(t)
				incomplete := mutation.Revisions[0]
				incomplete.RevisionID = "incomplete"
				incomplete.Evidence = append([]m.EvidenceRef(nil), incomplete.Evidence...)

				incomplete.Evidence[0].TextDigest = ""
				if reverse {
					mutation.Revisions = append(mutation.Revisions, incomplete)
				} else {
					mutation.Revisions = append([]m.MemoryRevision{incomplete}, mutation.Revisions...)
				}

				if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
					t.Fatal(err)
				}

				if got := privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
					t.Fatalf("incomplete sibling blessed: %s", got)
				}
			})
		}
	})
}

func TestSourcePrivacySensitiveSourceAndSiblingAlwaysWin(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		for _, sourceOnly := range []bool{true, false} {
			t.Run(map[bool]string{true: "source-only", false: "incomplete-sibling"}[sourceOnly], func(t *testing.T) {
				mutation := nativeMutation(t)
				if sourceOnly {
					mutation.Sources[0].Sensitivity = m.SensitivitySensitive
					mutation.Revisions = nil
					mutation.Heads = nil
				} else {
					mutation.Revisions[0].Sensitivity = m.SensitivitySensitive
					incomplete := mutation.Revisions[0]
					incomplete.RevisionID = "incomplete"
					incomplete.Sensitivity = m.SensitivityStandard
					incomplete.Evidence = append([]m.EvidenceRef(nil), incomplete.Evidence...)
					incomplete.Evidence[0].TextDigest = ""
					mutation.Revisions = append(mutation.Revisions, incomplete)
				}

				if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
					t.Fatal(err)
				}

				if got := privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID); got != "sensitive" {
					t.Fatalf("known sensitivity lost: %s", got)
				}
			})
		}
	})
}

func TestSourcePrivacyRejectsUnboundedParentEvidence(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}
		// All parents exist and are individually valid. Reject their unbounded
		// combined authority before loading history, rather than scan it at ingest.
		oversized := mutation.Revisions[0]
		oversized.RevisionID = "oversized"
		oversized.Parents = make([]string, m.MaxSourcesPerRevision+1)

		parents := make([]m.MemoryRevision, len(oversized.Parents))
		for i := range parents {
			parents[i] = mutation.Revisions[0]
			parents[i].RevisionID = "parent-" + strconv.Itoa(i)
			oversized.Parents[i] = parents[i].RevisionID
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "oversized"
		mutation.Operation.Fingerprint = "oversized"

		mutation.Revisions = append(parents, oversized)
		mutation.Heads = nil

		_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
		if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeLimitExceeded {
			t.Fatalf("unbounded parents accepted: %v", err)
		}
	})
}

func TestSourcePrivacyUnclassifiedMessageAppendInvalidatesAuthority(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "append-message"
		mutation.Operation.Fingerprint = "append-message"
		mutation.Sources = nil
		mutation.Items = nil
		mutation.Revisions = nil
		mutation.Heads = nil
		mutation.Messages = append([]m.MessageRecord(nil), mutation.Messages[:1]...)
		mutation.Messages[0].MessageID = "unclassified-message"
		mutation.Messages[0].Payload = privacyRef("unclassified-message", []byte("unclassified"))

		mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Messages[0].Payload, Data: []byte("unclassified")}}
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		if got := privacyState(t, probe, mutation.Scope, mutation.Messages[0].SourceID); got != "unknown" {
			t.Fatalf("new unclassified text blessed: %s", got)
		}
	})
}

func TestSourcePrivacyNewSourceWithPriorDanglingHistoryStaysUnknown(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		dangling := mutation
		dangling.Sources = nil
		dangling.Messages = nil

		dangling.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)

		dangling.Revisions[0].Sensitivity = m.SensitivitySensitive
		if _, err := store.ApplyCanonicalMutation(context.Background(), dangling); err != nil {
			t.Fatal(err)
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "new-source"
		mutation.Operation.Fingerprint = "new-source"
		mutation.Revisions[0].RevisionID = "new-source"

		mutation.Heads[0].RevisionID = "new-source"
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		if got := privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
			t.Fatalf("historic classification erased by source creation: %s", got)
		}
	})
}

func TestSourcePrivacyStandardSiblingCannotCertifyMessageExtension(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "extend-with-standard"
		mutation.Operation.Fingerprint = "extend-with-standard"
		mutation.Revisions[0].RevisionID = "extend-with-standard"
		mutation.Heads[0].RevisionID = "extend-with-standard"
		mutation.Messages = append([]m.MessageRecord(nil), mutation.Messages[:1]...)
		mutation.Messages[0].MessageID = "new-unclassified-message"
		mutation.Messages[0].Payload = privacyRef("new-unclassified-message", []byte("unclassified"))

		mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Messages[0].Payload, Data: []byte("unclassified")}}
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatal(err)
		}

		if got := privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
			t.Fatalf("standard subset certified extension: %s", got)
		}
	})
}

func TestSourcePrivacyRejectsWrongMessageOwnership(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		mutation.Messages[0].SourceID = "other-source"

		_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
		if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
			t.Fatalf("cross-source evidence accepted: %v", err)
		}
	})
}

func TestSourcePrivacyRejectsDanglingMessageBeforeSourceCreation(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		mutation.Sources = nil
		mutation.Items = nil
		mutation.Revisions = nil
		mutation.Heads = nil

		_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
		if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
			t.Fatalf("dangling raw membership accepted: %v", err)
		}

		state, stateErr := store.LoadScopeState(context.Background(), mutation.Scope)
		if stateErr != nil || state.Version != 0 {
			t.Fatalf("dangling message changed scope: %+v %v", state, stateErr)
		}
	})
}
