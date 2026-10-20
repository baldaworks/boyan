//go:build integration

package storefixture_test

import (
	"encoding/json"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestPublicReaderDerivedCompatibilityFilters(t *testing.T) {
	for _, filter := range []string{"legacy", "query", "category", "kind", "source", "export", "conversation", "key"} {
		t.Run(filter, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
				mutation := nativeMutation(t)
				category := memory.AtomCategoryEvent
				compat := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
					Kind: memory.DerivedKindAtom, Category: &category, Text: "Compatible EVENT", LegacyItemID: "legacy-item",
					LegacyRevisionID: "legacy-revision", LegacyOperationID: "legacy-operation"}

				data, err := json.Marshal(compat)
				if err != nil {
					t.Fatal(err)
				}

				mutation.Revisions[0].Payload = privacyRef("compatibility", data)

				mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: mutation.Revisions[0].Payload,
					Data: data})
				if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}

				request, want := readerDerivedFilter(t, mutation, filter)

				result, err := publicReader(t, f.Store).SearchDerived(t.Context(), request)
				if err != nil || len(result.Results) != want {
					t.Fatalf("result=%+v err=%v", result, err)
				}

				if want == 1 && (result.Results[0].RevisionID != "legacy-revision" ||
					result.Results[0].ItemID != "legacy-item" || result.Results[0].Text != "Compatible EVENT") {
					t.Fatalf("compatibility=%+v", result)
				}
			})
		})
	}
}

func TestPublicReaderPreservesTemporalBounds(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		mutation := nativeMutation(t)
		observed := mutation.Operation.CommittedAt.Add(234567891 * time.Nanosecond)
		from, until, event := observed.Add(time.Nanosecond), observed.Add(3*time.Nanosecond), observed.Add(time.Hour)

		mutation.Revisions[0].Temporal = memory.Temporal{ObservedAt: observed, ValidFrom: &from,
			ValidUntil: &until, ExpiresAt: &until, EventAt: &event}
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		records, err := publicReader(t, f.Store).LoadRecallRecords(t.Context(), mutation.Scope, []string{"revision"})
		if err != nil || len(records) != 1 {
			t.Fatalf("records=%+v err=%v", records, err)
		}

		for _, nanos := range []int{0, 1, 2, 3} {
			at := observed.Add(time.Duration(nanos) * time.Nanosecond)
			request := memory.RecallRequest{Scope: mutation.Scope, Query: "protected", Limit: 10, AsOf: &at}
			err := records[0].Validate(request, time.Now())

			eligible := nanos == 1 || nanos == 2
			if (err == nil) != eligible {
				t.Fatalf("nanos=%d temporal=%+v err=%v", nanos, records[0].Temporal, err)
			}
		}
	})
}

func readerDerivedFilter(t *testing.T, mutation memory.CanonicalMutation,
	filter string) (memory.DerivedSearchRequest, int) {
	t.Helper()

	request := memory.DerivedSearchRequest{Scope: mutation.Scope, Query: "event", Limit: 10}
	want := 1

	switch filter {
	case "query":
		request.Query = "absent"
		want = 0
	case "category":
		category := memory.AtomCategoryFact
		request.Category = &category
		want = 0
	case "kind":
		kind := memory.DerivedKindProfile
		request.Kind = &kind
		want = 0
	case "source":
		request.SourceID = mutation.Sources[0].SourceID
	case "export":
		request.SourceID = newPrivacyHistory(t).turn.ExportID
	case "conversation":
		request.ConversationID = "foreign"
		want = 0
	case "key":
		request.MemoryKey = "foreign"
		want = 0
	}

	return request, want
}
