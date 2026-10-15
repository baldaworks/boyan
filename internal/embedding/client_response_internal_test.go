package embedding

import (
	"runtime"
	"strings"
	"testing"
)

func TestDecodeResponseBoundsMaterializedEntries(t *testing.T) {
	provider := testOpenAIProvider(t, 1)
	body := []byte(`{"model":"m","data":[{"index":0,"embedding":[1]}` +
		strings.Repeat(`,{}`, 50_000) + `]}`)

	allocated, err := decodeAllocatedBytes(provider, body, 1)
	requireInternalCode(t, err, CodeInvalidResponse)

	if allocated > 128<<10 {
		t.Fatalf("decodeResponse() allocated %d bytes for excess entries, want at most 128 KiB", allocated)
	}
}

func TestDecodeResponseBoundsMaterializedComponents(t *testing.T) {
	provider := testOpenAIProvider(t, 1)
	body := []byte(`{"model":"m","data":[{"index":0,"embedding":[` +
		strings.Repeat(`0,`, 50_000) + `0]}]}`)

	allocated, err := decodeAllocatedBytes(provider, body, 1)
	requireInternalCode(t, err, CodeDimensionMismatch)

	if allocated > 128<<10 {
		t.Fatalf("decodeResponse() allocated %d bytes for excess components, want at most 128 KiB", allocated)
	}
}

func TestDecodeResponseAllowsLegalFieldOrder(t *testing.T) {
	provider := testOpenAIProvider(t, 2)
	body := []byte(`{"data":[{"embedding":[3,4],"index":0}],"model":"m"}`)

	vectors, err := provider.decodeResponse(body, 1)
	if err != nil {
		t.Fatalf("decodeResponse() error = %v", err)
	}

	if vectors[0][0] != 0.6 || vectors[0][1] != 0.8 {
		t.Fatalf("decodeResponse() = %#v, want [[0.6 0.8]]", vectors)
	}
}

func decodeAllocatedBytes(provider *openAIProvider, body []byte, inputCount int) (uint64, error) {
	runtime.GC()

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := provider.decodeResponse(body, inputCount)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	return after.TotalAlloc - before.TotalAlloc, err
}

func testOpenAIProvider(t *testing.T, dimensions int) *openAIProvider {
	t.Helper()

	provider, err := NewOpenAIProvider(OpenAIOptions{
		Endpoint: "https://provider.example/v1/embeddings", Model: "m", Dimensions: dimensions,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	return provider.(*openAIProvider)
}

func requireInternalCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()

	got, ok := Classify(err)
	if !ok || got != want {
		t.Fatalf("Classify(%v) = %q, %v; want %q, true", err, got, ok, want)
	}
}
