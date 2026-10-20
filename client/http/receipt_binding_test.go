package clienthttp_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	clienthttp "github.com/baldaworks/boyan/client/http"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

const responseSecret = "secret-foreign-export-or-response-detail"

type ingestFixture struct {
	route    string
	exportID string
	call     func(context.Context, *clienthttp.Client) (boyan.IngestReceipt, error)
}

func ingestFixtures(t *testing.T) []ingestFixture {
	t.Helper()

	turn := mustTurn(t, "requested-secret-turn")
	boundary, err := boyan.NewBoundary(
		turn.Scope, turn.Conversation, "requested-secret-boundary", boyan.BoundaryReasonClose,
		time.Date(2026, time.August, 9, 13, 6, 0, 0, time.UTC),
	)
	requireNoError(t, err, "NewBoundary() error = %v")

	return []ingestFixture{
		{"turns", turn.ExportID, func(ctx context.Context, client *clienthttp.Client) (boyan.IngestReceipt, error) {
			return client.IngestTurn(ctx, boyan.IngestEnvelope[boyan.Turn]{
				SchemaVersion: boyan.IngestHTTPSchemaVersionV1, ProducerID: "producer-1", ScopeSequence: 1, Payload: turn,
			})
		}},
		{"boundaries", boundary.ExportID, func(ctx context.Context, client *clienthttp.Client) (boyan.IngestReceipt, error) {
			return client.IngestBoundary(ctx, boyan.IngestEnvelope[boyan.Boundary]{
				SchemaVersion: boyan.IngestHTTPSchemaVersionV1, ProducerID: "producer-1", ScopeSequence: 1, Payload: boundary,
			})
		}},
	}
}

func receiptFixture(exportID string, disposition boyan.IngestDisposition) boyan.IngestReceipt {
	return boyan.IngestReceipt{
		SchemaVersion: boyan.IngestReceiptSchemaVersionV1, ExportID: exportID,
		Disposition: disposition, Projection: boyan.ProjectionStatusLagging,
		OperationID: "operation-one", OperationIDs: []string{"operation-one", "operation-two"},
		ScopeVersion: 7, ScopeChangeSeq: 11,
	}
}

func encodeResponse(t *testing.T, value any) string {
	t.Helper()

	body, err := json.Marshal(value)
	requireNoError(t, err, "json.Marshal() error = %v")

	return string(body)
}

func assertRejectedReceipt(
	t *testing.T, receipt boyan.IngestReceipt, err error, code boyan.ErrorCode, class boyan.ErrorClass,
) {
	t.Helper()

	if !reflect.DeepEqual(receipt, boyan.IngestReceipt{}) {
		t.Errorf("rejected receipt = %+v, want zero receipt", receipt)
	}

	gotCode, gotClass, ok := boyan.ClassifyError(err)
	if !ok || gotCode != code || gotClass != class {
		t.Fatalf("error = %v (%s,%s,%v), want (%s,%s,true)", err, gotCode, gotClass, ok, code, class)
	}
}
