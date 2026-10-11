package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDenyCanonicalSourceBatchMarksSourceBeforeBoundedCascade(t *testing.T) {
	t.Parallel()

	store := &canonicalForgetStore{revisionIDs: []string{"revision-1", "revision-2"}, nextCursor: "revision-2"}
	deniedAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)

	result, err := memory.DenyCanonicalSourceBatch(context.Background(), store, memory.CanonicalSourceForgetBatchRequest{
		Scope: memory.Scope{
			Key:  "canonical:forget",
			Kind: memory.ScopeKindPersonal,
		}, SourceID: "source-1", Limit: 2, DeniedAt: deniedAt,
	})
	if err != nil {
		t.Fatalf("DenyCanonicalSourceBatch() error = %v", err)
	}

	if want := []string{
		"deny-source:source-1",
		"scan:source-1:",
		"deny-revision:revision-1",
		"deny-revision:revision-2",
	}; !reflect.DeepEqual(store.calls, want) {
		t.Fatalf("calls = %#v, want %#v", store.calls, want)
	}

	if want := (memory.CanonicalSourceForgetBatchResult{
		RevisionIDs: []string{
			"revision-1",
			"revision-2",
		},
		NextCursor: "revision-2",
	}); !reflect.DeepEqual(
		result,
		want,
	) {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
}

func TestDenyCanonicalSourceBatchFailsClosedBeforeScanFailure(t *testing.T) {
	t.Parallel()

	store := &canonicalForgetStore{scanErr: errors.New("scan interrupted")}

	_, err := memory.DenyCanonicalSourceBatch(context.Background(), store, memory.CanonicalSourceForgetBatchRequest{
		Scope: memory.Scope{
			Key:  "canonical:forget",
			Kind: memory.ScopeKindPersonal,
		}, SourceID: "source-1", Limit: 1, DeniedAt: time.Now(),
	})
	if !errors.Is(err, store.scanErr) {
		t.Fatalf("DenyCanonicalSourceBatch() error = %v, want scan error", err)
	}

	if want := []string{"deny-source:source-1", "scan:source-1:"}; !reflect.DeepEqual(store.calls, want) {
		t.Fatalf("calls = %#v, want %#v", store.calls, want)
	}
}

func TestDenyCanonicalSourceBatchRejectsInvalidStoreIdentity(t *testing.T) {
	t.Parallel()

	store := &canonicalForgetStore{revisionIDs: []string{""}}

	_, err := memory.DenyCanonicalSourceBatch(context.Background(), store, memory.CanonicalSourceForgetBatchRequest{
		Scope: memory.Scope{
			Key:  "canonical:forget",
			Kind: memory.ScopeKindPersonal,
		}, SourceID: "source-1", Limit: 1, DeniedAt: time.Now(),
	})
	if code, _, ok := memory.ClassifyError(err); !ok || code != memory.CodeStoreFailure {
		t.Fatalf("DenyCanonicalSourceBatch() error = %v, want store failure", err)
	}

	if want := []string{"deny-source:source-1", "scan:source-1:"}; !reflect.DeepEqual(store.calls, want) {
		t.Fatalf("calls = %#v, want %#v", store.calls, want)
	}
}

type canonicalForgetStore struct {
	calls       []string
	revisionIDs []string
	nextCursor  string
	scanErr     error
}

func (s *canonicalForgetStore) DenySource(_ context.Context, _ memory.Scope, sourceID string, _ time.Time) error {
	s.calls = append(s.calls, "deny-source:"+sourceID)

	return nil
}

func (s *canonicalForgetStore) SourceRevisionBatch(
	_ context.Context,
	_ memory.Scope,
	sourceID, afterRevisionID string,
	_ uint32,
) (
	[]string,
	string,
	error,
) {
	s.calls = append(s.calls, "scan:"+sourceID+":"+afterRevisionID)
	if s.scanErr != nil {
		return nil, "", s.scanErr
	}

	return append([]string(nil), s.revisionIDs...), s.nextCursor, nil
}

func (s *canonicalForgetStore) DenyRevision(_ context.Context, _ memory.Scope, revisionID string, _ time.Time) error {
	s.calls = append(s.calls, "deny-revision:"+revisionID)

	return nil
}
