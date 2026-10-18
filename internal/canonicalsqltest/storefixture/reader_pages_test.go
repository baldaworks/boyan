package storefixture_test

import (
	"context"
	"errors"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublicReaderRawPagesAndReopen(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		initial := nativeMutation(t)

		ids := []string{"aa", "a\x00z", "z", "aaa", "a", strings.Repeat("q", 65535)}
		for index, id := range ids {
			mutation := nativeMutation(t)
			mutation.ExpectedScopeVersion = uint64(index)
			mutation.Operation.OperationID = id
			mutation.Items[0].ItemID = id
			mutation.Revisions[0].ItemID = id
			mutation.Revisions[0].RevisionID = id

			mutation.Heads = []memory.ItemHead{{ItemID: id, RevisionID: id}}
			if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
		}

		want := []string{"a", "a\x00z", "aa", "aaa", strings.Repeat("q", 65535), "z"}
		assertReaderRawPages(t, f.Store.(activeReaderStore), initial.Scope, initial.Sources[0].SourceID, want)

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()

		assertReaderRawPages(t, reopened.(activeReaderStore), initial.Scope, initial.Sources[0].SourceID, want)
	})
}

func assertReaderRawPages(t *testing.T, store activeReaderStore, scope memory.Scope, source string, want []string) {
	t.Helper()

	after := ""
	for _, id := range want {
		heads, err := store.ScanActiveHeads(t.Context(), memory.ActiveHeadScanRequest{Scope: scope,
			AfterItemID: after, Limit: 1})
		if err != nil || len(heads) != 1 || heads[0].ItemID != id {
			t.Fatalf("head after %q: count=%d err=%v", after, len(heads), err)
		}

		ids, next, err := store.SourceRevisionBatch(t.Context(), scope, source, after, 1)
		if err != nil || !reflect.DeepEqual(ids, []string{id}) || next != id {
			t.Fatalf("source page after %q: count=%d err=%v", after, len(ids), err)
		}

		after = id
	}

	assertReaderPageEnds(t, store, scope, after)
}

func assertReaderPageEnds(t *testing.T, store activeReaderStore, scope memory.Scope, after string) {
	t.Helper()

	heads, err := store.ScanActiveHeads(t.Context(), memory.ActiveHeadScanRequest{Scope: scope,
		AfterItemID: after, Limit: 1})
	if err != nil || len(heads) != 0 {
		t.Fatalf("end page=%+v err=%v", heads, err)
	}

	heads, err = store.ScanActiveHeads(t.Context(), memory.ActiveHeadScanRequest{Scope: scope,
		AfterItemID: "a0", Limit: 1})
	if err != nil || len(heads) != 1 || heads[0].ItemID != "aa" {
		t.Fatalf("absent cursor=%+v err=%v", heads, err)
	}
}

func TestPublicReaderExactScopesAndUnsigned(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
		scopes := []memory.Scope{{Key: "scope:z", Kind: memory.ScopeKindPersonal}, {Key: "scope:aa",
			Kind: memory.ScopeKindPersonal},
			{Key: "scope:aa", Kind: memory.ScopeKindGroup}, {Key: "scope:a\x00z", Kind: memory.ScopeKindPersonal}}
		for _, scope := range scopes {
			mutation := nativeMutation(t)
			mutation.Scope = scope
			mutation.Items[0].Scope = scope

			mutation.Sources[0].Scope = scope
			if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
		}

		expected := []memory.Scope{scopes[3], scopes[2], scopes[1], scopes[0]}
		assertReaderScopePages(t, f.Store.(activeReaderStore), expected)

		state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scopes[1],
			Version: math.MaxUint64, ChangeSeq: math.MaxUint64}
		p.write(t, canonicalsql.Key{Family: canonicalsql.ScopeState, Scope: scopes[1]}, state)
		reader := publicReader(t, f.Store)

		seq, err := reader.CurrentScopeChangeSeq(t.Context(), scopes[1])
		if err != nil || seq != math.MaxUint64 {
			t.Fatalf("seq=%d err=%v", seq, err)
		}

		denyScope := nativeMutation(t)
		denyScope.Scope = scopes[1]
		denyMutationSource(t, p, denyScope)

		for _, scope := range scopes {
			got, err := reader.LoadRecallRecords(t.Context(), scope, []string{"revision"})

			want := 1
			if scope == scopes[1] {
				want = 0
			}

			if err != nil || len(got) != want {
				t.Fatalf("scope=%+v got=%d want=%d err=%v", scope, len(got), want, err)
			}
		}
	})
}

func assertReaderScopePages(t *testing.T, store activeReaderStore, want []memory.Scope) {
	t.Helper()

	var cursor []byte
	for i, scope := range want {
		states, next, err := store.ListCanonicalScopeStates(t.Context(), cursor, 1)
		if err != nil || len(states) != 1 || states[0].Scope != scope {
			t.Fatalf("scope page=%+v err=%v", states, err)
		}

		if (len(next) == 0) != (i == len(want)-1) {
			t.Fatalf("unexpected terminal cursor at %d", i)
		}

		cursor = next
	}

	if _, _, err := store.ListCanonicalScopeStates(t.Context(), []byte("bad"), 1); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestPublicReaderSupersessionTimeAndCancellation(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		mutation := nativeMutation(t)
		instant := mutation.Operation.CommittedAt.Add(123456789 * time.Nanosecond)

		mutation.Revisions[0].Temporal.ObservedAt = instant
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		replacement := laterMutation(mutation, "replacement", 1)

		replacement.Revisions[0].Revision = math.MaxUint64
		if _, err := store.ApplyCanonicalMutation(t.Context(), replacement); err != nil {
			t.Fatal(err)
		}

		reader := publicReader(t, f.Store)

		records, err := reader.LoadRecallRecords(t.Context(), mutation.Scope, []string{"revision", "replacement", "foreign"})
		if err != nil || len(records) != 1 || records[0].RevisionID != "replacement" ||
			records[0].Revision != math.MaxUint64 || !records[0].CreatedAt.Equal(instant) {
			t.Fatalf("superseded records=%+v err=%v", records, err)
		}

		assertReaderTimes(t, reader, mutation.Scope, instant)
		assertReaderWatermarkAndCancel(t, reader, mutation.Scope)
	})
}

func assertReaderTimes(t *testing.T, reader sqlReader, scope memory.Scope, instant time.Time) {
	t.Helper()

	for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		asOf := instant.Add(delta)
		result, err := reader.SearchDerived(t.Context(), memory.DerivedSearchRequest{Scope: scope,
			Query: "protected", Limit: 10, AsOf: &asOf})

		count := 1
		if delta < 0 {
			count = 0
		}

		if err != nil || len(result.Results) != count {
			t.Fatalf("asof delta=%s results=%d err=%v", delta, len(result.Results), err)
		}
	}
}

func assertReaderWatermarkAndCancel(t *testing.T, reader sqlReader, scope memory.Scope) {
	t.Helper()
	_, err := reader.SearchDerived(t.Context(), memory.DerivedSearchRequest{Scope: scope, Query: "protected",
		Limit: 10, MinScopeChangeSeq: 3})
	assertMutationCode(t, err, memory.CodeConflict)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := reader.LoadRecallRecords(ctx, scope, []string{"replacement"})
	if !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatalf("canceled=%+v err=%v", got, err)
	}
}
