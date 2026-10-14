package memory_test

import (
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallResponseMinimumWatermark(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		counter string
		minimum uint64
		reject  bool
	}{
		{name: "missing", minimum: 42, reject: true},
		{name: "zero", counter: `,"scope_change_seq":0`, minimum: 42, reject: true},
		{name: "below", counter: `,"scope_change_seq":1`, minimum: 42, reject: true},
		{name: "matching", counter: `,"scope_change_seq":42`, minimum: 42},
		{name: "exceeding", counter: `,"scope_change_seq":43`, minimum: 42},
		{name: "zero_minimum_missing"},
		{name: "zero_minimum_zero", counter: `,"scope_change_seq":0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var response memory.RecallResponse
			if err := json.Unmarshal([]byte(`{"schema_version":"v1"`+test.counter+`}`), &response); err != nil {
				t.Fatal(err)
			}

			response.SchemaVersion = memory.RecallSchemaVersionV1
			response.Trust = memory.ReferenceTrustUntrusted
			response.Scope = memory.Scope{Key: "test:watermark", Kind: memory.ScopeKindPersonal}
			request := memory.RecallRequest{
				SchemaVersion: memory.RecallSchemaVersionV1, Scope: response.Scope,
				Query: "reference", Limit: 1, MinScopeChangeSeq: test.minimum,
			}

			for _, empty := range []bool{true, false} {
				response.Results = nil
				if !empty {
					response.Results = []memory.RecallReference{{
						SchemaVersion: memory.RecallSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted,
						Scope: response.Scope, State: memory.RevisionStateActive, Text: "private reference",
						ItemID: "item", RevisionID: "revision", Revision: 1, Kind: memory.MemoryKindState,
					}}
				}

				assertRecallWatermarkValidation(t, response.Validate(request), test.reject, empty)
			}
		})
	}
}

func assertRecallWatermarkValidation(t *testing.T, err error, reject, empty bool) {
	t.Helper()

	if !reject {
		if err != nil {
			t.Fatalf("empty=%v: Validate() = %v", empty, err)
		}

		return
	}

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeConflict || class != memory.ErrorClassRetryable || !memory.IsRetryable(err) {
		t.Fatalf("empty=%v: expected retryable conflict, got %v", empty, err)
	}
}
