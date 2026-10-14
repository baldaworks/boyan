package clienthttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	clienthttp "github.com/baldaworks/boyan/client/http"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func invalidReceiptBodies(t *testing.T, exportID string) map[string]string {
	t.Helper()

	valid := receiptFixture(exportID, boyan.IngestDispositionAccepted)
	encoded := encodeResponse(t, valid)
	tests := map[string]string{
		"malformed":          "{",
		"empty":              "",
		"null":               "null",
		"unknown":            "{\"extra\":true," + encoded[1:],
		"trailing":           encoded + " {}",
		"trailing_malformed": encoded + " invalid",
	}

	fields := map[string]any{
		"schema_version":    "invalid-schema",
		"export_id":         " noncanonical ",
		"disposition":       "invalid-disposition",
		"projection_status": "invalid-projection",
		"scope_version":     0,
		"scope_change_seq":  0,
		"operation_id":      " invalid-operation ",
		"operation_ids":     []string{"duplicate", "duplicate"},
	}
	for field, value := range fields {
		var body map[string]any
		requireNoError(t, json.Unmarshal([]byte(encoded), &body), "decode fixture: %v")
		body[field] = value
		tests[field] = encodeResponse(t, body)
	}

	return tests
}

func TestClientRejectsInvalidSuccessReceipts(t *testing.T) {
	t.Parallel()

	for _, fixture := range ingestFixtures(t) {
		for name, body := range invalidReceiptBodies(t, fixture.exportID) {
			t.Run(fixture.route+"/"+name, func(t *testing.T) {
				receipt, err := ingestResponse(t, fixture, http.StatusOK, body)
				assertRejectedReceipt(t, receipt, err, boyan.CodeInvalidDerived, boyan.ErrorClassPermanent)
			})
		}
	}
}

type responseFailureCase struct {
	name   string
	status int
	body   string
	code   boyan.ErrorCode
	class  boyan.ErrorClass
}

func responseFailureCases(t *testing.T) []responseFailureCase {
	t.Helper()

	retryable := encodeResponse(t, boyan.IngestErrorResponse{
		Code: boyan.CodeConflict, Class: boyan.ErrorClassRetryable, Message: responseSecret,
	})
	permanent := encodeResponse(t, boyan.IngestErrorResponse{
		Code: boyan.CodeDisabled, Class: boyan.ErrorClassPermanent, Message: responseSecret,
	})
	unknownClass := encodeResponse(t, boyan.IngestErrorResponse{
		Code: boyan.CodeDisabled, Class: "unknown", Message: responseSecret,
	})
	unknownCode := encodeResponse(t, boyan.IngestErrorResponse{
		Code: "future_server_code", Class: boyan.ErrorClassRetryable, Message: responseSecret,
	})

	return []responseFailureCase{
		{"retryable_body_overrides_400", 400, retryable, boyan.CodeConflict, boyan.ErrorClassRetryable},
		{"permanent_body_overrides_503", 503, permanent, boyan.CodeDisabled, boyan.ErrorClassPermanent},
		{"unknown_class_falls_back", 504, unknownClass, boyan.CodeTimeout, boyan.ErrorClassRetryable},
		{"unknown_code_falls_back", 503, unknownCode, boyan.CodeUnavailable, boyan.ErrorClassRetryable},
		{"invalid_body_falls_back", 429, "{}", boyan.CodeUnavailable, boyan.ErrorClassRetryable},
		{"malformed_falls_back", 503, "{", boyan.CodeUnavailable, boyan.ErrorClassRetryable},
		{
			"unknown_field_falls_back", 409, "{\"unknown\":true," + permanent[1:],
			boyan.CodeConflict, boyan.ErrorClassRetryable,
		},
		{"trailing_falls_back", 400, retryable + " {}", boyan.CodeInvalidDerived, boyan.ErrorClassPermanent},
		{"unknown_status_falls_back", 418, responseSecret, boyan.CodePermanent, boyan.ErrorClassPermanent},
	}
}

func TestClientPreservesErrorResponseClassification(t *testing.T) {
	t.Parallel()

	for _, fixture := range ingestFixtures(t) {
		for _, tc := range responseFailureCases(t) {
			t.Run(fixture.route+"/"+tc.name, func(t *testing.T) {
				receipt, err := ingestResponse(t, fixture, tc.status, tc.body)
				assertRejectedReceipt(t, receipt, err, tc.code, tc.class)
				assertSafeClientFailure(t, err)
			})
		}
	}
}

func assertSafeClientFailure(t *testing.T, err error) {
	t.Helper()

	var typed *boyan.Error
	if !errors.As(err, &typed) || typed.Message != "http ingest request failed" || typed.Unwrap() != nil {
		t.Fatalf("error = %v, want constant safe failure without server detail", err)
	}
}

type failedTransport struct{}

func (failedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New(responseSecret)
}

func TestClientPreservesTransportFailureClassification(t *testing.T) {
	t.Parallel()

	for _, fixture := range ingestFixtures(t) {
		t.Run(fixture.route, func(t *testing.T) {
			client, err := clienthttp.New(clienthttp.Config{
				BaseURL: "http://127.0.0.1", Client: &http.Client{Transport: failedTransport{}},
			})
			requireNoError(t, err, "New() error = %v")
			receipt, err := fixture.call(context.Background(), client)
			assertRejectedReceipt(t, receipt, err, boyan.CodeUnavailable, boyan.ErrorClassRetryable)

			var typed *boyan.Error
			if !errors.As(err, &typed) || typed.Message != "client request failed" || typed.Unwrap() == nil {
				t.Fatalf("transport error = %v, want original transport failure convention", err)
			}
		})
	}
}
