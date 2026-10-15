package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

func (provider *openAIProvider) Embed(
	ctx context.Context,
	inputs []string,
) (output [][]float32, resultErr error) {
	if ctx == nil {
		return nil, newError(CodeInvalidInput)
	}

	defer propagateCallerContext(ctx, &output, &resultErr)

	if err := validateInputs(ctx, inputs); err != nil {
		return nil, err
	}

	payload, err := provider.requestPayload(inputs)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	if err := provider.acquire(callCtx); err != nil {
		return nil, callError(ctx, callCtx, err)
	}
	defer func() { <-provider.slots }()

	request, err := provider.request(callCtx, payload)
	if err != nil {
		return nil, err
	}

	status, encoded, err := provider.call(ctx, callCtx, request)
	if err != nil {
		return nil, err
	}

	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, statusError(status, encoded)
	}

	return provider.decodeResponse(encoded, len(inputs))
}

func validateInputs(ctx context.Context, inputs []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(inputs) == 0 || len(inputs) > maxBatchSize {
		return newError(CodeInputLimit)
	}

	for _, input := range inputs {
		if !utf8.ValidString(input) || strings.TrimSpace(input) == "" {
			return newError(CodeInvalidInput)
		}

		if len(input) > maxInputBytes {
			return newError(CodeInputLimit)
		}
	}

	return nil
}

func propagateCallerContext(ctx context.Context, output *[][]float32, resultErr *error) {
	if err := ctx.Err(); err != nil {
		*output = nil
		*resultErr = err
	}
}

func (provider *openAIProvider) requestPayload(inputs []string) ([]byte, error) {
	payload, err := json.Marshal(embeddingRequest{
		Model: provider.model, Input: inputs, EncodingFormat: "float",
	})
	if err != nil || len(payload) > maxRequestBytes {
		return nil, newError(CodeInputLimit)
	}

	return payload, nil
}

func (provider *openAIProvider) acquire(ctx context.Context) error {
	select {
	case provider.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (provider *openAIProvider) request(ctx context.Context, payload []byte) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, newError(CodeInvalidInput)
	}

	request.GetBody = nil

	request.Header.Set("Content-Type", "application/json")

	if provider.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+provider.apiKey)
	}

	return request, nil
}

func (provider *openAIProvider) call(
	parent context.Context,
	callCtx context.Context,
	request *http.Request,
) (int, []byte, error) {
	response, err := provider.httpClient.Do(request)
	if err != nil {
		return 0, nil, callError(parent, callCtx, err)
	}
	defer func() { _ = response.Body.Close() }()

	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, callError(parent, callCtx, err)
	}

	if err := parent.Err(); err != nil {
		return 0, nil, err
	}

	if len(encoded) > maxResponseBytes {
		return 0, nil, newError(CodeResponseLimit)
	}

	return response.StatusCode, encoded, nil
}

func callError(parent, call context.Context, cause error) error {
	if err := parent.Err(); err != nil {
		return err
	}

	if call.Err() != nil || errors.Is(cause, context.DeadlineExceeded) {
		return newError(CodeDeadline)
	}

	return newError(CodeUnavailable)
}
