//go:build integration

package clienthttp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	clienthttp "github.com/baldaworks/boyan/client/http"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestClientRejectsForeignIngestReceipts(t *testing.T) {
	t.Parallel()

	for _, fixture := range ingestFixtures(t) {
		for _, disposition := range []boyan.IngestDisposition{
			boyan.IngestDispositionAccepted, boyan.IngestDispositionDuplicate,
		} {
			t.Run(fixture.route+"/"+string(disposition), func(t *testing.T) {
				foreign := receiptFixture(responseSecret, disposition)
				requireNoError(t, foreign.Validate(), "foreign fixture invalid: %v")
				receipt, err := ingestResponse(t, fixture, http.StatusOK, encodeResponse(t, foreign))
				assertRejectedReceipt(t, receipt, err, boyan.CodeInvalidDerived, boyan.ErrorClassPermanent)

				var typed *boyan.Error
				if !errors.As(err, &typed) {
					t.Fatalf("error = %v, want typed error", err)
				}

				if typed.Message != "client response export identity does not match request" || typed.Unwrap() != nil {
					t.Fatalf("error = %+v, want constant identity mismatch without cause", typed)
				}

				if strings.Contains(err.Error(), fixture.exportID) || strings.Contains(err.Error(), responseSecret) {
					t.Fatalf("mismatch error disclosed request or response identity: %v", err)
				}
			})
		}
	}
}

func TestClientPreservesMatchingIngestReceipts(t *testing.T) {
	t.Parallel()

	for _, fixture := range ingestFixtures(t) {
		for _, disposition := range []boyan.IngestDisposition{
			boyan.IngestDispositionAccepted, boyan.IngestDispositionDuplicate,
		} {
			t.Run(fixture.route+"/"+string(disposition), func(t *testing.T) {
				want := receiptFixture(fixture.exportID, disposition)
				receipt, err := ingestResponse(t, fixture, http.StatusOK, encodeResponse(t, want))
				requireNoError(t, err, "matching receipt error = %v")

				if !reflect.DeepEqual(receipt, want) {
					t.Fatalf("receipt = %+v, want all fields %+v", receipt, want)
				}
			})
		}
	}
}

func ingestResponse(
	t *testing.T, fixture ingestFixture, status int, body string,
) (boyan.IngestReceipt, error) {
	t.Helper()

	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.Method != http.MethodPost || r.URL.Path != "/v1/ingest/"+fixture.route {
			t.Errorf("request = %s %s, want selected POST route", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	requireNoError(t, err, "New() error = %v")
	receipt, err := fixture.call(context.Background(), client)

	if calls.Load() != 1 {
		t.Fatalf("HTTP calls = %d, want exactly one", calls.Load())
	}

	return receipt, err
}
