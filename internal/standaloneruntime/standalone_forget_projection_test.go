package standaloneruntime //nolint:testpackage // Private composition enforces order and context ownership.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
)

type denialRetireFunc func(context.Context, memory.Scope) error

func (f denialRetireFunc) ReconcileCanonicalDenials(ctx context.Context, scope memory.Scope) error {
	return f(ctx, scope)
}

type denialSyncFunc func(context.Context, memory.Scope) error

func (f denialSyncFunc) Sync(ctx context.Context, scope memory.Scope) error { return f(ctx, scope) }

//nolint:cyclop,funlen,gocognit // Required dependency, ordering and context-priority cases share the same call trace.
func TestForgetProjectionSyncerOrderAndErrors(t *testing.T) {
	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "denial:order"}
	failure := memory.RetryableError(memory.CodeConflict, "required projection unavailable", errors.New("boundary"))

	for _, failAt := range []string{"", "retire", "sync", "cancel-retire", "cancel-sync"} {
		t.Run(failAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var calls []string

			call := func(at string) error {
				calls = append(calls, at)
				if failAt == "cancel-"+at {
					cancel()

					return failure
				}

				if failAt == at {
					return failure
				}

				return nil
			}
			s := &forgetProjectionSyncer{retire: denialRetireFunc(func(got context.Context, gotScope memory.Scope) error {
				if got != ctx || gotScope != scope {
					t.Fatal("retirement context/scope changed")
				}

				return call("retire")
			}), next: denialSyncFunc(func(got context.Context, gotScope memory.Scope) error {
				if got != ctx || gotScope != scope {
					t.Fatal("sync context/scope changed")
				}

				return call("sync")
			})}
			err := s.Sync(ctx, scope)

			wantCalls := []string{"retire", "sync"}
			if failAt == "retire" || failAt == "cancel-retire" {
				wantCalls = []string{"retire"}
			}

			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("calls=%v", calls)
			}

			switch failAt {
			case "":
				requireNoError(t, err, "joined sync: %v")
			case "cancel-retire", "cancel-sync":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("context priority: %v", err)
				}
			default:
				if !errors.Is(err, failure) {
					t.Fatalf("required error changed: %v", err)
				}
			}
		})
	}

	for _, missing := range []string{"receiver", "retire", "sync", "context", "scope", "canceled"} {
		t.Run(missing, func(t *testing.T) {
			var (
				retire canonicalDenialRetirer = denialRetireFunc(func(context.Context, memory.Scope) error {
					t.Fatal("invalid call mutated retirement")

					return nil
				})
				next projection.Syncer = denialSyncFunc(func(context.Context, memory.Scope) error {
					t.Fatal("invalid call mutated sync")

					return nil
				})
			)

			s := &forgetProjectionSyncer{retire: retire, next: next}
			ctx := t.Context()
			input := scope

			switch missing {
			case "receiver":
				s = nil
			case "retire":
				s.retire = nil
			case "sync":
				s.next = nil
			case "context":
				ctx = nil
			case "scope":
				input = memory.Scope{}
			case "canceled":
				var cancel context.CancelFunc

				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			if err := s.Sync(ctx, input); err == nil {
				t.Fatal("invalid dependency/request accepted")
			}
		})
	}
}
