//go:build integration

package clienthttp_test

import (
	"net/http"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

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
