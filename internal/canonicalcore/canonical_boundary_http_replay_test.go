//go:build integration

package canonicalcore_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpingest "github.com/baldaworks/boyan/internal/httpingest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryHTTPChangedDeliveryAfterRestart(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		f := newBoundaryReplayFixture(t, false, factory)

		status := postReplayBoundary(t, f, f.export, 1)
		if status != http.StatusOK {
			t.Fatalf("first status=%d", status)
		}

		f.restart(t)

		for _, sequence := range []uint64{1, 2} {
			changed := f.export

			changed.Reason = m.BoundaryReasonReset
			if status = postReplayBoundary(t, f, changed, sequence); status != http.StatusConflict {
				t.Fatalf("changed sequence%d status=%d", sequence, status)
			}
		}

		equivalent := f.export
		equivalent.TransitionID = " \t" + equivalent.TransitionID + "\n"

		equivalent.OccurredAt = equivalent.OccurredAt.In(time.FixedZone("offset", 6*60*60))
		if status = postReplayBoundary(t, f, equivalent, 1); status != http.StatusOK {
			t.Fatalf("equivalent same-sequence replay status=%d", status)
		}

		if f.model.scenarioCalls != 1 || f.model.profileCalls != 1 {
			t.Fatal("HTTP replay entered models")
		}
	})
}

func postReplayBoundary(t *testing.T, f *boundaryReplayFixture, export m.Boundary, sequence uint64) int {
	t.Helper()

	handler, err := httpingest.NewHandler(httpingest.Config{Turns: f.runtime, Boundaries: f.runtime})
	requireNoError(t, err, "handler: %v")
	body, err := json.Marshal(m.IngestEnvelope[m.Boundary]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: "http-replay", ScopeSequence: sequence, Payload: export})
	requireNoError(t, err, "envelope: %v")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/ingest/boundaries", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)

	return response.Code
}
