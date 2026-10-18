package storefixture_test

import (
	"context"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"testing"
	"time"
)

func TestPublicForgetOutcomeAndDenialReplay(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
		s := f.Store.(forgetStore)
		scope := memory.Scope{Key: "forget:replay", Kind: memory.ScopeKindPersonal}
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		r := memory.CanonicalForgetCommitRequest{Scope: scope, OperationID: "forget", Kind: memory.ForgetKindScope,
			Fingerprint: "fp", ForgottenAt: at}

		out, err := s.CommitCanonicalForget(t.Context(), r)
		if err != nil || !out.ForgottenAt.Equal(at) || out.ScopeVersion != 1 {
			t.Fatalf("out=%+v %v", out, err)
		}

		r.ForgottenAt = at.Add(time.Hour)

		replay, err := s.CommitCanonicalForget(t.Context(), r)
		if err != nil || !reflect.DeepEqual(out, replay) {
			t.Fatalf("replay=%+v %v", replay, err)
		}

		requireForgetSuccess(t, s.DenySource(t.Context(), scope, "source", at))

		requireForgetSuccess(t, s.DenySource(t.Context(), scope, "source", at.Add(time.Hour)))

		requireForgetSuccess(t, s.DenyRevision(t.Context(), scope, "revision", at))

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		requireForgetSuccess(t, err)

		defer reopened.Close()

		s = reopened.(forgetStore)

		replay, err = s.CommitCanonicalForget(t.Context(), r)
		if err != nil || !reflect.DeepEqual(out, replay) {
			t.Fatalf("reopened=%+v %v", replay, err)
		}

		r.Fingerprint = "different"
		if _, err := s.CommitCanonicalForget(t.Context(), r); err == nil {
			t.Fatal("changed fingerprint replayed")
		}

		assertPublicForgetDenials(t, reopened, scope)
	})
}

func TestPublicForgetLegacyScrub(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		scrub, ok := f.Store.(interface {
			ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sources, revisions []string) error
		})
		if !ok {
			t.Fatal("public SQL owner lacks bounded legacy scrub")
		}

		m := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		s := f.Store.(forgetStore)
		if err := s.DenySource(t.Context(), m.Scope, m.Sources[0].SourceID, time.Now()); err != nil {
			t.Fatal(err)
		}

		if err := s.DenyRevision(t.Context(), m.Scope, m.Revisions[0].RevisionID, time.Now()); err != nil {
			t.Fatal(err)
		}

		if err := scrub.ScrubCanonicalForget(t.Context(), m.Scope, []string{m.Sources[0].SourceID},
			[]string{m.Revisions[0].RevisionID}); err != nil {
			t.Fatal(err)
		}

		for _, p := range m.Payloads {
			if _, err := store.LoadPayload(t.Context(), p.Ref); err == nil {
				t.Fatal("legacy scrub left content")
			}
		}
	})
}

func assertPublicForgetDenials(t *testing.T, reopened storefixture.Store, scope memory.Scope) {
	t.Helper()

	denial := reopened.(interface {
		IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
		IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	})
	if denied, err := denial.IsSourceDenied(t.Context(), scope, "source"); err != nil || !denied {
		t.Fatalf("source denial=%t %v", denied, err)
	}

	if denied, err := denial.IsRevisionDenied(t.Context(), scope, "revision"); err != nil || !denied {
		t.Fatalf("revision denial=%t %v", denied, err)
	}

	scope.Kind = memory.ScopeKindGroup
	if denied, err := denial.IsSourceDenied(t.Context(), scope, "source"); err != nil || denied {
		t.Fatalf("foreign denial=%t %v", denied, err)
	}
}

func requireForgetSuccess(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
