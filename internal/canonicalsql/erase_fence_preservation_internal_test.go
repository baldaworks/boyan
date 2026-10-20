package canonicalsql

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeEraseRawPayloadGuardAndOwnedProgress(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		history := prepareTraceHistory(t, owner)
		request := erasePrivacyRequest(history)
		_, err := owner.BeginScopeErase(t.Context(), request)
		requireEraseTestNoError(t, err)

		err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return checkScopeEraseFence(ctx, tx, history.scope, request.OperationID)
		})
		requireEraseTestNoError(t, err)

		err = owner.DeletePayload(t.Context(), history.payloads[0].Ref)
		if !memory.IsRetryable(err) {
			t.Fatalf("raw payload delete crossed pending erase guard: %v", err)
		}

		err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return checkScopeEraseFence(ctx, tx, history.scope, "foreign")
		})
		if !memory.IsRetryable(err) {
			t.Fatalf("foreign owner passed pending erase fence: %v", err)
		}
	})
}
