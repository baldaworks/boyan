package clienthttp_test

import (
	"context"
	"net/http"
	"testing"

	clienthttp "github.com/baldaworks/boyan/client/http"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type deadlineTransport struct{}

func (deadlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestClientClassifiesTransportDeadlineAsTimeout(t *testing.T) {
	t.Parallel()

	client, err := clienthttp.New(clienthttp.Config{
		BaseURL: "http://127.0.0.1",
		Client:  &http.Client{Transport: deadlineTransport{}},
	})
	requireNoError(t, err, "New() error = %v")

	turn := mustTurn(t, "transport-deadline")
	_, ingestErr := client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-deadline",
		ScopeSequence: 1,
		Payload:       turn,
	})
	assertClientError(t, ingestErr, boyan.CodeTimeout, boyan.ErrorClassRetryable)
	assertClientError(t, client.Live(context.Background()), boyan.CodeTimeout, boyan.ErrorClassRetryable)
}

func assertClientError(t *testing.T, err error, code boyan.ErrorCode, class boyan.ErrorClass) {
	t.Helper()

	gotCode, gotClass, ok := boyan.ClassifyError(err)
	if !ok || gotCode != code || gotClass != class {
		t.Fatalf("ClassifyError(%v) = %q, %q, %v; want %q, %q, true", err, gotCode, gotClass, ok, code, class)
	}
}
