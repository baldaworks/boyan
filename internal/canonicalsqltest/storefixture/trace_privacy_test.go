package storefixture_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicTracePrivacyAndCorruption(t *testing.T) {
	for _, kind := range []string{"source-denied", "revision-denied", "sensitive", "unknown", "incomplete",
		"unprepared", "scope-fence", "payload-missing", "payload-corrupt", "head-corrupt", "source-binding",
		"membership-subset", "message-owner", "certified-owner", "scope-stamp", "restore-loading"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if kind == "membership-subset" || kind == "certified-owner" {
					certifyMutation(t, &m)
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				want := alterReaderAuthority(t, p, m, kind)
				if want == "" {
					want = memory.CodeForgotten
				}

				got, err := publicTraceReader(t, f.Store).Trace(t.Context(), traceRequest(m))
				assertEmptyTrace(t, got, err)
				assertMutationCode(t, err, want)
			})
		})
	}
}

func TestPublicTraceEvidenceMembership(t *testing.T) {
	for _, kind := range []string{"uncertified-omitted-owner", "uncertified-incomplete", "certified-omitted-owner",
		"valid-certified", "valid-embedded-only", "embedded-omitted-owner", "evidence-digest",
		"second-evidence-omitted", "certified-extra-evidence", "embedded-evidence-digest"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := nativeMutation(t)
				if strings.Contains(kind, "certified") && !strings.HasPrefix(kind, "uncertified") {
					certifyMutation(t, &m)
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				alterReaderMembership(t, p, m, kind)

				got, err := publicTraceReader(t, f.Store).Trace(t.Context(), traceRequest(m))
				if strings.HasPrefix(kind, "valid-") {
					if err != nil || len(got.Sources) != 1 || len(got.Sources[0].Turn.Messages) != 2 {
						t.Fatalf("independent membership unavailable: %+v %v", got, err)
					}
				} else {
					assertEmptyTrace(t, got, err)
				}
			})
		})
	}
}

func TestPublicTraceClosureAndHistoricalState(t *testing.T) {
	for _, kind := range []string{"historical", "parent", "missing-parent", "wrong-root", "other-key", "other-kind",
		"node-limit", "cancel", "revision-sensitive", "source-sensitive", "extension", "raw-extension", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				runPublicTraceClosure(t, f, store, p, kind)
			})
		})
	}
}

func runPublicTraceClosure(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe, kind string) {
	t.Helper()

	m := nativeMutation(t)
	if kind == "revision-sensitive" {
		m.Revisions[0].Sensitivity = memory.SensitivitySensitive
	}

	if kind == "source-sensitive" {
		m.Sources[0].Sensitivity = memory.SensitivitySensitive
	}

	if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	r := publicTraceReader(t, f.Store)
	request := traceRequest(m)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	configureTraceClosure(t, store, p, m, &request, kind, cancel)

	got, err := r.Trace(ctx, request)

	assertTraceClosureResult(t, got, err, kind)
}

func assertTraceClosureResult(t *testing.T, got memory.TraceResponse, err error, kind string) {
	t.Helper()

	if kind == "historical" || kind == "parent" {
		want := 1
		if kind == "parent" {
			want = 2
		}

		if err != nil || len(got.Revisions) != want ||
			got.Revisions[want-1].Canonical.State != memory.RevisionStateSuperseded {
			t.Fatalf("historical closure=%+v err=%v", got, err)
		}

		return
	}

	assertEmptyTrace(t, got, err)

	assertTraceClosureCode(t, err, kind)
}

func assertTraceClosureCode(t *testing.T, err error, kind string) {
	t.Helper()

	if kind == "cancel" && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}

	if kind == "node-limit" {
		assertMutationCode(t, err, memory.CodeLimitExceeded)
	}

	if kind == "wrong-root" {
		assertMutationCode(t, err, memory.CodeScopeViolation)
	}
}

func alterTraceClosure(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	if kind == "raw-extension" {
		message := m.Messages[0]
		message.MessageID = "extra"
		p.write(t, canonicalsql.Key{Family: canonicalsql.Message, Scope: m.Scope, Parts: [][]byte{[]byte("extra")}}, message)
	}

	if kind == "missing-parent" || kind == "cycle" {
		revision := m.Revisions[0]

		revision.Parents = []string{"missing"}
		if kind == "cycle" {
			revision.Parents[0] = revision.RevisionID
		}

		p.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: m.Scope,
			Parts: [][]byte{[]byte(revision.RevisionID)}}, revision)
	}
}

func traceLaterMutation(m memory.CanonicalMutation) memory.CanonicalMutation {
	later := m
	later.ExpectedScopeVersion = 1
	later.Operation.OperationID, later.Operation.Fingerprint = "later", "later"
	later.Sources, later.Messages, later.Payloads = nil, nil, nil

	later.Revisions = append([]memory.MemoryRevision(nil), m.Revisions...)
	later.Revisions[0].RevisionID = "later"
	later.Revisions[0].Revision = 2
	later.Revisions[0].Parents = []string{m.Revisions[0].RevisionID}
	later.Heads = []memory.ItemHead{{ItemID: m.Items[0].ItemID, RevisionID: "later"}}

	return later
}

func assertEmptyTrace(t *testing.T, got memory.TraceResponse, err error) {
	t.Helper()

	if err == nil || len(got.Revisions) != 0 || len(got.Sources) != 0 {
		t.Fatalf("unsafe partial graph=%+v err=%v", got, err)
	}
}

func configureTraceClosure(t *testing.T, store mutationStore, p mutationProbe, m memory.CanonicalMutation,
	request *memory.TraceRequest, kind string, cancel context.CancelFunc) {
	t.Helper()

	switch kind {
	case "historical", "parent":
		configureHistoricalTrace(t, store, m, request, kind)
	case "wrong-root":
		request.Root.ItemID = "other"
	case "other-key":
		request.Scope.Key += ":other"
	case "other-kind":
		request.Scope.Kind = memory.ScopeKindGroup
	case "node-limit":
		request.MaxNodes = 1
	case "cancel":
		cancel()
	case "extension":
		extendTraceMembership(t, store, m)
	default:
		alterTraceClosure(t, p, m, kind)
	}
}

func extendTraceMembership(t *testing.T, store mutationStore, m memory.CanonicalMutation) {
	t.Helper()

	more := memory.CanonicalMutation{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: m.Scope,
		ExpectedScopeVersion: 1, Operation: memory.OperationRecord{OperationID: "extra", Fingerprint: "extra",
			CommittedAt: m.Operation.CommittedAt},
		Messages: append([]memory.MessageRecord(nil), m.Messages[:1]...)}

	more.Messages[0].MessageID = "extra"
	if _, err := store.ApplyCanonicalMutation(t.Context(), more); err != nil {
		t.Fatal(err)
	}
}

func configureHistoricalTrace(t *testing.T, store mutationStore, m memory.CanonicalMutation,
	request *memory.TraceRequest, kind string) {
	t.Helper()

	later := traceLaterMutation(m)
	if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
		t.Fatal(err)
	}

	if kind == "parent" {
		request.Root.RevisionID = "later"
	}
}
