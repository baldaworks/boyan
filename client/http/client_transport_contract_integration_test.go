//go:build integration

package clienthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	clienthttp "github.com/baldaworks/boyan/client/http"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestClientClassifiesHTTPClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL, Timeout: time.Millisecond})
	requireNoError(t, err, "New() error = %v")
	assertClientError(t, client.Live(context.Background()), boyan.CodeTimeout, boyan.ErrorClassRetryable)
}

func TestClientRejectsCrossOriginIngestRedirect(t *testing.T) {
	t.Parallel()

	turn := mustTurn(t, "redirect-turn")

	var targetCalls atomic.Int64

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)

		_ = json.NewEncoder(w).Encode(receiptFixture(turn.ExportID, boyan.IngestDispositionAccepted))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	for _, configured := range []struct {
		name   string
		client *http.Client
	}{
		{name: "default"},
		{name: "supplied", client: &http.Client{}},
	} {
		t.Run(configured.name, func(t *testing.T) {
			client, err := clienthttp.New(clienthttp.Config{BaseURL: origin.URL, Client: configured.client})
			requireNoError(t, err, "New() error = %v")

			receipt, err := client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
				SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
				ProducerID:    "producer-redirect",
				ScopeSequence: 1,
				Payload:       turn,
			})
			if err == nil {
				t.Fatalf("IngestTurn() receipt = %+v, want redirect rejection", receipt)
			}
		})
	}

	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("cross-origin target calls = %d, want 0", got)
	}
}
